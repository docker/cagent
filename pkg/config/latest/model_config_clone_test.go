package latest

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestModelConfig_Clone_DeepCopiesPointerFields(t *testing.T) {
	t.Parallel()

	temp := 0.7
	maxTokens := int64(4096)
	topP := 0.9
	parallel := true
	trackUsage := true

	original := &ModelConfig{
		Provider:          "openai",
		Model:             "gpt-4o",
		Temperature:       &temp,
		MaxTokens:         &maxTokens,
		TopP:              &topP,
		ParallelToolCalls: &parallel,
		TrackUsage:        &trackUsage,
		ThinkingBudget:    &ThinkingBudget{Effort: "high"},
		ProviderOpts:      map[string]any{"key": "value"},
		Routing: []RoutingRule{
			{Model: "fast", Examples: []string{"quick question"}},
		},
	}

	clone := original.Clone()

	// Mutate every pointer/collection field in the original.
	*original.Temperature = 0.1
	*original.MaxTokens = 1
	*original.TopP = 0.1
	*original.ParallelToolCalls = false
	*original.TrackUsage = false
	original.ThinkingBudget.Effort = "low"
	original.ProviderOpts["key"] = "mutated"
	original.Routing[0].Examples[0] = "mutated"

	// Clone must be unaffected.
	assert.InDelta(t, 0.7, *clone.Temperature, 0.001)
	assert.Equal(t, int64(4096), *clone.MaxTokens)
	assert.InDelta(t, 0.9, *clone.TopP, 0.001)
	assert.True(t, *clone.ParallelToolCalls)
	assert.True(t, *clone.TrackUsage)
	assert.Equal(t, "high", clone.ThinkingBudget.Effort)
	assert.Equal(t, "value", clone.ProviderOpts["key"])
	assert.Equal(t, "quick question", clone.Routing[0].Examples[0])
}

func TestModelConfig_Clone_Nil(t *testing.T) {
	t.Parallel()

	var m *ModelConfig
	assert.Nil(t, m.Clone())
}

func TestModelConfig_Clone_MinimalFields(t *testing.T) {
	t.Parallel()

	original := &ModelConfig{
		Provider: "anthropic",
		Model:    "claude-sonnet-4-5",
	}

	clone := original.Clone()

	assert.Equal(t, "anthropic", clone.Provider)
	assert.Equal(t, "claude-sonnet-4-5", clone.Model)
	assert.Nil(t, clone.Temperature)
	assert.Nil(t, clone.MaxTokens)
	assert.Nil(t, clone.ProviderOpts)
	assert.Nil(t, clone.Routing)
}
