package openai

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/config/latest"
	"github.com/docker/docker-agent/pkg/environment"
	"github.com/docker/docker-agent/pkg/modelerrors"
)

// TestCopilotEndpointSelection verifies that the GitHub Copilot provider
// auto-selects the Responses API for models that require it (gpt-5, Codex,
// ...), while keeping older / chat-only models on /chat/completions.
//
// GitHub Copilot proxies the same OpenAI models and rejects the newer ones on
// /chat/completions with a 400 — see
// https://github.com/docker/docker-agent/issues/2885.
func TestCopilotEndpointSelection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider string
		model    string
		wantPath string
	}{
		{
			name:     "copilot gpt-5 routes to responses",
			provider: "github-copilot",
			model:    "gpt-5",
			wantPath: "/responses",
		},
		{
			name:     "copilot codex routes to responses",
			provider: "github-copilot",
			model:    "gpt-5.3-codex",
			wantPath: "/responses",
		},
		{
			name:     "copilot gpt-4o stays on chat completions",
			provider: "github-copilot",
			model:    "gpt-4o",
			wantPath: "/chat/completions",
		},
		{
			name:     "copilot gemini chat model stays on chat completions",
			provider: "github-copilot",
			model:    "gemini-3.1-pro-preview",
			wantPath: "/chat/completions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var (
				gotPath string
				mu      sync.Mutex
			)

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				gotPath = r.URL.Path
				mu.Unlock()
				if r.URL.Path == "/responses" {
					writeResponsesSSEResponse(w)
				} else {
					writeSSEResponse(w)
				}
			}))
			defer server.Close()

			cfg := &latest.ModelConfig{
				Provider: tt.provider,
				Model:    tt.model,
				BaseURL:  server.URL,
				TokenKey: "GITHUB_TOKEN",
			}

			env := environment.NewMapEnvProvider(map[string]string{
				"GITHUB_TOKEN": "ghp_secret",
			})

			client, err := NewClient(t.Context(), cfg, env)
			require.NoError(t, err)

			stream, err := client.CreateChatCompletionStream(
				t.Context(),
				[]chat.Message{{Role: chat.MessageRoleUser, Content: "hi"}},
				nil,
			)
			require.NoError(t, err)
			defer stream.Close()

			for {
				if _, err := stream.Recv(); err != nil {
					break
				}
			}

			mu.Lock()
			defer mu.Unlock()
			assert.Equal(t, tt.wantPath, gotPath)
		})
	}
}

// TestCopilotExplicitAPITypeWins verifies that an explicit api_type in
// provider_opts overrides the auto-selection.
func TestCopilotExplicitAPITypeWins(t *testing.T) {
	t.Parallel()

	var (
		gotPath string
		mu      sync.Mutex
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotPath = r.URL.Path
		mu.Unlock()
		writeSSEResponse(w)
	}))
	defer server.Close()

	// gpt-5 would normally auto-select responses, but the explicit
	// chat-completions api_type must take precedence.
	cfg := &latest.ModelConfig{
		Provider: "github-copilot",
		Model:    "gpt-5",
		BaseURL:  server.URL,
		TokenKey: "GITHUB_TOKEN",
		ProviderOpts: map[string]any{
			"api_type": "openai_chatcompletions",
		},
	}

	env := environment.NewMapEnvProvider(map[string]string{
		"GITHUB_TOKEN": "ghp_secret",
	})

	client, err := NewClient(t.Context(), cfg, env)
	require.NoError(t, err)

	stream, err := client.CreateChatCompletionStream(
		t.Context(),
		[]chat.Message{{Role: chat.MessageRoleUser, Content: "hi"}},
		nil,
	)
	require.NoError(t, err)
	defer stream.Close()

	for {
		if _, err := stream.Recv(); err != nil {
			break
		}
	}

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "/chat/completions", gotPath)
}

func TestDirectClientClassifiesErrorsWithoutExposingProviderDetails(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, body string
		overflow   bool
	}{
		{"plain text", "model is not supported by this endpoint", false},
		{"detail", `{"detail":"input exceeds the context window"}`, true},
		{"private message", `{"error":{"message":"Invalid API key: random-opaque-production-password"}}`, false},
		{"string error", `{"error":"input exceeds the context window"}`, true},
		{"standard error", `{"error":{"message":"maximum context length exceeded","code":"context_length_exceeded"}}`, true},
		{"long message", `{"error":{"message":"` + strings.Repeat("request rejected ", 30) + `input exceeds the context window"}}`, true},
	} {
		for _, apiType := range []string{"openai_chatcompletions", "openai_responses"} {
			t.Run(tc.name+"/"+apiType, func(t *testing.T) {
				t.Parallel()
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(tc.body))
				}))
				defer server.Close()

				cfg := &latest.ModelConfig{
					Provider:     "github-copilot",
					Model:        "gpt-4o",
					BaseURL:      server.URL,
					TokenKey:     "GITHUB_TOKEN",
					ProviderOpts: map[string]any{"api_type": apiType},
				}
				env := environment.NewMapEnvProvider(map[string]string{"GITHUB_TOKEN": "ghp_secret"})
				client, err := NewClient(t.Context(), cfg, env)
				require.NoError(t, err)

				stream, err := client.CreateChatCompletionStream(t.Context(), []chat.Message{{Role: chat.MessageRoleUser, Content: "hi"}}, nil)
				require.NoError(t, err)
				defer stream.Close()

				_, streamErr := stream.Recv()
				require.Error(t, streamErr)
				assert.Contains(t, streamErr.Error(), "HTTP 400")
				assert.NotContains(t, streamErr.Error(), "random-opaque-production-password")
				assert.NotContains(t, streamErr.Error(), "input exceeds the context window")
				assert.NotContains(t, streamErr.Error(), "model is not supported")
				assert.Equal(t, tc.overflow, modelerrors.IsContextOverflowError(streamErr))
			})
		}
	}
}
