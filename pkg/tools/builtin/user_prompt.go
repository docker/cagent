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

Use the user_prompt tool when you need to ask the user a question or gather input interactively.
This tool displays a dialog to the user and waits for their response.

### When to use this tool:
- When you need clarification from the user before proceeding
- When you need to collect specific information (e.g., credentials, preferences, choices)
- When the user needs to make a decision between multiple options

### Schema support:
You can optionally provide a JSON schema to define the expected response structure:
- Object schemas with properties for collecting multiple fields
- Primitive type schemas (string, number, boolean) for simple inputs
- Enum types for presenting a list of choices
- Required fields to ensure necessary information is collected

### Example schemas:

Simple string input:
{
  "type": "string",
  "title": "API Key",
  "description": "Enter your API key"
}

Multiple choice:
{
  "type": "string",
  "enum": ["option1", "option2", "option3"],
  "title": "Select an option"
}

Object with multiple fields:
{
  "type": "object",
  "properties": {
    "username": {"type": "string", "description": "Your username"},
    "remember": {"type": "boolean", "description": "Remember me"}
  },
  "required": ["username"]
}

### Response format:
The tool returns a JSON object with:
- action: "accept" (user provided response), "decline" (user declined), or "cancel" (user cancelled)
- content: The user's response data (only present when action is "accept")`
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
