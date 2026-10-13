//go:build !js && !docker_agent_no_openai

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/config/latest"
	"github.com/docker/docker-agent/pkg/environment"
	"github.com/docker/docker-agent/pkg/httpclient"
	"github.com/docker/docker-agent/pkg/model/provider/options"
	"github.com/docker/docker-agent/pkg/model/provider/vertexai"
)

type cloneRoundTripper func(*http.Request) (*http.Response, error)

func (f cloneRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestCloneVertexNativeDispatch(t *testing.T) {
	t.Run("ADC", func(t *testing.T) {
		tokens := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/", r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"access_token":"fake-adc","token_type":"Bearer","expires_in":3600}`)
		}))
		defer tokens.Close()
		wireCalls := 0
		wrapper := options.WithHTTPTransportWrapper(func(http.RoundTripper) http.RoundTripper {
			return cloneRoundTripper(func(req *http.Request) (*http.Response, error) {
				wireCalls++
				assert.Equal(t, "us-east5-aiplatform.googleapis.com", req.URL.Host)
				assert.Equal(t, "/v1/projects/test-project/locations/us-east5/publishers/anthropic/models/claude-sonnet-4-6:streamRawPredict", req.URL.Path)
				assert.Equal(t, "Bearer fake-adc", req.Header.Get("Authorization"))
				assert.Empty(t, req.Header.Get("X-Api-Key"))
				body, err := io.ReadAll(req.Body)
				require.NoError(t, err)
				assert.Contains(t, string(body), `"anthropic_version":"vertex-2023-10-16"`)
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")), Request: req}, nil
			})
		})
		file := filepath.Join(t.TempDir(), "adc.json")
		require.NoError(t, os.WriteFile(file, fmt.Appendf(nil, `{"type":"authorized_user","client_id":"offline","client_secret":"offline","refresh_token":"offline","token_uri":%q}`, tokens.URL), 0o600))
		t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", file)
		googleCalls := 0
		registry := NewRegistry(map[string]Factory{"google": func(ctx context.Context, cfg *latest.ModelConfig, env environment.Provider, opts ...options.Opt) (Provider, error) {
			googleCalls++
			return vertexai.NewClient(ctx, cfg, env, opts...)
		}, "anthropic": func(context.Context, *latest.ModelConfig, environment.Provider, ...options.Opt) (Provider, error) {
			t.Fatal("direct Anthropic dispatch")
			return nil, nil
		}})
		original, err := registry.New(t.Context(), vertexCloneConfig("us-east5"), environment.NewMapEnvProvider(map[string]string{"ANTHROPIC_API_KEY": "must-not-leak"}), wrapper)
		require.NoError(t, err)
		clone, err := Clone(t.Context(), original, options.WithNoThinking(), options.WithMaxTokens(128))
		require.NoError(t, err)
		cfg := clone.BaseConfig()
		assert.Equal(t, "anthropic", cfg.ModelConfig.Provider)
		assert.Equal(t, "google", cfg.CloneSource().Provider)
		assert.Equal(t, "test-project", cfg.CloneSource().ProviderOpts["project"])
		assert.Equal(t, "us-east5", cfg.CloneSource().ProviderOpts["location"])
		assert.True(t, cfg.ModelOptions.NoThinking())
		assert.Equal(t, 2, googleCalls)
		for _, p := range []Provider{original, clone} {
			stream, err := p.CreateChatCompletionStream(t.Context(), []chat.Message{{Role: chat.MessageRoleUser, Content: "hello"}}, nil)
			require.NoError(t, err)
			for {
				_, err = stream.Recv()
				if err != nil {
					break
				}
			}
			stream.Close()
		}
		assert.Equal(t, 2, wireCalls)
	})
	t.Run("contextual token source", func(t *testing.T) {
		t.Parallel()
		for _, location := range []string{"us-east5", "global", "eu"} {
			t.Run(location, func(t *testing.T) {
				t.Parallel()
				var sessions []string
				source := func(ctx context.Context) (string, error) {
					sessions = append(sessions, httpclient.SessionIDFromContext(ctx))
					return "oauth-token", ctx.Err()
				}
				calls := 0
				registry := NewRegistry(map[string]Factory{"google": func(ctx context.Context, cfg *latest.ModelConfig, env environment.Provider, opts ...options.Opt) (Provider, error) {
					calls++
					if calls == 1 {
						return vertexai.NewClientWithTokenSource(ctx, cfg, env, source, opts...)
					}
					return vertexai.NewClient(ctx, cfg, env, opts...)
				}, "anthropic": func(context.Context, *latest.ModelConfig, environment.Provider, ...options.Opt) (Provider, error) {
					t.Fatal("direct Anthropic dispatch")
					return nil, nil
				}})
				wireCalls := 0
				wrapper := options.WithHTTPTransportWrapper(func(http.RoundTripper) http.RoundTripper {
					return cloneRoundTripper(func(req *http.Request) (*http.Response, error) {
						wireCalls++
						host := location + "-aiplatform.googleapis.com"
						if location == "global" {
							host = "aiplatform.googleapis.com"
						}
						if location == "eu" {
							host = "aiplatform.eu.rep.googleapis.com"
						}
						assert.Equal(t, host, req.URL.Host)
						assert.Equal(t, "/v1/projects/test-project/locations/"+location+"/publishers/anthropic/models/claude-sonnet-4-6:streamRawPredict", req.URL.Path)
						assert.Equal(t, "Bearer oauth-token", req.Header.Get("Authorization"))
						assert.Empty(t, req.Header.Get("X-Api-Key"))
						body, err := io.ReadAll(req.Body)
						require.NoError(t, err)
						var payload map[string]any
						require.NoError(t, json.Unmarshal(body, &payload))
						assert.Equal(t, "vertex-2023-10-16", payload["anthropic_version"])
						assert.NotContains(t, payload, "model")
						assert.NotContains(t, payload, "thinking")
						assert.InDelta(t, float64(256), payload["max_tokens"], 0)
						return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")), Request: req}, nil
					})
				})
				original, err := registry.New(t.Context(), vertexCloneConfig(location), environment.NewMapEnvProvider(map[string]string{"ANTHROPIC_API_KEY": "must-not-leak"}), options.WithGateway("https://ignored-gateway.example"), wrapper)
				require.NoError(t, err)
				clone, err := Clone(t.Context(), original, options.WithNoThinking(), options.WithMaxTokens(128), options.WithStructuredOutput(nil))
				require.NoError(t, err)
				clone, err = Clone(t.Context(), clone, options.WithGeneratingTitle())
				require.NoError(t, err)
				assert.Equal(t, 3, calls)
				stream, err := clone.CreateChatCompletionStream(httpclient.ContextWithSessionID(t.Context(), "clone-session"), []chat.Message{{Role: chat.MessageRoleUser, Content: "hello"}}, nil)
				require.NoError(t, err)
				for {
					_, err = stream.Recv()
					if err != nil {
						break
					}
				}
				stream.Close()
				assert.Equal(t, 1, wireCalls)
				assert.Equal(t, []string{"", "", "", "clone-session"}, sessions)
			})
		}
	})
}

func vertexCloneConfig(location string) *latest.ModelConfig {
	return &latest.ModelConfig{Provider: "google", Model: "claude-sonnet-4-6", BypassModelsGateway: true, ThinkingBudget: &latest.ThinkingBudget{Tokens: 2048}, ProviderOpts: map[string]any{"publisher": "anthropic", "project": "test-project", "location": location}}
}

func TestCloneModelGardenCompatibleTransportAndSiblingAuth(t *testing.T) {
	t.Parallel()
	cfg := &latest.ModelConfig{Provider: "google", Model: "meta/llama-test", ProviderOpts: map[string]any{"publisher": "meta", "project": "test-project", "location": "us-central1"}}
	var sources, wire int
	source := func(ctx context.Context) (string, error) { sources++; return "vertex-only", ctx.Err() }
	registry := NewRegistry(map[string]Factory{"google": vertexai.NewClient, "openai": openAITestFactory})
	wrapper := options.WithHTTPTransportWrapper(func(http.RoundTripper) http.RoundTripper {
		return cloneRoundTripper(func(req *http.Request) (*http.Response, error) {
			wire++
			assert.Equal(t, "us-central1-aiplatform.googleapis.com", req.URL.Host)
			assert.Equal(t, "/v1beta1/projects/test-project/locations/us-central1/endpoints/openapi/chat/completions", req.URL.Path)
			assert.Equal(t, "Bearer vertex-only", req.Header.Get("Authorization"))
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: [DONE]\n\n")), Request: req}, nil
		})
	})
	original, err := registry.New(t.Context(), cfg, environment.NewNoEnvProvider(), options.WithTokenSource(source), wrapper)
	require.NoError(t, err)
	clone, err := Clone(t.Context(), original, options.WithNoThinking())
	require.NoError(t, err)
	stream, err := clone.CreateChatCompletionStream(t.Context(), []chat.Message{{Role: chat.MessageRoleUser, Content: "hello"}}, nil)
	require.NoError(t, err)
	drainStream(t, stream)
	stream.Close()
	assert.Equal(t, 1, wire)
	assert.Positive(t, sources)
	// A leaf's stored OAuth source must not enter a router's sibling options.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		assert.Equal(t, "Bearer sibling-key", req.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	factories := NewRegistry(map[string]Factory{"google": func(context.Context, *latest.ModelConfig, environment.Provider, ...options.Opt) (Provider, error) {
		return original, nil
	}, "openai": openAITestFactory})
	router, err := factories.NewWithModels(t.Context(), &latest.ModelConfig{Provider: "openai", Model: "gpt-4o", Routing: []latest.RoutingRule{{Model: "vertex", Examples: []string{"vertex"}}}}, map[string]latest.ModelConfig{"vertex": *cfg}, environment.NewMapEnvProvider(map[string]string{"OPENAI_API_KEY": "sibling-key"}), options.WithProviders(map[string]latest.ProviderConfig{"openai": {BaseURL: server.URL, TokenKey: "OPENAI_API_KEY"}}))
	require.NoError(t, err)
	router, err = Clone(t.Context(), router, options.WithNoThinking())
	require.NoError(t, err)
	stream, err = router.CreateChatCompletionStream(t.Context(), []chat.Message{{Role: chat.MessageRoleUser, Content: "unmatched fallback request"}}, nil)
	require.NoError(t, err)
	drainStream(t, stream)
	stream.Close()
	assert.Equal(t, 1, wire, "fallback must not call the Vertex transport")
}
