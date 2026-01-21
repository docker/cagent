package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/model/provider/options"
)

// TestApplyOverrides_Thinking tests that applyOverrides correctly clears
// thinking configuration when Thinking is set to false (disabled).
func TestApplyOverrides_Thinking(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                      string
		config                    *latest.ModelConfig
		thinkingEnabled           *bool // nil means no override, true means enabled, false means disabled
		expectThinkingBudget      *latest.ThinkingBudget
		expectInterleavedThinking *bool // nil means key should not exist
	}{
		{
			name: "clears explicit thinking_budget when disabled",
			config: &latest.ModelConfig{
				Provider:       "anthropic",
				Model:          "claude-sonnet-4-0",
				ThinkingBudget: &latest.ThinkingBudget{Tokens: 8192},
			},
			thinkingEnabled:      boolPtr(false),
			expectThinkingBudget: nil,
		},
		{
			name: "clears interleaved_thinking when disabled",
			config: &latest.ModelConfig{
				Provider:       "anthropic",
				Model:          "claude-sonnet-4-0",
				ThinkingBudget: &latest.ThinkingBudget{Tokens: 16384},
				ProviderOpts:   map[string]any{"interleaved_thinking": true},
			},
			thinkingEnabled:           boolPtr(false),
			expectThinkingBudget:      nil,
			expectInterleavedThinking: nil, // key should be removed
		},
		{
			name: "preserves thinking_budget when enabled",
			config: &latest.ModelConfig{
				Provider:       "anthropic",
				Model:          "claude-sonnet-4-0",
				ThinkingBudget: &latest.ThinkingBudget{Tokens: 8192},
			},
			thinkingEnabled:      boolPtr(true),
			expectThinkingBudget: &latest.ThinkingBudget{Tokens: 8192},
		},
		{
			name: "preserves interleaved_thinking when enabled",
			config: &latest.ModelConfig{
				Provider:       "anthropic",
				Model:          "claude-sonnet-4-0",
				ThinkingBudget: &latest.ThinkingBudget{Tokens: 8192},
				ProviderOpts:   map[string]any{"interleaved_thinking": true},
			},
			thinkingEnabled:           boolPtr(true),
			expectThinkingBudget:      &latest.ThinkingBudget{Tokens: 8192},
			expectInterleavedThinking: boolPtr(true),
		},
		{
			name: "preserves other ProviderOpts when clearing thinking",
			config: &latest.ModelConfig{
				Provider:       "anthropic",
				Model:          "claude-sonnet-4-0",
				ThinkingBudget: &latest.ThinkingBudget{Tokens: 8192},
				ProviderOpts: map[string]any{
					"interleaved_thinking": true,
					"other_option":         "preserved",
				},
			},
			thinkingEnabled:      boolPtr(false),
			expectThinkingBudget: nil,
		},
		{
			name: "nil options is a no-op",
			config: &latest.ModelConfig{
				Provider:       "anthropic",
				Model:          "claude-sonnet-4-0",
				ThinkingBudget: &latest.ThinkingBudget{Tokens: 8192},
			},
			thinkingEnabled:      nil, // Will pass nil opts
			expectThinkingBudget: &latest.ThinkingBudget{Tokens: 8192},
		},
		{
			name: "applies defaults when enabled and ThinkingBudget is nil (OpenAI)",
			config: &latest.ModelConfig{
				Provider:       "openai",
				Model:          "gpt-4o",
				ThinkingBudget: nil, // No thinking configured
			},
			thinkingEnabled:      boolPtr(true),
			expectThinkingBudget: &latest.ThinkingBudget{Effort: "medium"}, // OpenAI default
		},
		{
			name: "applies defaults when enabled and ThinkingBudget is nil (Anthropic)",
			config: &latest.ModelConfig{
				Provider:       "anthropic",
				Model:          "claude-sonnet-4-0",
				ThinkingBudget: nil, // No thinking configured
			},
			thinkingEnabled:           boolPtr(true),
			expectThinkingBudget:      &latest.ThinkingBudget{Tokens: 8192}, // Anthropic default
			expectInterleavedThinking: boolPtr(true),                        // Anthropic default
		},
		{
			name: "restores defaults when /think used with tokens=0",
			config: &latest.ModelConfig{
				Provider:       "openai",
				Model:          "gpt-4o",
				ThinkingBudget: &latest.ThinkingBudget{Tokens: 0}, // User had thinking disabled
			},
			thinkingEnabled:      boolPtr(true),                            // User runs /think
			expectThinkingBudget: &latest.ThinkingBudget{Effort: "medium"}, // Apply OpenAI default
		},
		{
			name: "restores defaults when /think used with effort=none",
			config: &latest.ModelConfig{
				Provider:       "anthropic",
				Model:          "claude-sonnet-4-0",
				ThinkingBudget: &latest.ThinkingBudget{Effort: "none"}, // User had thinking disabled
			},
			thinkingEnabled:           boolPtr(true),                        // User runs /think
			expectThinkingBudget:      &latest.ThinkingBudget{Tokens: 8192}, // Apply Anthropic default
			expectInterleavedThinking: boolPtr(true),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Build options
			var opts *options.ModelOptions
			if tt.thinkingEnabled != nil {
				mo := options.ModelOptions{}
				options.WithThinking(*tt.thinkingEnabled)(&mo)
				opts = &mo
			}

			// Save original other options for preservation check
			var originalOtherOpts map[string]any
			if tt.config.ProviderOpts != nil {
				originalOtherOpts = make(map[string]any)
				for k, v := range tt.config.ProviderOpts {
					if k != "interleaved_thinking" {
						originalOtherOpts[k] = v
					}
				}
			}

			// Apply overrides
			result := applyOverrides(tt.config, opts)

			// Verify thinking budget
			if tt.expectThinkingBudget == nil {
				assert.Nil(t, result.ThinkingBudget, "ThinkingBudget should be nil")
			} else {
				require.NotNil(t, result.ThinkingBudget, "ThinkingBudget should be set")
				assert.Equal(t, tt.expectThinkingBudget.Tokens, result.ThinkingBudget.Tokens)
				assert.Equal(t, tt.expectThinkingBudget.Effort, result.ThinkingBudget.Effort)
			}

			// Verify interleaved_thinking
			if tt.expectInterleavedThinking == nil && tt.thinkingEnabled != nil && !*tt.thinkingEnabled {
				// Key should be removed when thinking is disabled
				if result.ProviderOpts != nil {
					_, exists := result.ProviderOpts["interleaved_thinking"]
					assert.False(t, exists, "interleaved_thinking should be removed")
				}
			} else if tt.expectInterleavedThinking != nil {
				require.NotNil(t, result.ProviderOpts)
				val, exists := result.ProviderOpts["interleaved_thinking"]
				require.True(t, exists, "interleaved_thinking should exist")
				assert.Equal(t, *tt.expectInterleavedThinking, val)
			}

			// Verify other ProviderOpts are preserved
			for k, v := range originalOtherOpts {
				require.NotNil(t, result.ProviderOpts, "ProviderOpts should exist for preserved keys")
				assert.Equal(t, v, result.ProviderOpts[k], "other ProviderOpts key %s should be preserved", k)
			}
		})
	}
}

// TestApplyOverrides_AllProviders tests that thinking override works for all providers.
func TestApplyOverrides_AllProviders(t *testing.T) {
	t.Parallel()

	providers := []struct {
		name     string
		provider string
		model    string
	}{
		{"OpenAI", "openai", "gpt-4o"},
		{"Anthropic", "anthropic", "claude-sonnet-4-0"},
		{"Google", "google", "gemini-2.5-flash"},
		{"Bedrock Claude", "amazon-bedrock", "global.anthropic.claude-sonnet-4-5-20250929-v1:0"},
		{"Mistral (alias)", "mistral", "mistral-large-latest"},
		{"xAI (alias)", "xai", "grok-2"},
	}

	for _, p := range providers {
		t.Run(p.name, func(t *testing.T) {
			t.Parallel()

			// Create config with thinking budget
			config := &latest.ModelConfig{
				Provider:       p.provider,
				Model:          p.model,
				ThinkingBudget: &latest.ThinkingBudget{Tokens: 8192},
			}

			// Apply override with thinking disabled
			mo := options.ModelOptions{}
			options.WithThinking(false)(&mo)
			result := applyOverrides(config, &mo)

			// Thinking should be cleared for all providers
			assert.Nil(t, result.ThinkingBudget,
				"ThinkingBudget should be cleared for provider %s", p.provider)
		})
	}
}

// TestDefaultsThenOverrides tests the full flow: defaults applied first, then overrides.
func TestDefaultsThenOverrides(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                 string
		config               *latest.ModelConfig
		thinkingEnabled      bool
		expectThinkingBudget *latest.ThinkingBudget
	}{
		{
			name: "OpenAI: defaults applied, then cleared by override",
			config: &latest.ModelConfig{
				Provider: "openai",
				Model:    "gpt-4o",
				// No ThinkingBudget set - defaults will apply
			},
			thinkingEnabled:      false,
			expectThinkingBudget: nil, // Override clears the default
		},
		{
			name: "OpenAI: defaults applied, preserved when enabled",
			config: &latest.ModelConfig{
				Provider: "openai",
				Model:    "gpt-4o",
			},
			thinkingEnabled:      true,
			expectThinkingBudget: &latest.ThinkingBudget{Effort: "medium"}, // Default preserved
		},
		{
			name: "Anthropic: defaults applied, then cleared by override",
			config: &latest.ModelConfig{
				Provider: "anthropic",
				Model:    "claude-sonnet-4-0",
			},
			thinkingEnabled:      false,
			expectThinkingBudget: nil,
		},
		{
			name: "Anthropic: defaults applied, preserved when enabled",
			config: &latest.ModelConfig{
				Provider: "anthropic",
				Model:    "claude-sonnet-4-0",
			},
			thinkingEnabled:      true,
			expectThinkingBudget: &latest.ThinkingBudget{Tokens: 8192},
		},
		{
			name: "Google Gemini 2.5: defaults applied, then cleared by override",
			config: &latest.ModelConfig{
				Provider: "google",
				Model:    "gemini-2.5-flash",
			},
			thinkingEnabled:      false,
			expectThinkingBudget: nil,
		},
		{
			name: "Google Gemini 3 Pro: defaults applied, then cleared by override",
			config: &latest.ModelConfig{
				Provider: "google",
				Model:    "gemini-3-pro",
			},
			thinkingEnabled:      false,
			expectThinkingBudget: nil,
		},
		{
			name: "Bedrock Claude: defaults applied, then cleared by override",
			config: &latest.ModelConfig{
				Provider: "amazon-bedrock",
				Model:    "anthropic.claude-3-sonnet",
			},
			thinkingEnabled:      false,
			expectThinkingBudget: nil,
		},
		{
			name: "Explicit budget cleared by override",
			config: &latest.ModelConfig{
				Provider:       "anthropic",
				Model:          "claude-sonnet-4-0",
				ThinkingBudget: &latest.ThinkingBudget{Tokens: 32000}, // Explicit
			},
			thinkingEnabled:      false,
			expectThinkingBudget: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Step 1: Apply defaults (simulating createDirectProvider flow)
			result := applyProviderDefaults(tt.config, nil)

			// Step 2: Apply overrides
			mo := options.ModelOptions{}
			options.WithThinking(tt.thinkingEnabled)(&mo)
			result = applyOverrides(result, &mo)

			// Verify result
			if tt.expectThinkingBudget == nil {
				assert.Nil(t, result.ThinkingBudget, "ThinkingBudget should be nil after override")
			} else {
				require.NotNil(t, result.ThinkingBudget, "ThinkingBudget should be set")
				assert.Equal(t, tt.expectThinkingBudget.Tokens, result.ThinkingBudget.Tokens)
				assert.Equal(t, tt.expectThinkingBudget.Effort, result.ThinkingBudget.Effort)
			}
		})
	}
}

// TestApplyOverrides_NilOpts tests that nil options returns config unchanged.
func TestApplyOverrides_NilOpts(t *testing.T) {
	t.Parallel()

	config := &latest.ModelConfig{
		Provider:       "anthropic",
		Model:          "claude-sonnet-4-0",
		ThinkingBudget: &latest.ThinkingBudget{Tokens: 8192},
		ProviderOpts:   map[string]any{"interleaved_thinking": true},
	}

	result := applyOverrides(config, nil)

	// Should be unchanged
	require.NotNil(t, result.ThinkingBudget)
	assert.Equal(t, 8192, result.ThinkingBudget.Tokens)
	assert.Equal(t, true, result.ProviderOpts["interleaved_thinking"])
}

// TestApplyOverrides_DoesNotModifyOriginal tests that applyOverrides creates a copy.
func TestApplyOverrides_DoesNotModifyOriginal(t *testing.T) {
	t.Parallel()

	original := &latest.ModelConfig{
		Provider:       "anthropic",
		Model:          "claude-sonnet-4-0",
		ThinkingBudget: &latest.ThinkingBudget{Tokens: 8192},
		ProviderOpts:   map[string]any{"interleaved_thinking": true},
	}

	mo := options.ModelOptions{}
	options.WithThinking(false)(&mo)
	result := applyOverrides(original, &mo)

	// Original should be unchanged
	require.NotNil(t, original.ThinkingBudget, "Original ThinkingBudget should be unchanged")
	assert.Equal(t, 8192, original.ThinkingBudget.Tokens)

	// Result should have changes
	assert.Nil(t, result.ThinkingBudget, "Result ThinkingBudget should be nil")
}

// TestApplyOverrides_RestoresDefaultsFromDisabled tests that using /think when
// the config has thinking explicitly disabled (Tokens=0 or Effort="none") applies
// provider defaults. This is the key behavior that makes /think work when YAML
// starts with thinking_budget: 0 or thinking_budget: none.
//
// Note: applyProviderDefaults now converts disabled thinking (Tokens=0 or Effort="none")
// to nil ThinkingBudget. The /think command (applyOverrides with Thinking=true) then
// applies provider defaults since ThinkingBudget is nil.
func TestApplyOverrides_RestoresDefaultsFromDisabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                 string
		config               *latest.ModelConfig
		expectThinkingBudget *latest.ThinkingBudget
	}{
		{
			name: "Anthropic: /think with Tokens=0 applies default 8192",
			config: &latest.ModelConfig{
				Provider:       "anthropic",
				Model:          "claude-sonnet-4-0",
				ThinkingBudget: &latest.ThinkingBudget{Tokens: 0},
			},
			expectThinkingBudget: &latest.ThinkingBudget{Tokens: 8192},
		},
		{
			name: "Anthropic: /think with Effort=none applies default 8192",
			config: &latest.ModelConfig{
				Provider:       "anthropic",
				Model:          "claude-sonnet-4-0",
				ThinkingBudget: &latest.ThinkingBudget{Effort: "none"},
			},
			expectThinkingBudget: &latest.ThinkingBudget{Tokens: 8192},
		},
		{
			name: "OpenAI: /think with Tokens=0 applies default medium",
			config: &latest.ModelConfig{
				Provider:       "openai",
				Model:          "gpt-4o",
				ThinkingBudget: &latest.ThinkingBudget{Tokens: 0},
			},
			expectThinkingBudget: &latest.ThinkingBudget{Effort: "medium"},
		},
		{
			name: "OpenAI: /think with Effort=none applies default medium",
			config: &latest.ModelConfig{
				Provider:       "openai",
				Model:          "gpt-4o",
				ThinkingBudget: &latest.ThinkingBudget{Effort: "none"},
			},
			expectThinkingBudget: &latest.ThinkingBudget{Effort: "medium"},
		},
		{
			name: "Gemini 2.5: /think with Tokens=0 applies default -1 (dynamic)",
			config: &latest.ModelConfig{
				Provider:       "google",
				Model:          "gemini-2.5-flash",
				ThinkingBudget: &latest.ThinkingBudget{Tokens: 0},
			},
			expectThinkingBudget: &latest.ThinkingBudget{Tokens: -1},
		},
		{
			name: "Bedrock Claude: /think with Tokens=0 applies default 8192",
			config: &latest.ModelConfig{
				Provider:       "amazon-bedrock",
				Model:          "anthropic.claude-3-sonnet",
				ThinkingBudget: &latest.ThinkingBudget{Tokens: 0},
			},
			expectThinkingBudget: &latest.ThinkingBudget{Tokens: 8192},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Step 1: Apply provider defaults (simulating createDirectProvider flow)
			// This now converts disabled thinking (Tokens=0 or Effort="none") to nil
			result := applyProviderDefaults(tt.config, nil)

			// Verify thinking is disabled (nil) after provider defaults
			assert.Nil(t, result.ThinkingBudget,
				"ThinkingBudget should be nil after applyProviderDefaults when explicitly disabled")

			// Step 2: Apply override with thinking explicitly enabled (simulates /think toggle)
			mo := options.ModelOptions{}
			options.WithThinking(true)(&mo)
			result = applyOverrides(result, &mo)

			// Verify defaults were applied - /think enables thinking with provider defaults
			require.NotNil(t, result.ThinkingBudget, "ThinkingBudget should be set after /think")
			assert.Equal(t, tt.expectThinkingBudget.Tokens, result.ThinkingBudget.Tokens, "Tokens should match default")
			assert.Equal(t, tt.expectThinkingBudget.Effort, result.ThinkingBudget.Effort, "Effort should match default")
		})
	}
}

// TestIsThinkingBudgetDisabled tests the helper function.
func TestIsThinkingBudgetDisabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		budget   *latest.ThinkingBudget
		expected bool
	}{
		{"nil budget", nil, false},
		{"Tokens=0", &latest.ThinkingBudget{Tokens: 0}, true},
		{"Effort=none", &latest.ThinkingBudget{Effort: "none"}, true},
		{"Tokens=8192", &latest.ThinkingBudget{Tokens: 8192}, false},
		{"Effort=medium", &latest.ThinkingBudget{Effort: "medium"}, false},
		{"Tokens=-1 (dynamic)", &latest.ThinkingBudget{Tokens: -1}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, isThinkingBudgetDisabled(tt.budget))
		})
	}
}
