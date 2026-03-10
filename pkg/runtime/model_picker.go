package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/docker/docker-agent/pkg/session"
	"github.com/docker/docker-agent/pkg/tools"
	"github.com/docker/docker-agent/pkg/tools/builtin"
)

// findModelPickerTool returns the ModelPickerTool from the current agent's
// toolsets, or nil if the agent has no model_picker configured.
func (r *LocalRuntime) findModelPickerTool() *builtin.ModelPickerTool {
	currentName := r.CurrentAgentName()
	a, err := r.team.Agent(currentName)
	if err != nil {
		return nil
	}
	for _, ts := range a.ToolSets() {
		if mpt, ok := tools.As[*builtin.ModelPickerTool](ts); ok {
			return mpt
		}
	}
	return nil
}

// handleChangeModel handles the change_model tool call by switching the current agent's model.
func (r *LocalRuntime) handleChangeModel(ctx context.Context, _ *session.Session, toolCall tools.ToolCall, _ chan Event) (*tools.ToolCallResult, error) {
	var params builtin.ChangeModelArgs
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	if params.Model == "" {
		return tools.ResultError("model parameter is required"), nil
	}

	// Validate the requested model against the allowed list
	mpt := r.findModelPickerTool()
	if mpt == nil {
		return tools.ResultError("model_picker is not configured for this agent"), nil
	}
	allowed := mpt.AllowedModels()
	if !slices.Contains(allowed, params.Model) {
		return tools.ResultError(fmt.Sprintf(
			"model %q is not in the allowed list. Available models: %s",
			params.Model, strings.Join(allowed, ", "),
		)), nil
	}

	return r.setCurrentAgentModel(ctx, params.Model)
}

// handleRevertModel handles the revert_model tool call by reverting the current agent to its default model.
func (r *LocalRuntime) handleRevertModel(ctx context.Context, _ *session.Session, _ tools.ToolCall, _ chan Event) (*tools.ToolCallResult, error) {
	return r.setCurrentAgentModel(ctx, "")
}

// setCurrentAgentModel sets the model for the current agent. An empty modelRef
// reverts to the agent's default model. The main loop detects the resulting
// model change and automatically notifies the TUI, so no AgentInfo event is
// emitted here.
func (r *LocalRuntime) setCurrentAgentModel(ctx context.Context, modelRef string) (*tools.ToolCallResult, error) {
	currentName := r.CurrentAgentName()
	if err := r.SetAgentModel(ctx, currentName, modelRef); err != nil {
		return tools.ResultError(fmt.Sprintf("failed to set model: %v", err)), nil
	}

	if modelRef == "" {
		slog.Info("Model reverted via model_picker tool", "agent", currentName)
		return tools.ResultSuccess("Model reverted to the agent's default model"), nil
	}
	slog.Info("Model changed via model_picker tool", "agent", currentName, "model", modelRef)
	return tools.ResultSuccess("Model changed to " + modelRef), nil
}
