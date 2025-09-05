package dmr

import (
	"reflect"
	"testing"

	latest "github.com/docker/cagent/pkg/config/v2"
)

func TestNewClientWithDefaultBaseURL(t *testing.T) {
	// No base_url provided, should use default
	cfg := &latest.ModelConfig{
		Provider: "dmr",
		Model:    "ai/qwen3",
		// BaseURL is empty, should use default
	}

	client, err := NewClient(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if client.baseURL != "http://localhost:12434/engines/llama.cpp/v1" {
		t.Errorf("Expected default baseURL to be 'http://localhost:12434/engines/llama.cpp/v1', got '%s'", client.baseURL)
	}
}

func TestNewClientWithExplicitBaseURL(t *testing.T) {
	// Explicit base_url provided, should use that
	customURL := "https://custom.example.com:8080/api/v1"
	cfg := &latest.ModelConfig{
		Provider: "dmr",
		Model:    "ai/qwen3",
		BaseURL:  customURL,
	}

	client, err := NewClient(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if client.baseURL != customURL {
		t.Errorf("Expected baseURL to be '%s', got '%s'", customURL, client.baseURL)
	}
}

func TestNewClientWithWrongType(t *testing.T) {
	// Wrong model type, should return error
	cfg := &latest.ModelConfig{
		Provider: "openai", // Wrong type
		Model:    "gpt-4",
	}

	_, err := NewClient(t.Context(), cfg)
	if err == nil {
		t.Fatal("Expected error for wrong model type, got nil")
	}
}

func TestBuildDockerConfigureArgs(t *testing.T) {
	args := buildDockerModelConfigureArgs("ai/qwen3:14B-Q6_K", 8192, []string{"--temp", "0.7", "--top-p", "0.9"})
	expected := []string{"model", "configure", "--context-size=8192", "ai/qwen3:14B-Q6_K", "--", "--temp", "0.7", "--top-p", "0.9"}
	if !reflect.DeepEqual(args, expected) {
		t.Fatalf("unexpected args.\nexpected: %#v\nactual:   %#v", expected, args)
	}
}

func TestBuildRuntimeFlagsFromModelConfig_LlamaCpp(t *testing.T) {
	cfg := &latest.ModelConfig{
		Temperature:      0.6,
		TopP:             0.95,
		FrequencyPenalty: 0.2,
		PresencePenalty:  0.1,
	}

	flags := buildRuntimeFlagsFromModelConfig("llama.cpp", cfg)

	// Order matters based on implementation
	expected := []string{"--temp", "0.6", "--top-p", "0.95", "--frequency-penalty", "0.2", "--presence-penalty", "0.1"}
	if !reflect.DeepEqual(flags, expected) {
		t.Fatalf("unexpected runtime flags.\nexpected: %#v\nactual:   %#v", expected, flags)
	}
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
	expected := []string{"model", "configure", "--context-size=4096", "ai/qwen3:14B-Q6_K", "--", "--temp", "0.6", "--top-p", "0.9", "--threads", "6"}
	if !reflect.DeepEqual(args, expected) {
		t.Fatalf("unexpected configure args.\nexpected: %#v\nactual:   %#v", expected, args)
	}
}

func TestMergeRuntimeFlagsPreferUser_WarnsAndPrefersUser(t *testing.T) {
	// Derived suggests temp/top-p, user overrides both and adds threads
	derived := []string{"--temp", "0.5", "--top-p", "0.8"}
	user := []string{"--temp", "0.7", "--threads", "8"}

	merged, warnings := mergeRuntimeFlagsPreferUser(derived, user)

	// Expect 1 warnings for --temp overriding
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning1, got %d: %#v", len(warnings), warnings)
	}
	// Derived conflicting flags should be dropped, user ones kept and appended
	expected := []string{"--top-p", "0.8", "--temp", "0.7", "--threads", "8"}
	if !reflect.DeepEqual(merged, expected) {
		t.Fatalf("unexpected merged flags.\nexpected: %#v\nactual:   %#v", expected, merged)
	}
}
