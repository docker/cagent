// Package anthropictest serves scripted Anthropic Messages API streams for
// runtime tests that drive a real Anthropic client. Only tests import it.
package anthropictest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
)

// Request is one captured request.
type Request struct {
	Path  string
	Betas []string
	Body  map[string]any
	Raw   string
}

// Server answers each request with the next scripted SSE reply; the last
// reply repeats.
type Server struct {
	srv *httptest.Server

	mu       sync.Mutex
	requests []Request
	replies  []string
}

func NewServer(t *testing.T, replies ...string) *Server {
	t.Helper()
	s := &Server{replies: replies}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		req := Request{Path: r.URL.Path, Raw: string(raw)}
		for _, h := range r.Header.Values("anthropic-beta") {
			for b := range strings.SplitSeq(h, ",") {
				req.Betas = append(req.Betas, strings.TrimSpace(b))
			}
		}
		_ = json.Unmarshal(raw, &req.Body)
		s.mu.Lock()
		s.requests = append(s.requests, req)
		reply := ""
		if n := len(s.replies); n > 0 {
			reply = s.replies[min(len(s.requests), n)-1]
		}
		s.mu.Unlock()
		w.Header().Set("content-type", "text/event-stream")
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *Server) URL() string { return s.srv.URL }

// Requests returns a copy of the requests received so far.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.requests)
}

// TextReply is a plain text answer reporting inputTokens.
func TextReply(id, text string, inputTokens int64) string {
	return body(
		messageStart(id, inputTokens),
		event("content_block_start", map[string]any{"index": 0, "content_block": map[string]any{"type": "text", "text": ""}}),
		event("content_block_delta", map[string]any{"index": 0, "delta": map[string]any{"type": "text_delta", "text": text}}),
		event("content_block_stop", map[string]any{"index": 0}),
		messageDelta("end_turn", map[string]any{"output_tokens": 7}),
		event("message_stop", nil),
	)
}

// ThinkingToolReply is signed thinking followed by one tool call.
func ThinkingToolReply(id, thinking, signature, toolID, toolName string, inputTokens int64) string {
	return body(
		messageStart(id, inputTokens),
		event("content_block_start", map[string]any{"index": 0, "content_block": map[string]any{"type": "thinking", "thinking": "", "signature": ""}}),
		event("content_block_delta", map[string]any{"index": 0, "delta": map[string]any{"type": "thinking_delta", "thinking": thinking}}),
		event("content_block_delta", map[string]any{"index": 0, "delta": map[string]any{"type": "signature_delta", "signature": signature}}),
		event("content_block_stop", map[string]any{"index": 0}),
		event("content_block_start", map[string]any{"index": 1, "content_block": map[string]any{"type": "tool_use", "id": toolID, "name": toolName, "input": map[string]any{}}}),
		event("content_block_delta", map[string]any{"index": 1, "delta": map[string]any{"type": "input_json_delta", "partial_json": "{}"}}),
		event("content_block_stop", map[string]any{"index": 1}),
		messageDelta("tool_use", map[string]any{"output_tokens": 7}),
		event("message_stop", nil),
	)
}

// CompactionReply is a signed native compaction block. Each iteration is an
// {input, output} token pair billed on its own.
func CompactionReply(id, summary, signature string, iterations ...[2]int64) string {
	usage := make([]any, 0, len(iterations))
	for _, it := range iterations {
		usage = append(usage, map[string]any{"type": "compaction", "input_tokens": it[0], "output_tokens": it[1]})
	}
	return body(
		messageStart(id, 0),
		event("content_block_start", map[string]any{"index": 0, "content_block": map[string]any{"type": "compaction", "content": nil, "encrypted_content": "", "signature": nil}}),
		event("content_block_delta", map[string]any{"index": 0, "delta": map[string]any{"type": "compaction_delta", "content": summary, "encrypted_content": "enc-" + id}}),
		event("content_block_delta", map[string]any{"index": 0, "delta": map[string]any{"type": "signature_delta", "signature": signature}}),
		event("content_block_stop", map[string]any{"index": 0}),
		messageDelta("compaction", map[string]any{"input_tokens": 0, "output_tokens": 0, "iterations": usage}),
		event("message_stop", nil),
	)
}

type sse struct {
	name string
	data map[string]any
}

func event(name string, data map[string]any) sse {
	if data == nil {
		data = map[string]any{}
	}
	data["type"] = name
	return sse{name, data}
}

func messageStart(id string, inputTokens int64) sse {
	return event("message_start", map[string]any{"message": map[string]any{
		"id": id, "type": "message", "role": "assistant", "model": "claude-test", "content": []any{},
		"usage": map[string]any{"input_tokens": inputTokens, "output_tokens": 0},
	}})
}

func messageDelta(stopReason string, usage map[string]any) sse {
	return event("message_delta", map[string]any{"delta": map[string]any{"stop_reason": stopReason}, "usage": usage})
}

func body(events ...sse) string {
	var sb strings.Builder
	for _, e := range events {
		data, _ := json.Marshal(e.data)
		fmt.Fprintf(&sb, "event: %s\ndata: %s\n\n", e.name, data)
	}
	return sb.String()
}
