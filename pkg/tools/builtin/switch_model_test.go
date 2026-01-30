package builtin

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSwitchModelToolset(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		models  []string
		wantErr bool
	}{
		{"valid models", []string{"fast", "powerful"}, false},
		{"empty list", []string{}, true},
		{"nil list", nil, true},
		{"empty model in list", []string{"fast", "", "powerful"}, true},
		{"whitespace-only model", []string{"fast", "   ", "powerful"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			toolset, err := NewSwitchModelToolset(tt.models)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.models[0], toolset.CurrentModel())
		})
	}
}

func TestSwitchModelToolset_callTool(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		model       string
		wantError   bool
		wantOutput  string
		wantCurrent string
	}{
		{
			name:        "switches to allowed model",
			model:       "powerful",
			wantOutput:  "Switched model from \"fast\" to \"powerful\"",
			wantCurrent: "powerful",
		},
		{
			name:        "already on requested model",
			model:       "fast",
			wantOutput:  "Model is already set to \"fast\"",
			wantCurrent: "fast",
		},
		{
			name:        "rejects unknown model",
			model:       "unknown",
			wantError:   true,
			wantOutput:  "model \"unknown\" is not allowed",
			wantCurrent: "fast",
		},
		{
			name:        "rejects empty model",
			model:       "",
			wantError:   true,
			wantOutput:  "model parameter is required",
			wantCurrent: "fast",
		},
		{
			name:        "rejects whitespace-only model",
			model:       "   ",
			wantError:   true,
			wantOutput:  "model parameter is required",
			wantCurrent: "fast",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			toolset, err := NewSwitchModelToolset([]string{"fast", "powerful"})
			require.NoError(t, err)

			result, err := toolset.callTool(ctx, SwitchModelArgs{Model: tt.model})

			require.NoError(t, err)
			assert.Equal(t, tt.wantError, result.IsError)
			assert.Contains(t, result.Output, tt.wantOutput)
			assert.Equal(t, tt.wantCurrent, toolset.CurrentModel())
		})
	}
}

func TestSwitchModelToolset_Tools(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	toolset, err := NewSwitchModelToolset([]string{"fast", "powerful"})
	require.NoError(t, err)

	tools, err := toolset.Tools(ctx)
	require.NoError(t, err)
	require.Len(t, tools, 1)

	tool := tools[0]
	assert.Equal(t, ToolNameSwitchModel, tool.Name)
	assert.Equal(t, "model", tool.Category)
	assert.True(t, tool.Annotations.ReadOnlyHint)
	assert.NotNil(t, tool.Handler)

	// Description includes model info
	assert.Contains(t, tool.Description, "fast (default) (current)")
	assert.Contains(t, tool.Description, "powerful")
	assert.Contains(t, tool.Description, "rejected")

	// After switching, description updates
	_, _ = toolset.callTool(ctx, SwitchModelArgs{Model: "powerful"})
	tools, _ = toolset.Tools(ctx)
	assert.Contains(t, tools[0].Description, "fast (default)")
	assert.Contains(t, tools[0].Description, "powerful (current)")
}

func TestSwitchModelToolset_Instructions(t *testing.T) {
	t.Parallel()

	toolset, err := NewSwitchModelToolset([]string{"fast", "powerful"})
	require.NoError(t, err)

	instructions := toolset.Instructions()

	assert.Contains(t, instructions, "Model Switching Guidelines")
	assert.Contains(t, instructions, "Switch to a faster/cheaper model")
	assert.Contains(t, instructions, "Switch to a more powerful model")
	assert.Contains(t, instructions, "Best Practices")
}

func TestSwitchModelToolset_OnSwitchCallback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		initialModel     string
		targetModel      string
		wantCallbackCall bool
		wantCallbackArg  string
	}{
		{
			name:             "callback called on successful switch",
			initialModel:     "fast",
			targetModel:      "powerful",
			wantCallbackCall: true,
			wantCallbackArg:  "powerful",
		},
		{
			name:             "callback not called when already on model",
			initialModel:     "fast",
			targetModel:      "fast",
			wantCallbackCall: false,
		},
		{
			name:             "callback not called for invalid model",
			initialModel:     "fast",
			targetModel:      "unknown",
			wantCallbackCall: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()

			toolset, err := NewSwitchModelToolset([]string{"fast", "powerful"})
			require.NoError(t, err)

			var callbackCalled bool
			var callbackArg string
			toolset.SetOnSwitchCallback(func(newModel string) error {
				callbackCalled = true
				callbackArg = newModel
				return nil
			})

			_, _ = toolset.callTool(ctx, SwitchModelArgs{Model: tt.targetModel})

			assert.Equal(t, tt.wantCallbackCall, callbackCalled, "callback called mismatch")
			if tt.wantCallbackCall {
				assert.Equal(t, tt.wantCallbackArg, callbackArg, "callback argument mismatch")
			}
		})
	}
}

func TestSwitchModelToolset_OnSwitchCallback_NilCallback(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	toolset, err := NewSwitchModelToolset([]string{"fast", "powerful"})
	require.NoError(t, err)

	// Ensure no panic when callback is nil
	result, err := toolset.callTool(ctx, SwitchModelArgs{Model: "powerful"})
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Equal(t, "powerful", toolset.CurrentModel())
}

func TestSwitchModelToolset_OnSwitchCallback_WithError(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	toolset, err := NewSwitchModelToolset([]string{"fast", "powerful"})
	require.NoError(t, err)

	// Set callback that returns an error
	callbackErr := fmt.Errorf("API key not configured")
	toolset.SetOnSwitchCallback(func(newModel string) error {
		return callbackErr
	})

	// Call the tool - should fail because callback returns error
	result, err := toolset.callTool(ctx, SwitchModelArgs{Model: "powerful"})
	require.NoError(t, err) // No Go error, but tool error
	assert.True(t, result.IsError, "should be a tool error")
	assert.Contains(t, result.Output, "Failed to switch model")
	assert.Contains(t, result.Output, "API key not configured")

	// Verify internal state was rolled back
	assert.Equal(t, "fast", toolset.CurrentModel(), "internal state should be rolled back to previous model")
}
