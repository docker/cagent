package builtin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/docker/cagent/pkg/tools"
)

const ToolNameUserPrompt = "user_prompt"

type UserPromptTool struct {
	elicitationHandler tools.ElicitationHandler
}

// Verify interface compliance
var (
	_ tools.ToolSet      = (*UserPromptTool)(nil)
	_ tools.Elicitable   = (*UserPromptTool)(nil)
	_ tools.Instructable = (*UserPromptTool)(nil)
)

type UserPromptArgs struct {
	Message string         `json:"message" jsonschema:"The message/question to display to the user"`
	Schema  map[string]any `json:"schema,omitempty" jsonschema:"JSON Schema defining the expected response structure. Supports object schemas with properties or primitive type schemas."`
}

type UserPromptResponse struct {
	Action  string         `json:"action" jsonschema:"The user action: accept, decline, or cancel"`
	Content map[string]any `json:"content,omitempty" jsonschema:"The user response data (only present when action is accept)"`
}

func NewUserPromptTool() *UserPromptTool {
	return &UserPromptTool{}
}

func (t *UserPromptTool) SetElicitationHandler(handler tools.ElicitationHandler) {
	t.elicitationHandler = handler
}

func (t *UserPromptTool) userPrompt(ctx context.Context, params UserPromptArgs) (*tools.ToolCallResult, error) {
	if t.elicitationHandler == nil {
		return tools.ResultError("user_prompt tool is not available in this context (no elicitation handler configured)"), nil
	}

	req := &mcp.ElicitParams{
		Message:         params.Message,
		RequestedSchema: params.Schema,
	}

	result, err := t.elicitationHandler(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("elicitation request failed: %w", err)
	}

	response := UserPromptResponse{
		Action:  string(result.Action),
		Content: result.Content,
	}

	responseJSON, err := json.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}

	if result.Action != tools.ElicitationActionAccept {
		return &tools.ToolCallResult{
			Output:  string(responseJSON),
			IsError: true,
		}, nil
	}

	return tools.ResultSuccess(string(responseJSON)), nil
}

func (t *UserPromptTool) Instructions() string {
	return `## Using the user_prompt tool

Use user_prompt to ask the user a question or gather input when you need clarification, specific information, or a decision.

Optionally provide a JSON schema to structure the expected response (object, primitive, or enum types).

Example schema for multiple choice:
{"type": "string", "enum": ["option1", "option2"], "title": "Select an option"}

Example schema for structured input:
{"type": "object", "properties": {"name": {"type": "string"}}, "required": ["name"]}

Response contains "action" (accept/decline/cancel) and "content" (user data, only when accepted).`
}

func (t *UserPromptTool) Tools(context.Context) ([]tools.Tool, error) {
	return []tools.Tool{
		{
			Name:         ToolNameUserPrompt,
			Category:     "user_prompt",
			Description:  "Ask the user a question and wait for their response. Use this when you need interactive input, clarification, or confirmation from the user. Optionally provide a JSON schema to define the expected response structure.",
			Parameters:   tools.MustSchemaFor[UserPromptArgs](),
			OutputSchema: tools.MustSchemaFor[UserPromptResponse](),
			Handler:      tools.NewHandler(t.userPrompt),
			Annotations: tools.ToolAnnotations{
				ReadOnlyHint: true,
				Title:        "User Prompt",
			},
		},
	}, nil
}
