package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/config/latest"
)

// TestApplyModelDefaults_OpenAI tests that OpenAI models get the correct default thinking_budget.
func TestApplyModelDefaults_OpenAI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                   string
		config                 *latest.ModelConfig
		expectThinkingBudget   *latest.ThinkingBudget
		expectProviderOptsKeys []string
	}{
		{
			name: "openai provider gets medium thinking_budget default",
			config: &latest.ModelConfig{
				Provider: "openai",
				Model:    "gpt-4o",
			},
			expectThinkingBudget: &latest.ThinkingBudget{Effort: "medium"},
		},
		{
			name: "openai_chatcompletions api_type gets medium thinking_budget default",
			config: &latest.ModelConfig{
				Provider:     "custom",
				Model:        "custom-model",
				ProviderOpts: map[string]any{"api_type": "openai_chatcompletions"},
			},
			expectThinkingBudget: &latest.ThinkingBudget{Effort: "medium"},
		},
		{
			name: "openai_responses api_type gets medium thinking_budget default",
			config: &latest.ModelConfig{
				Provider:     "custom",
				Model:        "custom-model",
				ProviderOpts: map[string]any{"api_type": "openai_responses"},
			},
			expectThinkingBudget: &latest.ThinkingBudget{Effort: "medium"},
		},
		{
			name: "mistral alias (openai) gets medium thinking_budget default",
			config: &latest.ModelConfig{
				Provider: "mistral",
				Model:    "mistral-large-latest",
			},
			expectThinkingBudget: &latest.ThinkingBudget{Effort: "medium"},
		},
		{
			name: "xai alias (openai) gets medium thinking_budget default",
			config: &latest.ModelConfig{
				Provider: "xai",
				Model:    "grok-2",
			},
			expectThinkingBudget: &latest.ThinkingBudget{Effort: "medium"},
		},
		{
			name: "explicit thinking_budget is preserved",
			config: &latest.ModelConfig{
				Provider:       "openai",
				Model:          "gpt-4o",
				ThinkingBudget: &latest.ThinkingBudget{Effort: "high"},
			},
			expectThinkingBudget: &latest.ThinkingBudget{Effort: "high"},
		},
		{
			name: "explicit thinking_budget with tokens is preserved",
			config: &latest.ModelConfig{
				Provider:       "openai",
				Model:          "gpt-4o",
				ThinkingBudget: &latest.ThinkingBudget{Tokens: 5000},
			},
			expectThinkingBudget: &latest.ThinkingBudget{Tokens: 5000},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Apply defaults
			applyModelDefaults(tt.config)

			// Verify thinking budget
			require.NotNil(t, tt.config.ThinkingBudget, "ThinkingBudget should be set")
			assert.Equal(t, tt.expectThinkingBudget.Effort, tt.config.ThinkingBudget.Effort, "Effort should match")
			assert.Equal(t, tt.expectThinkingBudget.Tokens, tt.config.ThinkingBudget.Tokens, "Tokens should match")
		})
	}
}

// TestApplyModelDefaults_Anthropic tests that Anthropic models get the correct defaults.
func TestApplyModelDefaults_Anthropic(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                      string
		config                    *latest.ModelConfig
		expectThinkingBudget      *latest.ThinkingBudget
		expectInterleavedThinking bool
		expectExplicitInterleaved bool // true if we expect an explicit value in ProviderOpts
	}{
		{
			name: "anthropic provider gets 8192 thinking_budget default",
			config: &latest.ModelConfig{
				Provider: "anthropic",
				Model:    "claude-sonnet-4-0",
			},
			expectThinkingBudget:      &latest.ThinkingBudget{Tokens: 8192},
			expectInterleavedThinking: true,
			expectExplicitInterleaved: true,
		},
		{
			name: "anthropic provider with no initial ProviderOpts",
			config: &latest.ModelConfig{
				Provider: "anthropic",
				Model:    "claude-opus-4-0",
			},
			expectThinkingBudget:      &latest.ThinkingBudget{Tokens: 8192},
			expectInterleavedThinking: true,
			expectExplicitInterleaved: true,
		},
		{
			name: "explicit thinking_budget is preserved",
			config: &latest.ModelConfig{
				Provider:       "anthropic",
				Model:          "claude-sonnet-4-0",
				ThinkingBudget: &latest.ThinkingBudget{Tokens: 16384},
			},
			expectThinkingBudget:      &latest.ThinkingBudget{Tokens: 16384},
			expectInterleavedThinking: true,
			expectExplicitInterleaved: true,
		},
		{
			name: "explicit interleaved_thinking false is preserved",
			config: &latest.ModelConfig{
				Provider:     "anthropic",
				Model:        "claude-sonnet-4-0",
				ProviderOpts: map[string]any{"interleaved_thinking": false},
			},
			expectThinkingBudget:      &latest.ThinkingBudget{Tokens: 8192},
			expectInterleavedThinking: false,
			expectExplicitInterleaved: true,
		},
		{
			name: "explicit interleaved_thinking true is preserved",
			config: &latest.ModelConfig{
				Provider:     "anthropic",
				Model:        "claude-sonnet-4-0",
				ProviderOpts: map[string]any{"interleaved_thinking": true},
			},
			expectThinkingBudget:      &latest.ThinkingBudget{Tokens: 8192},
			expectInterleavedThinking: true,
			expectExplicitInterleaved: true,
		},
		{
			name: "existing ProviderOpts are preserved",
			config: &latest.ModelConfig{
				Provider:     "anthropic",
				Model:        "claude-sonnet-4-0",
				ProviderOpts: map[string]any{"some_other_option": "value"},
			},
			expectThinkingBudget:      &latest.ThinkingBudget{Tokens: 8192},
			expectInterleavedThinking: true,
			expectExplicitInterleaved: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Save original ProviderOpts keys to check preservation
			originalOpts := make(map[string]any)
			if tt.config.ProviderOpts != nil {
				for k, v := range tt.config.ProviderOpts {
					originalOpts[k] = v
				}
			}

			// Apply defaults
			applyModelDefaults(tt.config)

			// Verify thinking budget
			require.NotNil(t, tt.config.ThinkingBudget, "ThinkingBudget should be set")
			assert.Equal(t, tt.expectThinkingBudget.Tokens, tt.config.ThinkingBudget.Tokens, "Tokens should match")

			// Verify interleaved_thinking
			if tt.expectExplicitInterleaved {
				require.NotNil(t, tt.config.ProviderOpts, "ProviderOpts should be set")
				val, exists := tt.config.ProviderOpts["interleaved_thinking"]
				require.True(t, exists, "interleaved_thinking should be set in ProviderOpts")
				assert.Equal(t, tt.expectInterleavedThinking, val, "interleaved_thinking should match expected value")
			}

			// Verify original ProviderOpts are preserved
			for k, v := range originalOpts {
				if k != "interleaved_thinking" {
					assert.Equal(t, v, tt.config.ProviderOpts[k], "original ProviderOpts key %s should be preserved", k)
				}
			}
		})
	}
}

// TestApplyModelDefaults_Google tests that Google Gemini models get the correct defaults.
func TestApplyModelDefaults_Google(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                 string
		config               *latest.ModelConfig
		expectThinkingBudget *latest.ThinkingBudget
		expectNoDefault      bool // true if no default should be applied
	}{
		{
			name: "gemini-2.5-flash gets dynamic thinking default (-1)",
			config: &latest.ModelConfig{
				Provider: "google",
				Model:    "gemini-2.5-flash",
			},
			expectThinkingBudget: &latest.ThinkingBudget{Tokens: -1},
		},
		{
			name: "gemini-2.5-pro gets dynamic thinking default (-1)",
			config: &latest.ModelConfig{
				Provider: "google",
				Model:    "gemini-2.5-pro",
			},
			expectThinkingBudget: &latest.ThinkingBudget{Tokens: -1},
		},
		{
			name: "gemini-2.5-flash-lite gets dynamic thinking default (-1)",
			config: &latest.ModelConfig{
				Provider: "google",
				Model:    "gemini-2.5-flash-lite",
			},
			expectThinkingBudget: &latest.ThinkingBudget{Tokens: -1},
		},
		{
			name: "gemini-3-pro gets high thinking level default",
			config: &latest.ModelConfig{
				Provider: "google",
				Model:    "gemini-3-pro",
			},
			expectThinkingBudget: &latest.ThinkingBudget{Effort: "high"},
		},
		{
			name: "gemini-3-pro-preview gets high thinking level default",
			config: &latest.ModelConfig{
				Provider: "google",
				Model:    "gemini-3-pro-preview",
			},
			expectThinkingBudget: &latest.ThinkingBudget{Effort: "high"},
		},
		{
			name: "gemini-3-flash gets medium thinking level default",
			config: &latest.ModelConfig{
				Provider: "google",
				Model:    "gemini-3-flash",
			},
			expectThinkingBudget: &latest.ThinkingBudget{Effort: "medium"},
		},
		{
			name: "gemini-3-flash-preview gets medium thinking level default",
			config: &latest.ModelConfig{
				Provider: "google",
				Model:    "gemini-3-flash-preview",
			},
			expectThinkingBudget: &latest.ThinkingBudget{Effort: "medium"},
		},
		{
			name: "gemini-2.0-flash is not affected (old model)",
			config: &latest.ModelConfig{
				Provider: "google",
				Model:    "gemini-2.0-flash",
			},
			expectNoDefault: true,
		},
		{
			name: "gemini-1.5-pro is not affected (old model)",
			config: &latest.ModelConfig{
				Provider: "google",
				Model:    "gemini-1.5-pro",
			},
			expectNoDefault: true,
		},
		{
			name: "explicit thinking_budget is preserved for gemini-2.5",
			config: &latest.ModelConfig{
				Provider:       "google",
				Model:          "gemini-2.5-flash",
				ThinkingBudget: &latest.ThinkingBudget{Tokens: 8192},
			},
			expectThinkingBudget: &latest.ThinkingBudget{Tokens: 8192},
		},
		{
			name: "explicit thinking_budget is preserved for gemini-3",
			config: &latest.ModelConfig{
				Provider:       "google",
				Model:          "gemini-3-pro",
				ThinkingBudget: &latest.ThinkingBudget{Effort: "low"},
			},
			expectThinkingBudget: &latest.ThinkingBudget{Effort: "low"},
		},
		{
			name: "thinking_budget 0 disables thinking completely (nil)",
			config: &latest.ModelConfig{
				Provider:       "google",
				Model:          "gemini-2.5-flash",
				ThinkingBudget: &latest.ThinkingBudget{Tokens: 0},
			},
			expectNoDefault: true, // thinking_budget: 0 means disable thinking entirely
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Apply defaults
			applyModelDefaults(tt.config)

			if tt.expectNoDefault {
				assert.Nil(t, tt.config.ThinkingBudget, "ThinkingBudget should not be set for old Gemini model")
				return
			}

			// Verify thinking budget
			require.NotNil(t, tt.config.ThinkingBudget, "ThinkingBudget should be set")
			assert.Equal(t, tt.expectThinkingBudget.Effort, tt.config.ThinkingBudget.Effort, "Effort should match")
			assert.Equal(t, tt.expectThinkingBudget.Tokens, tt.config.ThinkingBudget.Tokens, "Tokens should match")
		})
	}
}

// TestApplyModelDefaults_Bedrock tests that Amazon Bedrock Claude models get the correct defaults.
func TestApplyModelDefaults_Bedrock(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                      string
		config                    *latest.ModelConfig
		expectThinkingBudget      *latest.ThinkingBudget
		expectInterleavedThinking bool
		expectExplicitInterleaved bool // true if we expect an explicit value in ProviderOpts
		expectNoDefault           bool // true if no default should be applied
	}{
		{
			name: "bedrock claude model gets defaults",
			config: &latest.ModelConfig{
				Provider: "amazon-bedrock",
				Model:    "anthropic.claude-3-sonnet",
			},
			expectThinkingBudget:      &latest.ThinkingBudget{Tokens: 8192},
			expectInterleavedThinking: true,
			expectExplicitInterleaved: true,
		},
		{
			name: "bedrock claude-sonnet-4 model gets defaults",
			config: &latest.ModelConfig{
				Provider: "amazon-bedrock",
				Model:    "anthropic.claude-sonnet-4-20250514-v1:0",
			},
			expectThinkingBudget:      &latest.ThinkingBudget{Tokens: 8192},
			expectInterleavedThinking: true,
			expectExplicitInterleaved: true,
		},
		{
			name: "bedrock global claude model gets defaults",
			config: &latest.ModelConfig{
				Provider: "amazon-bedrock",
				Model:    "global.anthropic.claude-sonnet-4-5-20250929-v1:0",
			},
			expectThinkingBudget:      &latest.ThinkingBudget{Tokens: 8192},
			expectInterleavedThinking: true,
			expectExplicitInterleaved: true,
		},
		{
			name: "bedrock claude opus model gets defaults",
			config: &latest.ModelConfig{
				Provider: "amazon-bedrock",
				Model:    "anthropic.claude-opus-4-0",
			},
			expectThinkingBudget:      &latest.ThinkingBudget{Tokens: 8192},
			expectInterleavedThinking: true,
			expectExplicitInterleaved: true,
		},
		{
			name: "bedrock non-claude model is not affected",
			config: &latest.ModelConfig{
				Provider: "amazon-bedrock",
				Model:    "amazon.titan-text-express-v1",
			},
			expectNoDefault: true,
		},
		{
			name: "bedrock mistral model is not affected",
			config: &latest.ModelConfig{
				Provider: "amazon-bedrock",
				Model:    "mistral.mistral-large-latest",
			},
			expectNoDefault: true,
		},
		{
			name: "explicit thinking_budget is preserved",
			config: &latest.ModelConfig{
				Provider:       "amazon-bedrock",
				Model:          "anthropic.claude-3-sonnet",
				ThinkingBudget: &latest.ThinkingBudget{Tokens: 16384},
			},
			expectThinkingBudget:      &latest.ThinkingBudget{Tokens: 16384},
			expectInterleavedThinking: true,
			expectExplicitInterleaved: true,
		},
		{
			name: "explicit interleaved_thinking false is preserved",
			config: &latest.ModelConfig{
				Provider:     "amazon-bedrock",
				Model:        "anthropic.claude-3-sonnet",
				ProviderOpts: map[string]any{"interleaved_thinking": false},
			},
			expectThinkingBudget:      &latest.ThinkingBudget{Tokens: 8192},
			expectInterleavedThinking: false,
			expectExplicitInterleaved: true,
		},
		{
			name: "existing ProviderOpts are preserved",
			config: &latest.ModelConfig{
				Provider:     "amazon-bedrock",
				Model:        "anthropic.claude-3-sonnet",
				ProviderOpts: map[string]any{"region": "us-west-2"},
			},
			expectThinkingBudget:      &latest.ThinkingBudget{Tokens: 8192},
			expectInterleavedThinking: true,
			expectExplicitInterleaved: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Save original ProviderOpts keys to check preservation
			originalOpts := make(map[string]any)
			if tt.config.ProviderOpts != nil {
				for k, v := range tt.config.ProviderOpts {
					originalOpts[k] = v
				}
			}

			// Apply defaults
			applyModelDefaults(tt.config)

			if tt.expectNoDefault {
				assert.Nil(t, tt.config.ThinkingBudget, "ThinkingBudget should not be set for non-Claude Bedrock model")
				if tt.config.ProviderOpts != nil {
					_, exists := tt.config.ProviderOpts["interleaved_thinking"]
					assert.False(t, exists, "interleaved_thinking should not be set for non-Claude Bedrock model")
				}
				return
			}

			// Verify thinking budget
			require.NotNil(t, tt.config.ThinkingBudget, "ThinkingBudget should be set")
			assert.Equal(t, tt.expectThinkingBudget.Tokens, tt.config.ThinkingBudget.Tokens, "Tokens should match")

			// Verify interleaved_thinking
			if tt.expectExplicitInterleaved {
				require.NotNil(t, tt.config.ProviderOpts, "ProviderOpts should be set")
				val, exists := tt.config.ProviderOpts["interleaved_thinking"]
				require.True(t, exists, "interleaved_thinking should be set in ProviderOpts")
				assert.Equal(t, tt.expectInterleavedThinking, val, "interleaved_thinking should match expected value")
			}

			// Verify original ProviderOpts are preserved
			for k, v := range originalOpts {
				if k != "interleaved_thinking" {
					assert.Equal(t, v, tt.config.ProviderOpts[k], "original ProviderOpts key %s should be preserved", k)
				}
			}
		})
	}
}

// TestApplyModelDefaults_NonAffectedProviders tests that other providers are not affected.
func TestApplyModelDefaults_NonAffectedProviders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config *latest.ModelConfig
	}{
		{
			name: "google gemini-2.0-flash is not affected (old model)",
			config: &latest.ModelConfig{
				Provider: "google",
				Model:    "gemini-2.0-flash",
			},
		},
		{
			name: "dmr provider is not affected",
			config: &latest.ModelConfig{
				Provider: "dmr",
				Model:    "ai/llama3.2",
			},
		},
		{
			name: "amazon-bedrock non-claude model is not affected",
			config: &latest.ModelConfig{
				Provider: "amazon-bedrock",
				Model:    "amazon.titan-text-express-v1",
			},
		},
		{
			name: "unknown provider is not affected",
			config: &latest.ModelConfig{
				Provider: "unknown",
				Model:    "some-model",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Apply defaults
			applyModelDefaults(tt.config)

			// Verify thinking_budget is NOT set
			assert.Nil(t, tt.config.ThinkingBudget, "ThinkingBudget should not be set for non-affected provider")

			// Verify interleaved_thinking is NOT set
			if tt.config.ProviderOpts != nil {
				_, exists := tt.config.ProviderOpts["interleaved_thinking"]
				assert.False(t, exists, "interleaved_thinking should not be set for non-affected provider")
			}
		})
	}
}

// TestApplyProviderDefaults_IncludesModelDefaults tests that applyProviderDefaults
// also applies model-specific defaults via applyModelDefaults.
func TestApplyProviderDefaults_IncludesModelDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                      string
		config                    *latest.ModelConfig
		customProviders           map[string]latest.ProviderConfig
		expectThinkingBudget      *latest.ThinkingBudget
		expectInterleavedThinking *bool
	}{
		{
			name: "openai model from config gets defaults",
			config: &latest.ModelConfig{
				Provider: "openai",
				Model:    "gpt-4o",
			},
			expectThinkingBudget: &latest.ThinkingBudget{Effort: "medium"},
		},
		{
			name: "anthropic model from config gets defaults",
			config: &latest.ModelConfig{
				Provider: "anthropic",
				Model:    "claude-sonnet-4-0",
			},
			expectThinkingBudget:      &latest.ThinkingBudget{Tokens: 8192},
			expectInterleavedThinking: boolPtr(true),
		},
		{
			name: "google gemini-2.5 model gets defaults",
			config: &latest.ModelConfig{
				Provider: "google",
				Model:    "gemini-2.5-flash",
			},
			expectThinkingBudget: &latest.ThinkingBudget{Tokens: -1},
		},
		{
			name: "google gemini-3-pro model gets defaults",
			config: &latest.ModelConfig{
				Provider: "google",
				Model:    "gemini-3-pro",
			},
			expectThinkingBudget: &latest.ThinkingBudget{Effort: "high"},
		},
		{
			name: "google gemini-3-flash model gets defaults",
			config: &latest.ModelConfig{
				Provider: "google",
				Model:    "gemini-3-flash",
			},
			expectThinkingBudget: &latest.ThinkingBudget{Effort: "medium"},
		},
		{
			name: "bedrock claude model gets defaults",
			config: &latest.ModelConfig{
				Provider: "amazon-bedrock",
				Model:    "anthropic.claude-3-sonnet",
			},
			expectThinkingBudget:      &latest.ThinkingBudget{Tokens: 8192},
			expectInterleavedThinking: boolPtr(true),
		},
		{
			name: "bedrock global claude model gets defaults",
			config: &latest.ModelConfig{
				Provider: "amazon-bedrock",
				Model:    "global.anthropic.claude-sonnet-4-5-20250929-v1:0",
			},
			expectThinkingBudget:      &latest.ThinkingBudget{Tokens: 8192},
			expectInterleavedThinking: boolPtr(true),
		},
		{
			name: "custom provider with openai api_type gets openai defaults",
			config: &latest.ModelConfig{
				Provider: "my_gateway",
				Model:    "gpt-4o",
			},
			customProviders: map[string]latest.ProviderConfig{
				"my_gateway": {
					APIType:  "openai_chatcompletions",
					BaseURL:  "https://api.example.com/v1",
					TokenKey: "MY_KEY",
				},
			},
			expectThinkingBudget: &latest.ThinkingBudget{Effort: "medium"},
		},
		{
			name: "custom provider with anthropic api_type gets anthropic defaults",
			config: &latest.ModelConfig{
				Provider: "my_anthropic_gateway",
				Model:    "claude-sonnet-4-0",
				ProviderOpts: map[string]any{
					"api_type": "anthropic",
				},
			},
			customProviders:           nil,
			expectThinkingBudget:      &latest.ThinkingBudget{Tokens: 8192},
			expectInterleavedThinking: boolPtr(true),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := applyProviderDefaults(tt.config, tt.customProviders)

			// Verify thinking budget
			if tt.expectThinkingBudget != nil {
				require.NotNil(t, result.ThinkingBudget, "ThinkingBudget should be set")
				assert.Equal(t, tt.expectThinkingBudget.Effort, result.ThinkingBudget.Effort, "Effort should match")
				assert.Equal(t, tt.expectThinkingBudget.Tokens, result.ThinkingBudget.Tokens, "Tokens should match")
			}

			// Verify interleaved_thinking for Anthropic
			if tt.expectInterleavedThinking != nil {
				require.NotNil(t, result.ProviderOpts, "ProviderOpts should be set")
				val, exists := result.ProviderOpts["interleaved_thinking"]
				require.True(t, exists, "interleaved_thinking should be set")
				assert.Equal(t, *tt.expectInterleavedThinking, val, "interleaved_thinking should match")
			}
		})
	}
}

// boolPtr is a helper to create a pointer to a bool value.
func boolPtr(b bool) *bool {
	return &b
}

// TestApplyProviderDefaults_ThinkingDefaultsApplied tests that thinking defaults
// are always applied when the config doesn't have an explicit thinking budget.
func TestApplyProviderDefaults_ThinkingDefaultsApplied(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                      string
		config                    *latest.ModelConfig
		expectThinkingBudget      *latest.ThinkingBudget
		expectInterleavedThinking bool
	}{
		{
			name: "OpenAI gets default thinking_budget",
			config: &latest.ModelConfig{
				Provider: "openai",
				Model:    "gpt-4o",
			},
			expectThinkingBudget: &latest.ThinkingBudget{Effort: "medium"},
		},
		{
			name: "Anthropic gets default thinking_budget and interleaved_thinking",
			config: &latest.ModelConfig{
				Provider: "anthropic",
				Model:    "claude-sonnet-4-0",
			},
			expectThinkingBudget:      &latest.ThinkingBudget{Tokens: 8192},
			expectInterleavedThinking: true,
		},
		{
			name: "Google Gemini 2.5 gets default thinking_budget",
			config: &latest.ModelConfig{
				Provider: "google",
				Model:    "gemini-2.5-pro",
			},
			expectThinkingBudget: &latest.ThinkingBudget{Tokens: -1},
		},
		{
			name: "Google Gemini 3 Pro gets default thinking_budget",
			config: &latest.ModelConfig{
				Provider: "google",
				Model:    "gemini-3-pro",
			},
			expectThinkingBudget: &latest.ThinkingBudget{Effort: "high"},
		},
		{
			name: "Bedrock Claude gets default thinking_budget and interleaved_thinking",
			config: &latest.ModelConfig{
				Provider: "amazon-bedrock",
				Model:    "anthropic.claude-3-sonnet",
			},
			expectThinkingBudget:      &latest.ThinkingBudget{Tokens: 8192},
			expectInterleavedThinking: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Apply provider defaults
			result := applyProviderDefaults(tt.config, nil)

			// Verify default thinking budget was applied
			require.NotNil(t, result.ThinkingBudget, "ThinkingBudget should be set")
			assert.Equal(t, tt.expectThinkingBudget.Effort, result.ThinkingBudget.Effort, "Effort should match")
			assert.Equal(t, tt.expectThinkingBudget.Tokens, result.ThinkingBudget.Tokens, "Tokens should match")

			// Verify interleaved_thinking for Anthropic/Bedrock
			if tt.expectInterleavedThinking {
				require.NotNil(t, result.ProviderOpts, "ProviderOpts should be set")
				val, exists := result.ProviderOpts["interleaved_thinking"]
				require.True(t, exists, "interleaved_thinking should be set")
				assert.Equal(t, true, val, "interleaved_thinking should be true")
			}
		})
	}
}

// TestApplyProviderDefaults_ExplicitThinkingPreserved tests that explicitly set
// thinking options are preserved and not overwritten by defaults.
func TestApplyProviderDefaults_ExplicitThinkingPreserved(t *testing.T) {
	t.Parallel()

	config := &latest.ModelConfig{
		Provider:       "openai",
		Model:          "gpt-4o",
		ThinkingBudget: &latest.ThinkingBudget{Effort: "high"},
	}

	result := applyProviderDefaults(config, nil)

	require.NotNil(t, result.ThinkingBudget, "ThinkingBudget should be preserved")
	assert.Equal(t, "high", result.ThinkingBudget.Effort, "Effort should be preserved")
}

// TestApplyProviderDefaults_DisabledThinkingBecomesNil tests that explicitly disabled
// thinking (thinking_budget: 0 or thinking_budget: none) results in nil ThinkingBudget.
func TestApplyProviderDefaults_DisabledThinkingBecomesNil(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config *latest.ModelConfig
	}{
		{
			name: "thinking_budget 0 becomes nil",
			config: &latest.ModelConfig{
				Provider:       "anthropic",
				Model:          "claude-sonnet-4-0",
				ThinkingBudget: &latest.ThinkingBudget{Tokens: 0},
			},
		},
		{
			name: "thinking_budget none becomes nil",
			config: &latest.ModelConfig{
				Provider:       "openai",
				Model:          "gpt-4o",
				ThinkingBudget: &latest.ThinkingBudget{Effort: "none"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := applyProviderDefaults(tt.config, nil)

			assert.Nil(t, result.ThinkingBudget, "ThinkingBudget should be nil when explicitly disabled")
		})
	}
}
