package builtin

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/docker/cagent/pkg/tools"
)

const ToolNameSwitchModel = "switch_model"

// ModelSwitchCallback is called when the model is switched.
// It returns an error if the switch failed.
type ModelSwitchCallback func(newModel string) error

// SwitchModelToolset provides a tool that allows agents to switch between
// a predefined set of models during a conversation.
type SwitchModelToolset struct {
	mu           sync.RWMutex
	models       []string
	currentModel string              // currently selected model
	onSwitch     ModelSwitchCallback // optional callback when model changes
}

// Verify interface compliance
var (
	_ tools.ToolSet      = (*SwitchModelToolset)(nil)
	_ tools.Instructable = (*SwitchModelToolset)(nil)
)

type SwitchModelArgs struct {
	Model string `json:"model" jsonschema:"The model to switch to. Must be one of the allowed models listed in the tool description."`
}

// NewSwitchModelToolset creates a new switch_model toolset with the given allowed models.
// The first model in the list becomes the default and initially selected model.
// Panics if models is empty or contains empty strings.
func NewSwitchModelToolset(models []string) (*SwitchModelToolset, error) {
	if len(models) == 0 {
		return nil, fmt.Errorf("switch_model toolset requires at least one model")
	}
	for i, m := range models {
		if strings.TrimSpace(m) == "" {
			return nil, fmt.Errorf("switch_model toolset: model at index %d is empty", i)
		}
	}

	return &SwitchModelToolset{
		models:       slices.Clone(models),
		currentModel: models[0],
	}, nil
}

// CurrentModel returns the currently selected model.
func (t *SwitchModelToolset) CurrentModel() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.currentModel
}

// SetOnSwitchCallback sets a callback that will be invoked whenever the model is switched.
// The callback receives the new model name. This allows the runtime to react to model changes.
func (t *SwitchModelToolset) SetOnSwitchCallback(callback ModelSwitchCallback) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.onSwitch = callback
}

// Instructions returns guidance for when to use model switching.
func (t *SwitchModelToolset) Instructions() string {
	return `## Model Switching Guidelines

You have access to multiple AI models and can switch between them strategically.

### When to Consider Switching Models

**Switch to a faster/cheaper model when:**
- Performing simple, routine tasks (formatting, basic Q&A, short summaries)
- The current task doesn't require advanced reasoning
- Processing straightforward requests that any model can handle well
- Optimizing for response speed or cost efficiency

**Switch to a more powerful model when:**
- Facing complex reasoning or multi-step problems
- Writing or reviewing code that requires careful analysis
- Handling nuanced or ambiguous requests
- Generating detailed technical content
- The current model is struggling with the task quality

**Switch back to the default model when:**
- A specialized task is complete
- Returning to general conversation
- The extra capability is no longer needed

### Best Practices

1. Check the tool description to see available models and which one is currently active
2. Don't switch unnecessarily - there's overhead in changing models
3. Consider switching proactively before a complex task rather than after struggling
4. When in doubt about task complexity, prefer the more capable model`
}

// callTool handles the switch_model tool invocation.
func (t *SwitchModelToolset) callTool(_ context.Context, params SwitchModelArgs) (*tools.ToolCallResult, error) {
	requestedModel := strings.TrimSpace(params.Model)
	if requestedModel == "" {
		return tools.ResultError("model parameter is required and cannot be empty"), nil
	}

	// Check if the requested model is in the allowed list
	if !slices.Contains(t.models, requestedModel) {
		return tools.ResultError(fmt.Sprintf(
			"model %q is not allowed. Available models: %s",
			requestedModel,
			strings.Join(t.models, ", "),
		)), nil
	}

	// Get current state and callback atomically
	t.mu.RLock()
	previousModel := t.currentModel
	callback := t.onSwitch
	t.mu.RUnlock()

	// No-op if already on the requested model
	if previousModel == requestedModel {
		return tools.ResultSuccess(fmt.Sprintf("Model is already set to %q.", requestedModel)), nil
	}

	// Notify the runtime about the model change (before updating internal state)
	if callback != nil {
		if err := callback(requestedModel); err != nil {
			return tools.ResultError(fmt.Sprintf("Failed to switch model: %v", err)), nil
		}
	}

	// Update internal state after successful callback
	t.mu.Lock()
	t.currentModel = requestedModel
	t.mu.Unlock()

	return tools.ResultSuccess(fmt.Sprintf("Switched model from %q to %q.", previousModel, requestedModel)), nil
}

// Tools returns the switch_model tool definition.
func (t *SwitchModelToolset) Tools(context.Context) ([]tools.Tool, error) {
	t.mu.RLock()
	currentModel := t.currentModel
	t.mu.RUnlock()

	description := t.buildDescription(currentModel)

	return []tools.Tool{
		{
			Name:         ToolNameSwitchModel,
			Category:     "model",
			Description:  description,
			Parameters:   tools.MustSchemaFor[SwitchModelArgs](),
			OutputSchema: tools.MustSchemaFor[string](),
			Handler:      tools.NewHandler(t.callTool),
			Annotations: tools.ToolAnnotations{
				ReadOnlyHint: true,
				Title:        "Switch Model",
			},
		},
	}, nil
}

// buildDescription generates the tool description with current state.
func (t *SwitchModelToolset) buildDescription(currentModel string) string {
	var sb strings.Builder

	sb.WriteString("Switch the AI model used for subsequent responses.\n\n")
	sb.WriteString("**Available models:**\n")
	for _, m := range t.models {
		fmt.Fprintf(&sb, "- %s", m)
		if m == t.models[0] {
			sb.WriteString(" (default)")
		}
		if m == currentModel {
			sb.WriteString(" (current)")
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	sb.WriteString("Only the models listed above can be selected. ")
	sb.WriteString("Any other model will be rejected.")

	return sb.String()
}
