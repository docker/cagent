// Package transcribe provides real-time audio transcription using OpenAI's Realtime API.
package transcribe

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/gorilla/websocket"
	"golang.org/x/sync/errgroup"

	"github.com/docker/docker-agent/pkg/audio/capture"
)

const openAIRealtimeURL = "wss://api.openai.com/v1/realtime?model=gpt-4o-realtime-preview"

// ErrNotSupported is returned when transcription is unavailable on this platform.
var ErrNotSupported = errors.New("speech-to-text is only supported on macOS")

// TranscriptHandler receives transcription deltas and must return when ctx is canceled.
type TranscriptHandler func(ctx context.Context, delta string)

type audioCapture interface {
	Run(ctx context.Context, filePath string, handler capture.AudioHandler, ready func()) error
}

// Transcriber holds configuration; each Run owns its connection and audio capture.
type Transcriber struct {
	apiKey    string
	endpoint  string
	capture   audioCapture
	supported bool
}

// New creates a transcriber without opening a connection or microphone.
func New(apiKey string) *Transcriber {
	return &Transcriber{
		apiKey:    apiKey,
		endpoint:  openAIRealtimeURL,
		capture:   capture.NewCapturer(capture.SampleRate24000),
		supported: audioSupported,
	}
}

// IsSupported reports whether microphone capture is available on this platform.
func (t *Transcriber) IsSupported() bool { return t.supported }

// Run transcribes until cancellation or a capture/server error. Ready is called
// after capture starts. Callbacks must honor their context and finish before Run
// returns. Cancellation returns nil.
func (t *Transcriber) Run(ctx context.Context, handler TranscriptHandler, ready func(context.Context)) error {
	if !t.IsSupported() {
		return ErrNotSupported
	}
	if ctx.Err() != nil {
		return nil
	}
	if t.apiKey == "" {
		return errors.New("/speak sends audio directly to OpenAI; set a separate OPENAI_API_KEY environment variable even when chat uses Docker's models gateway, which does not support /speak")
	}

	conn, response, err := dial(ctx, t.endpoint, http.Header{
		"Authorization": []string{"Bearer " + t.apiKey},
		"OpenAI-Beta":   []string{"realtime=v1"},
	})
	if err != nil {
		if response != nil && response.Body != nil {
			response.Body.Close()
		}
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("connect /speak directly to OpenAI Realtime API (not Docker's models gateway): %w", err)
	}
	defer conn.Close()

	lifetime, cancel := context.WithCancel(ctx)
	defer cancel()
	group, runCtx := errgroup.WithContext(lifetime)
	closed := make(chan struct{})
	stopClose := context.AfterFunc(runCtx, func() {
		conn.Close() // Interrupt blocked reads and writes before joining workers.
		close(closed)
	})
	defer func() {
		if !stopClose() {
			<-closed
		}
	}()

	if err := conn.WriteJSON(map[string]any{
		"type": "session.update",
		"session": map[string]any{
			"modalities":         []string{"text"},
			"input_audio_format": "pcm16",
			"input_audio_transcription": map[string]string{
				"model": "whisper-1",
			},
			"turn_detection": map[string]any{"type": "server_vad"},
		},
	}); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("configure transcription: %w", err)
	}

	// Never block the native audio callback on network I/O.
	audio := make(chan []byte, 32)
	overflow := make(chan error, 1)
	group.Go(func() error {
		err := t.capture.Run(runCtx, "", func(data []byte) {
			select {
			case audio <- data:
			case <-runCtx.Done():
			default:
				select {
				case overflow <- errors.New("audio upload fell behind capture"):
				default:
				}
			}
		}, func() {
			if ready != nil {
				ready(runCtx)
			}
		})
		if runCtx.Err() != nil {
			return nil
		}
		if err != nil {
			return fmt.Errorf("capture audio: %w", err)
		}
		return errors.New("audio capture stopped unexpectedly")
	})
	group.Go(func() error {
		select {
		case <-runCtx.Done():
			return nil
		case err := <-overflow:
			return err
		}
	})
	group.Go(func() error {
		for {
			select {
			case <-runCtx.Done():
				return nil
			case data := <-audio:
				if err := conn.WriteJSON(map[string]string{
					"type":  "input_audio_buffer.append",
					"audio": base64.StdEncoding.EncodeToString(data),
				}); err != nil {
					return fmt.Errorf("upload audio: %w", err)
				}
			}
		}
	})
	group.Go(func() error { return readLoop(runCtx, conn, handler) })
	err = group.Wait()
	if ctx.Err() != nil {
		return nil
	}
	return err
}

// Gorilla applies handshake deadlines but does not close an established TCP
// connection when its context is canceled during the HTTP upgrade.
func dial(ctx context.Context, endpoint string, headers http.Header) (*websocket.Conn, *http.Response, error) {
	dialer := *websocket.DefaultDialer
	var stopClose func() bool
	var closed chan struct{}
	dialer.NetDialContext = func(dialCtx context.Context, network, address string) (net.Conn, error) {
		conn, err := (&net.Dialer{}).DialContext(dialCtx, network, address)
		if err != nil {
			return nil, err
		}
		closed = make(chan struct{})
		stopClose = context.AfterFunc(ctx, func() {
			conn.Close()
			close(closed)
		})
		return conn, nil
	}
	defer func() {
		if stopClose != nil && !stopClose() {
			<-closed
		}
	}()
	return dialer.DialContext(ctx, endpoint, headers)
}

func readLoop(ctx context.Context, conn *websocket.Conn, handler TranscriptHandler) error {
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read transcription: %w", err)
		}
		var event struct {
			Type  string `json:"type"`
			Delta string `json:"delta"`
			Error *struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(msg, &event) != nil {
			continue
		}
		switch event.Type {
		case "conversation.item.input_audio_transcription.delta":
			if handler != nil && event.Delta != "" && ctx.Err() == nil {
				handler(ctx, event.Delta)
			}
		case "error":
			if event.Error != nil && event.Error.Code != "input_audio_buffer_commit_empty" {
				return fmt.Errorf("transcription server (%s): %s", event.Error.Code, event.Error.Message)
			}
		}
	}
}
