package tests

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

type FakeOpenAIServer struct {
	responses []CannedResponse
}

type Opt func(*FakeOpenAIServer)

func WithResponseForQuestion(question, response string) Opt {
	return func(srv *FakeOpenAIServer) {
		srv.responses = append(srv.responses, CannedResponse{
			Question: question,
			Response: response,
		})
	}
}

func startFakeOpenAIServer(t *testing.T, opts ...Opt) *httptest.Server {
	t.Helper()

	srv := newFakeOpenAIServer(opts...)

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", srv.chatCompletionsHandler)

	httpServer := httptest.NewServer(mux)
	t.Cleanup(httpServer.Close)

	return httpServer
}

func newFakeOpenAIServer(opts ...Opt) *FakeOpenAIServer {
	server := &FakeOpenAIServer{}

	for _, opt := range opts {
		opt(server)
	}

	return server
}

func (s *FakeOpenAIServer) chatCompletionsHandler(w http.ResponseWriter, r *http.Request) {
	var requestBody Request
	if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
		http.Error(w, "decoding request body", http.StatusInternalServerError)
		return
	}

	if err := s.streamResponse(w, requestBody); err != nil {
		http.Error(w, "streaming response", http.StatusInternalServerError)
		return
	}
}

func (s *FakeOpenAIServer) streamResponse(w http.ResponseWriter, r Request) error {
	writer, ok := w.(WriterFlusher)
	if !ok {
		return errors.New("streaming not supported")
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	id := generateRandomID()
	created := time.Now().Unix()

	if err := sendChunk(writer, Response{
		ID:      id,
		Created: created,
		Model:   r.Model,
		Object:  "chat.completion.chunk",
		Choices: []Choice{{
			Delta: &Delta{
				Role: "assistant",
			},
		}},
	}); err != nil {
		return err
	}

	lastMessage := r.Messages[len(r.Messages)-1]
	content := ""
	for _, response := range s.responses {
		if lastMessage.Role == "user" && lastMessage.Content == response.Question {
			content = response.Response
			break
		}
	}

	if err := sendChunk(writer, Response{
		ID:      id,
		Created: created,
		Model:   r.Model,
		Object:  "chat.completion.chunk",
		Choices: []Choice{{
			Delta: &Delta{
				Content: content,
			},
		}},
	}); err != nil {
		return err
	}

	if err := sendChunk(writer, Response{
		ID:      id,
		Created: created,
		Model:   r.Model,
		Object:  "chat.completion.chunk",
		Choices: []Choice{{
			FinishReason: "stop",
		}},
	}); err != nil {
		return err
	}

	if err := sendChunk(writer, Response{
		ID:      id,
		Created: created,
		Model:   r.Model,
		Object:  "chat.completion.chunk",
		Usage: &Usage{
			InputTokens:  10,
			OutputTokens: 55,
			TotalTokens:  65,
		},
	}); err != nil {
		return err
	}

	// Send [DONE] message
	if _, err := w.Write([]byte("data: [DONE]\n\n")); err != nil {
		return fmt.Errorf("error writing done message: %w", err)
	}
	writer.Flush()

	return nil
}

func sendChunk(w WriterFlusher, chunk Response) error {
	data, err := json.Marshal(chunk)
	if err != nil {
		return fmt.Errorf("error marshaling response: %w", err)
	}

	if _, err := w.Write([]byte("data: " + string(data) + "\n\n")); err != nil {
		return fmt.Errorf("error writing response: %w", err)
	}

	w.Flush()
	return nil
}

func generateRandomID() string {
	return "chatcmpl-" + uuid.New().String()
}

type WriterFlusher interface {
	io.Writer
	http.Flusher
}

type Message struct {
	Content string `json:"content"`
	Role    string `json:"role"`
}

type Delta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

type Choice struct {
	FinishReason string   `json:"finish_reason,omitempty"`
	Index        int      `json:"index"`
	Message      *Message `json:"message,omitempty"`
	Delta        *Delta   `json:"delta,omitempty"`
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type Request struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

type Response struct {
	ID      string   `json:"id"`
	Created int64    `json:"created"`
	Choices []Choice `json:"choices"`
	Model   string   `json:"model"`
	Object  string   `json:"object"`
	Usage   *Usage   `json:"usage,omitempty"`
}

type CannedResponse struct {
	Question string
	Response string
}
