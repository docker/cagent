package dmr

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	latest "github.com/docker/cagent/pkg/config/v2"
)

func TestNewClientWithExplicitBaseURL(t *testing.T) {
	cfg := &latest.ModelConfig{
		Provider: "dmr",
		Model:    "ai/qwen3",
		BaseURL:  "https://custom.example.com:8080/api/v1",
	}

	client, err := NewClient(t.Context(), cfg)
	require.NoError(t, err)
	assert.Equal(t, "https://custom.example.com:8080/api/v1", client.baseURL)
}

func TestNewClientWithWrongType(t *testing.T) {
	cfg := &latest.ModelConfig{
		Provider: "openai",
		Model:    "gpt-4",
	}

	_, err := NewClient(t.Context(), cfg)
	require.Error(t, err)
}

func TestBuildDockerConfigureArgs(t *testing.T) {
	args := buildDockerModelConfigureArgs("ai/qwen3:14B-Q6_K", 8192, []string{"--temp", "0.7", "--top-p", "0.9"})

	assert.Equal(t, []string{"model", "configure", "--context-size=8192", "ai/qwen3:14B-Q6_K", "--", "--temp", "0.7", "--top-p", "0.9"}, args)
}

func TestBuildRuntimeFlagsFromModelConfig_LlamaCpp(t *testing.T) {
	flags := buildRuntimeFlagsFromModelConfig("llama.cpp", &latest.ModelConfig{
		Temperature:      0.6,
		TopP:             0.95,
		FrequencyPenalty: 0.2,
		PresencePenalty:  0.1,
	})

	assert.Equal(t, []string{"--temp", "0.6", "--top-p", "0.95", "--frequency-penalty", "0.2", "--presence-penalty", "0.1"}, flags)
}

func TestIntegrateFlagsWithProviderOptsOrder(t *testing.T) {
	cfg := &latest.ModelConfig{
		Temperature: 0.6,
		TopP:        0.9,
		MaxTokens:   4096,
		ProviderOpts: map[string]any{
			"runtime_flags": []string{"--threads", "6"},
		},
	}
	// derive config flags first, then merge provider opts (simulating NewClient path)
	derived := buildRuntimeFlagsFromModelConfig("llama.cpp", cfg)
	// provider opts should be appended after derived flags so they can override by order
	merged := append(derived, []string{"--threads", "6"}...)

	args := buildDockerModelConfigureArgs("ai/qwen3:14B-Q6_K", cfg.MaxTokens, merged)
	assert.Equal(t, []string{"model", "configure", "--context-size=4096", "ai/qwen3:14B-Q6_K", "--", "--temp", "0.6", "--top-p", "0.9", "--threads", "6"}, args)
}

func TestMergeRuntimeFlagsPreferUser_WarnsAndPrefersUser(t *testing.T) {
	// Derived suggests temp/top-p, user overrides both and adds threads
	derived := []string{"--temp", "0.5", "--top-p", "0.8"}
	user := []string{"--temp", "0.7", "--threads", "8"}

	merged, warnings := mergeRuntimeFlagsPreferUser(derived, user)

	// Expect 1 warnings for --temp overriding
	require.Len(t, warnings, 1)

	// Derived conflicting flags should be dropped, user ones kept and appended
	assert.Equal(t, []string{"--top-p", "0.8", "--temp", "0.7", "--threads", "8"}, merged)
}
