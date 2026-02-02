package dmr

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/config/latest"
)

func TestNewClientWithExplicitBaseURL(t *testing.T) {
	t.Parallel()

	cfg := &latest.ModelConfig{
		Provider: "dmr",
		Model:    "ai/qwen3",
		BaseURL:  "https://custom.example.com:8080/api/v1",
	}

	client, err := NewClient(t.Context(), cfg)
	require.NoError(t, err)
	assert.Equal(t, "https://custom.example.com:8080/api/v1", client.baseURL)
}

func TestNewClientReturnsErrNotInstalledWhenDockerModelUnsupported(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping docker CLI shim test on Windows")
	}

	tempDir := t.TempDir()
	dockerPath := filepath.Join(tempDir, "docker")
	script := "#!/bin/sh\n" +
		"printf 'unknown flag: --json\\n\\nUsage:  docker [OPTIONS] COMMAND [ARG...]\\n\\nRun '\\''docker --help'\\'' for more information\\n' >&2\n" +
		"exit 1\n"
	require.NoError(t, os.WriteFile(dockerPath, []byte(script), 0o755))

	t.Setenv("PATH", tempDir)
	t.Setenv("MODEL_RUNNER_HOST", "")

	cfg := &latest.ModelConfig{
		Provider: "dmr",
		Model:    "ai/qwen3",
	}

	_, err := NewClient(t.Context(), cfg)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrNotInstalled)
}

func TestGetDMRFallbackURLs(t *testing.T) {
	t.Parallel()

	t.Run("inside container", func(t *testing.T) {
		t.Parallel()

		urls := getDMRFallbackURLs(true)

		// Should return 3 container-specific fallback URLs
		require.Len(t, urls, 3)

		// Verify the expected URLs in order (container-specific endpoints)
		assert.Equal(t, "http://model-runner.docker.internal/engines/v1/", urls[0])
		assert.Equal(t, "http://host.docker.internal:12434/engines/v1/", urls[1])
		assert.Equal(t, "http://172.17.0.1:12434/engines/v1/", urls[2])
	})

	t.Run("on host", func(t *testing.T) {
		t.Parallel()

		urls := getDMRFallbackURLs(false)

		// Should return 1 host-specific fallback URL
		require.Len(t, urls, 1)

		// Verify localhost is the only fallback on host
		assert.Equal(t, "http://127.0.0.1:12434/engines/v1/", urls[0])
	})
}

func TestDMRConnectivity(t *testing.T) {
	t.Parallel()

	t.Run("reachable endpoint", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/models", r.URL.Path)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[]}`))
		}))
		defer server.Close()

		result := testDMRConnectivity(t.Context(), server.Client(), server.URL+"/")
		assert.True(t, result)
	})

	t.Run("reachable endpoint with error response", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		// Should still return true because server is reachable
		result := testDMRConnectivity(t.Context(), server.Client(), server.URL+"/")
		assert.True(t, result)
	})

	t.Run("unreachable endpoint", func(t *testing.T) {
		t.Parallel()

		// Use a port that's unlikely to have anything listening
		result := testDMRConnectivity(t.Context(), &http.Client{}, "http://127.0.0.1:59999/")
		assert.False(t, result)
	})
}

func TestNewClientWithWrongType(t *testing.T) {
	t.Parallel()

	cfg := &latest.ModelConfig{
		Provider: "openai",
		Model:    "gpt-4",
	}

	_, err := NewClient(t.Context(), cfg)
	require.Error(t, err)
}

func TestBuildConfigureURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		baseURL  string
		expected string
	}{
		{
			name:     "standard engines path",
			baseURL:  "http://127.0.0.1:12434/engines/v1/",
			expected: "http://127.0.0.1:12434/engines/_configure",
		},
		{
			name:     "standard engines path without trailing slash",
			baseURL:  "http://127.0.0.1:12434/engines/v1",
			expected: "http://127.0.0.1:12434/engines/_configure",
		},
		{
			name:     "Docker Desktop experimental prefix",
			baseURL:  "http://_/exp/vDD4.40/engines/v1",
			expected: "http://_/exp/vDD4.40/engines/_configure",
		},
		{
			name:     "Docker Desktop experimental prefix with trailing slash",
			baseURL:  "http://_/exp/vDD4.40/engines/v1/",
			expected: "http://_/exp/vDD4.40/engines/_configure",
		},
		{
			name:     "backend-scoped path",
			baseURL:  "http://127.0.0.1:12434/engines/llama.cpp/v1/",
			expected: "http://127.0.0.1:12434/engines/llama.cpp/_configure",
		},
		{
			name:     "container internal host",
			baseURL:  "http://model-runner.docker.internal/engines/v1/",
			expected: "http://model-runner.docker.internal/engines/_configure",
		},
		{
			name:     "custom port",
			baseURL:  "http://localhost:8080/engines/v1/",
			expected: "http://localhost:8080/engines/_configure",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := buildConfigureURL(tt.baseURL)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuildConfigureRequest(t *testing.T) {
	t.Parallel()

	t.Run("with all options", func(t *testing.T) {
		t.Parallel()
		specOpts := &speculativeDecodingOpts{
			draftModel:     "ai/qwen3:1B",
			numTokens:      5,
			acceptanceRate: 0.8,
		}
		contextSize := int64(8192)

		req := buildConfigureRequest("ai/qwen3:14B-Q6_K", &contextSize, []string{"--temp", "0.7", "--top-p", "0.9"}, specOpts)

		assert.Equal(t, "ai/qwen3:14B-Q6_K", req.Model)
		require.NotNil(t, req.ContextSize)
		assert.Equal(t, int32(8192), *req.ContextSize)
		assert.Equal(t, []string{"--temp", "0.7", "--top-p", "0.9"}, req.RuntimeFlags)
		require.NotNil(t, req.Speculative)
		assert.Equal(t, "ai/qwen3:1B", req.Speculative.DraftModel)
		assert.Equal(t, 5, req.Speculative.NumTokens)
		assert.InEpsilon(t, 0.8, req.Speculative.MinAcceptanceRate, 0.001)
	})

	t.Run("without speculative options", func(t *testing.T) {
		t.Parallel()
		contextSize := int64(4096)

		req := buildConfigureRequest("ai/qwen3:14B-Q6_K", &contextSize, []string{"--threads", "8"}, nil)

		assert.Equal(t, "ai/qwen3:14B-Q6_K", req.Model)
		require.NotNil(t, req.ContextSize)
		assert.Equal(t, int32(4096), *req.ContextSize)
		assert.Equal(t, []string{"--threads", "8"}, req.RuntimeFlags)
		assert.Nil(t, req.Speculative)
	})

	t.Run("without context size", func(t *testing.T) {
		t.Parallel()
		specOpts := &speculativeDecodingOpts{
			draftModel: "ai/qwen3:1B",
			numTokens:  5,
		}

		req := buildConfigureRequest("ai/qwen3:14B-Q6_K", nil, nil, specOpts)

		assert.Equal(t, "ai/qwen3:14B-Q6_K", req.Model)
		assert.Nil(t, req.ContextSize)
		assert.Nil(t, req.RuntimeFlags)
		require.NotNil(t, req.Speculative)
		assert.Equal(t, "ai/qwen3:1B", req.Speculative.DraftModel)
		assert.Equal(t, 5, req.Speculative.NumTokens)
	})

	t.Run("minimal config", func(t *testing.T) {
		t.Parallel()
		req := buildConfigureRequest("ai/qwen3:14B-Q6_K", nil, nil, nil)

		assert.Equal(t, "ai/qwen3:14B-Q6_K", req.Model)
		assert.Nil(t, req.ContextSize)
		assert.Nil(t, req.RuntimeFlags)
		assert.Nil(t, req.Speculative)
	})
}

func TestConfigureModelViaAPI(t *testing.T) {
	t.Parallel()

	t.Run("successful configuration", func(t *testing.T) {
		t.Parallel()

		var receivedRequest configureRequest
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "/engines/_configure", r.URL.Path)
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

			body, err := io.ReadAll(r.Body)
			if !assert.NoError(t, err) {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			err = json.Unmarshal(body, &receivedRequest)
			if !assert.NoError(t, err) {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}

			w.WriteHeader(http.StatusAccepted)
		}))
		defer server.Close()

		baseURL := server.URL + "/engines/v1/"
		contextSize := int64(8192)
		specOpts := &speculativeDecodingOpts{
			draftModel:     "ai/qwen3:1B",
			numTokens:      5,
			acceptanceRate: 0.8,
		}

		err := configureModel(t.Context(), server.Client(), baseURL, "ai/qwen3:14B", &contextSize, []string{"--temp", "0.7"}, specOpts)
		require.NoError(t, err)

		// Verify request body
		assert.Equal(t, "ai/qwen3:14B", receivedRequest.Model)
		require.NotNil(t, receivedRequest.ContextSize)
		assert.Equal(t, int32(8192), *receivedRequest.ContextSize)
		assert.Equal(t, []string{"--temp", "0.7"}, receivedRequest.RuntimeFlags)
		require.NotNil(t, receivedRequest.Speculative)
		assert.Equal(t, "ai/qwen3:1B", receivedRequest.Speculative.DraftModel)
		assert.Equal(t, 5, receivedRequest.Speculative.NumTokens)
		assert.InEpsilon(t, 0.8, receivedRequest.Speculative.MinAcceptanceRate, 0.001)
	})

	t.Run("server returns error", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("internal error"))
		}))
		defer server.Close()

		baseURL := server.URL + "/engines/v1/"
		err := configureModel(t.Context(), server.Client(), baseURL, "ai/qwen3:14B", nil, nil, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "500")
		assert.Contains(t, err.Error(), "internal error")
	})

	t.Run("server returns conflict", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte("runner already active"))
		}))
		defer server.Close()

		baseURL := server.URL + "/engines/v1/"
		err := configureModel(t.Context(), server.Client(), baseURL, "ai/qwen3:14B", nil, nil, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "409")
		assert.Contains(t, err.Error(), "runner already active")
	})
}

func TestBuildRuntimeFlagsFromModelConfig_LlamaCpp(t *testing.T) {
	t.Parallel()

	flags := buildRuntimeFlagsFromModelConfig("llama.cpp", &latest.ModelConfig{
		Temperature:      floatPtr(0.6),
		TopP:             floatPtr(0.95),
		FrequencyPenalty: floatPtr(0.2),
		PresencePenalty:  floatPtr(0.1),
	})

	assert.Equal(t, []string{"--temp", "0.6", "--top-p", "0.95", "--frequency-penalty", "0.2", "--presence-penalty", "0.1"}, flags)
}

func TestIntegrateFlagsWithProviderOptsOrder(t *testing.T) {
	t.Parallel()

	cfg := &latest.ModelConfig{
		Temperature: floatPtr(0.6),
		TopP:        floatPtr(0.9),
		MaxTokens:   int64Ptr(4096),
		ProviderOpts: map[string]any{
			"runtime_flags": []string{"--threads", "6"},
		},
	}
	// derive config flags first, then merge provider opts (simulating NewClient path)
	derived := buildRuntimeFlagsFromModelConfig("llama.cpp", cfg)
	// provider opts should be appended after derived flags so they can override by order
	merged := append(derived, []string{"--threads", "6"}...)

	req := buildConfigureRequest("ai/qwen3:14B-Q6_K", cfg.MaxTokens, merged, nil)
	assert.Equal(t, "ai/qwen3:14B-Q6_K", req.Model)
	require.NotNil(t, req.ContextSize)
	assert.Equal(t, int32(4096), *req.ContextSize)
	assert.Equal(t, []string{"--temp", "0.6", "--top-p", "0.9", "--threads", "6"}, req.RuntimeFlags)
}

func TestMergeRuntimeFlagsPreferUser_WarnsAndPrefersUser(t *testing.T) {
	t.Parallel()

	// Derived suggests temp/top-p, user overrides both and adds threads
	derived := []string{"--temp", "0.5", "--top-p", "0.8"}
	user := []string{"--temp", "0.7", "--threads", "8"}

	merged, warnings := mergeRuntimeFlagsPreferUser(derived, user)

	// Expect 1 warnings for --temp overriding
	require.Len(t, warnings, 1)

	// Derived conflicting flags should be dropped, user ones kept and appended
	assert.Equal(t, []string{"--top-p", "0.8", "--temp", "0.7", "--threads", "8"}, merged)
}

func floatPtr(f float64) *float64 {
	return &f
}

func int64Ptr(i int64) *int64 {
	return &i
}

func TestParseDMRProviderOptsWithSpeculativeDecoding(t *testing.T) {
	t.Parallel()

	cfg := &latest.ModelConfig{
		MaxTokens: int64Ptr(4096),
		ProviderOpts: map[string]any{
			"speculative_draft_model":     "ai/qwen3:1B",
			"speculative_num_tokens":      "5",
			"speculative_acceptance_rate": "0.75",
			"runtime_flags":               []string{"--threads", "8"},
		},
	}

	contextSize, runtimeFlags, specOpts := parseDMRProviderOpts(cfg)

	assert.Equal(t, int64(4096), *contextSize)
	assert.Equal(t, []string{"--threads", "8"}, runtimeFlags)
	require.NotNil(t, specOpts)
	assert.Equal(t, "ai/qwen3:1B", specOpts.draftModel)
	assert.Equal(t, 5, specOpts.numTokens)
	assert.InEpsilon(t, 0.75, specOpts.acceptanceRate, 0.001)
}

func TestParseDMRProviderOptsWithoutSpeculativeDecoding(t *testing.T) {
	t.Parallel()

	cfg := &latest.ModelConfig{
		MaxTokens: int64Ptr(4096),
		ProviderOpts: map[string]any{
			"runtime_flags": []string{"--threads", "8"},
		},
	}

	contextSize, runtimeFlags, specOpts := parseDMRProviderOpts(cfg)

	assert.Equal(t, int64(4096), *contextSize)
	assert.Equal(t, []string{"--threads", "8"}, runtimeFlags)
	assert.Nil(t, specOpts)
}

func TestConfigureRequestJSONSerialization(t *testing.T) {
	t.Parallel()

	t.Run("full request serializes correctly", func(t *testing.T) {
		t.Parallel()
		contextSize := int32(8192)
		req := configureRequest{
			Model:        "ai/qwen3:14B",
			ContextSize:  &contextSize,
			RuntimeFlags: []string{"--temp", "0.7"},
			Speculative: &speculativeDecodingRequest{
				DraftModel:        "ai/qwen3:1B",
				NumTokens:         5,
				MinAcceptanceRate: 0.8,
			},
		}

		data, err := json.Marshal(req)
		require.NoError(t, err)

		var parsed map[string]any
		err = json.Unmarshal(data, &parsed)
		require.NoError(t, err)

		assert.Equal(t, "ai/qwen3:14B", parsed["model"])
		assert.InEpsilon(t, float64(8192), parsed["context-size"].(float64), 0.001)
		assert.Equal(t, []any{"--temp", "0.7"}, parsed["runtime-flags"])

		spec, ok := parsed["speculative"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "ai/qwen3:1B", spec["draft_model"])
		assert.InEpsilon(t, float64(5), spec["num_tokens"].(float64), 0.001)
		assert.InEpsilon(t, 0.8, spec["min_acceptance_rate"].(float64), 0.001)
	})

	t.Run("minimal request omits nil fields", func(t *testing.T) {
		t.Parallel()
		req := configureRequest{
			Model: "ai/qwen3:14B",
		}

		data, err := json.Marshal(req)
		require.NoError(t, err)

		var parsed map[string]any
		err = json.Unmarshal(data, &parsed)
		require.NoError(t, err)

		assert.Equal(t, "ai/qwen3:14B", parsed["model"])
		_, hasContextSize := parsed["context-size"]
		assert.False(t, hasContextSize, "context-size should be omitted when nil")
		_, hasRuntimeFlags := parsed["runtime-flags"]
		assert.False(t, hasRuntimeFlags, "runtime-flags should be omitted when nil")
		_, hasSpeculative := parsed["speculative"]
		assert.False(t, hasSpeculative, "speculative should be omitted when nil")
	})
}
