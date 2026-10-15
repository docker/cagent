//go:build !js && !docker_agent_no_openai

//nolint:staticcheck // Sampling is supported during the MCP deprecation window.
package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/agent"
	"github.com/docker/docker-agent/pkg/config/latest"
	"github.com/docker/docker-agent/pkg/environment"
	"github.com/docker/docker-agent/pkg/model/provider"
	"github.com/docker/docker-agent/pkg/model/provider/openai"
	"github.com/docker/docker-agent/pkg/model/provider/options"
	"github.com/docker/docker-agent/pkg/team"
)

func TestSamplingHandlersHonorRoutedTokenCap(t *testing.T) {
	t.Parallel()
	for _, withTools := range []bool{false, true} {
		for _, message := range []string{"hello", "unrelated"} {
			t.Run(fmt.Sprintf("tools=%t/%s", withTools, message), func(t *testing.T) {
				t.Parallel()
				var payload map[string]any
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					assert.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"sampled\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
				}))
				defer server.Close()
				registry := provider.NewRegistry(map[string]provider.Factory{"openai_chatcompletions": func(ctx context.Context, cfg *latest.ModelConfig, env environment.Provider, opts ...options.Opt) (provider.Provider, error) {
					return openai.NewClient(ctx, cfg, env, opts...)
				}})
				cfg := &latest.ModelConfig{Provider: "custom", Model: "gpt-5.6", MaxTokens: new(int64(4096)), Routing: []latest.RoutingRule{{Model: "target", Examples: []string{"hello"}}}}
				models := map[string]latest.ModelConfig{"target": {Provider: "custom", Model: "gpt-5.6", MaxTokens: new(int64(8192))}}
				p, err := registry.NewWithModels(t.Context(), cfg, models, environment.NewMapEnvProvider(map[string]string{"KEY": "fake"}), options.WithMaxTokens(4096), options.WithProviders(map[string]latest.ProviderConfig{"custom": {BaseURL: server.URL, TokenKey: "KEY", APIType: "openai_chatcompletions", MaxTokens: new(int64(16384)), ThinkingBudget: &latest.ThinkingBudget{Effort: "high"}}}))
				require.NoError(t, err)
				rt, err := NewLocalRuntime(t.Context(), team.New(team.WithAgents(agent.New("root", "", agent.WithModel(p)))), WithCurrentAgent("root"), WithModelStore(mockModelStore{}))
				require.NoError(t, err)
				if withTools {
					_, err = rt.samplingWithToolsHandler(t.Context(), &mcp.CreateMessageWithToolsParams{MaxTokens: 512, Messages: []*mcp.SamplingMessageV2{{Role: "user", Content: []mcp.Content{&mcp.TextContent{Text: message}}}}})
				} else {
					_, err = rt.samplingHandler(t.Context(), &mcp.CreateMessageParams{MaxTokens: 512, Messages: []*mcp.SamplingMessage{{Role: "user", Content: &mcp.TextContent{Text: message}}}})
				}
				require.NoError(t, err)
				assert.InDelta(t, float64(512), payload["max_completion_tokens"], 0)
				assert.Equal(t, "none", payload["reasoning_effort"])
			})
		}
	}
}
