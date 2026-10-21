package providers_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/docker/docker-agent/pkg/config/latest"
)

// Server-side fallback is a beta of the Messages API: the request goes
// through the beta client with the July header, which accepts both forms.
func TestServerSideFallbackRequests(t *testing.T) {
	t.Parallel()

	env := map[string]string{"ANTHROPIC_API_KEY": "test-key"}
	for name, tc := range map[string]struct {
		fallbacks any
		want      any
	}{
		"default": {fallbacks: "default", want: "default"},
		"list":    {fallbacks: []any{"claude-sonnet-4-6"}, want: []any{map[string]any{"model": "claude-sonnet-4-6"}}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			req := captureFinalRequest(t, latest.ModelConfig{
				Provider: "anthropic", Model: "claude-opus-4-8",
				ProviderOpts: map[string]any{"fallbacks": tc.fallbacks},
			}, env)
			assert.Equal(t, "beta=true", req.query, "beta Messages API")
			assert.Contains(t, strings.Join(req.header.Values("anthropic-beta"), ","), "server-side-fallback-2026-07-01")
			assert.Equal(t, tc.want, req.body["fallbacks"])
		})
	}

	t.Run("none", func(t *testing.T) {
		t.Parallel()
		req := captureFinalRequest(t, latest.ModelConfig{Provider: "anthropic", Model: "claude-opus-4-8"}, env)
		assert.NotContains(t, req.body, "fallbacks")
		for _, beta := range req.header.Values("anthropic-beta") {
			assert.NotContains(t, beta, "server-side-fallback")
		}
	})
}
