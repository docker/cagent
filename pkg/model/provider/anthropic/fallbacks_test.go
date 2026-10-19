package anthropic

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/config/latest"
	"github.com/docker/docker-agent/pkg/environment"
)

func TestFallbackModels(t *testing.T) {
	t.Parallel()
	t.Run("absent", func(t *testing.T) {
		assert.Nil(t, fallbackModels(nil))
		assert.Nil(t, fallbackModels(map[string]any{"top_k": 40}))
	})

	t.Run("empty list", func(t *testing.T) {
		assert.Nil(t, fallbackModels(map[string]any{"fallbacks": []any{}}))
	})

	t.Run("invalid type", func(t *testing.T) {
		assert.Nil(t, fallbackModels(map[string]any{"fallbacks": "claude-opus-4-8"}))
	})

	t.Run("configured", func(t *testing.T) {
		assert.Equal(t,
			[]string{"claude-opus-4-8", "claude-sonnet-4-6"},
			fallbackModels(map[string]any{"fallbacks": []any{"claude-opus-4-8", "claude-sonnet-4-6"}}))
	})
}

func TestValidateFallbacks(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		model     string
		fallbacks any
		wantModel string
	}{
		{name: "5.x primary", model: "claude-fable-5-1", fallbacks: []any{"claude-opus-4-8"}, wantModel: "claude-fable-5-1"},
		{name: "legacy primary, 5.x target", model: "claude-opus-4-8", fallbacks: []any{"claude-sonnet-5-5"}, wantModel: "claude-sonnet-5-5"},
		{name: "gateway target", model: "claude-opus-4-8", fallbacks: []any{"vendor/claude-opus-5-5"}, wantModel: "vendor/claude-opus-5-5"},
		{name: "vertex target", model: "claude-opus-4-8", fallbacks: []any{"claude-sonnet-5-5@20261001"}, wantModel: "claude-sonnet-5-5@20261001"},
		{name: "absent", model: "claude-fable-5-1"},
		{name: "empty", model: "claude-fable-5-1", fallbacks: []any{}},
		{name: "no 5.x model", model: "claude-opus-4-8", fallbacks: []any{"claude-opus-4-7"}},
		{name: "malformed is ignored", model: "claude-fable-5-1", fallbacks: "not-a-list"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := &latest.ModelConfig{Provider: "anthropic", Model: tc.model}
			if tc.fallbacks != nil {
				cfg.ProviderOpts = map[string]any{"fallbacks": tc.fallbacks}
			}
			err := validateFallbacks(cfg)
			if tc.wantModel == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), `"`+tc.wantModel+`"`)
			assert.Contains(t, err.Error(), "issues/4584")
		})
	}
}

func TestNewClient_RejectsClaude5Fallbacks(t *testing.T) {
	t.Parallel()

	cfg := &latest.ModelConfig{Provider: "anthropic", Model: "claude-opus-4-8", ProviderOpts: map[string]any{"fallbacks": []any{"claude-fable-5"}}}
	_, err := NewClient(t.Context(), cfg, environment.NewMapEnvProvider(map[string]string{"ANTHROPIC_API_KEY": "k"}))
	require.ErrorContains(t, err, "issues/4584")
}
