package anthropic

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/config/latest"
	"github.com/docker/docker-agent/pkg/environment"
	"github.com/docker/docker-agent/pkg/model/provider/options"
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
		{name: "default on 5.x primary", model: "claude-fable-5-1", fallbacks: "default", wantModel: "claude-fable-5-1"},
		{name: "default on legacy primary", model: "claude-opus-4-8", fallbacks: "default"},
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

func TestParseFallbacks(t *testing.T) {
	t.Parallel()

	valid := map[string]struct {
		value any
		want  fallbackSpec
	}{
		"absent":       {},
		"empty list":   {value: []any{}},
		"default":      {value: "default", want: fallbackSpec{Default: true}},
		"one model":    {value: []any{"claude-opus-4-8"}, want: fallbackSpec{Models: []string{"claude-opus-4-8"}}},
		"three models": {value: []any{"a", "b", "c"}, want: fallbackSpec{Models: []string{"a", "b", "c"}}},
		"string slice": {value: []string{"a"}, want: fallbackSpec{Models: []string{"a"}}},
	}
	for name, tc := range valid {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			opts := map[string]any{}
			if tc.value != nil {
				opts["fallbacks"] = tc.value
			}
			got, err := parseFallbacks(opts)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}

	invalid := map[string]any{
		"other string": "claude-opus-4-8",
		"four models":  []any{"a", "b", "c", "d"},
		"duplicate":    []any{"a", "a"},
		"empty entry":  []any{"a", ""},
		"non-string":   []any{"a", 1},
		"object entry": []any{map[string]any{"model": "a"}},
		"wrong type":   42,
	}
	for name, value := range invalid {
		t.Run("invalid "+name, func(t *testing.T) {
			t.Parallel()
			_, err := parseFallbacks(map[string]any{"fallbacks": value})
			require.ErrorContains(t, err, "fallbacks")
		})
	}
}

func TestValidateFallbackSpec(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		model     string
		fallbacks any
		wantErr   string
	}{
		"list":                   {model: "claude-opus-4-8", fallbacks: []any{"claude-sonnet-4-6"}},
		"default":                {model: "claude-opus-4-8", fallbacks: "default"},
		"entry equals primary":   {model: "claude-opus-4-8", fallbacks: []any{"claude-opus-4-8"}, wantErr: "same as the model"},
		"malformed":              {model: "claude-opus-4-8", fallbacks: "nope", wantErr: "fallbacks"},
		"haiku 5.5 list":         {model: "claude-haiku-5-5", fallbacks: []any{"claude-sonnet-4-6"}, wantErr: "no server-side fallback"},
		"haiku 5.5 default only": {model: "claude-haiku-5-5", fallbacks: "default"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := validateFallbackSpec(&latest.ModelConfig{Provider: "anthropic", Model: tc.model, ProviderOpts: map[string]any{"fallbacks": tc.fallbacks}})
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestNewClient_RejectsMalformedFallbacks(t *testing.T) {
	t.Parallel()

	cfg := &latest.ModelConfig{Provider: "anthropic", Model: "claude-opus-4-8", ProviderOpts: map[string]any{"fallbacks": "claude-sonnet-4-6"}}
	_, err := NewClient(t.Context(), cfg, environment.NewMapEnvProvider(map[string]string{"ANTHROPIC_API_KEY": "k"}))
	require.ErrorContains(t, err, `fallbacks must be "default" or a list`)
}

func TestNewClient_FallbacksOnlyOnClaudeAPI(t *testing.T) {
	t.Parallel()

	env := environment.NewMapEnvProvider(map[string]string{"ANTHROPIC_API_KEY": "k"})
	for name, tc := range map[string]struct {
		baseURL string
		opts    []options.Opt
		wantErr string
	}{
		"claude api":          {},
		"explicit claude api": {baseURL: "https://api.anthropic.com"},
		"custom base_url":     {baseURL: "https://proxy.example.com", wantErr: "requires the Claude API"},
		"gateway":             {opts: []options.Opt{options.WithGateway("https://gateway.example.com")}, wantErr: "requires the Claude API"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, fallbacks := range []any{"default", []any{"claude-sonnet-4-6"}} {
				cfg := &latest.ModelConfig{Provider: "anthropic", Model: "claude-opus-4-8", BaseURL: tc.baseURL, ProviderOpts: map[string]any{"fallbacks": fallbacks}}
				_, err := NewClient(t.Context(), cfg, env, tc.opts...)
				if tc.wantErr == "" {
					require.NoError(t, err)
					continue
				}
				require.ErrorContains(t, err, tc.wantErr)
			}
		})
	}
}
