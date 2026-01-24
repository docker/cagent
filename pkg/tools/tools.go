package tools

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ToolHandler func(ctx context.Context, toolCall ToolCall) (*ToolCallResult, error)

type ToolCall struct {
	ID       string       `json:"id,omitempty"`
	Type     ToolType     `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type ToolCallResult struct {
	Output  string `json:"output"`
	IsError bool   `json:"isError,omitempty"`
	Meta    any    `json:"meta,omitempty"`
}

func ResultError(output string) *ToolCallResult {
	return &ToolCallResult{
		Output:  output,
		IsError: true,
	}
}

func ResultSuccess(output string) *ToolCallResult {
	return &ToolCallResult{
		Output:  output,
		IsError: false,
	}
}

// OpenAI-like Tool Types

type ToolType string

type Tool struct {
	Name                    string          `json:"name"`
	Category                string          `json:"category"`
	Description             string          `json:"description,omitempty"`
	Parameters              any             `json:"parameters"`
	Annotations             ToolAnnotations `json:"annotations"`
	OutputSchema            any             `json:"outputSchema"`
	Handler                 ToolHandler     `json:"-"`
	AddDescriptionParameter bool            `json:"-"`
}

type ToolAnnotations mcp.ToolAnnotations

type ElicitationAction string

const (
	ElicitationActionAccept  ElicitationAction = "accept"
	ElicitationActionDecline ElicitationAction = "decline"
	ElicitationActionCancel  ElicitationAction = "cancel"
)

type ElicitationResult struct {
	Action  ElicitationAction `json:"action"`
	Content map[string]any    `json:"content,omitempty"`
}

// ElicitationHandler is a function type that handles elicitation requests from the MCP server
// This allows the runtime to handle elicitation requests and propagate them to its own client
type ElicitationHandler func(ctx context.Context, req *mcp.ElicitParams) (ElicitationResult, error)

// BaseToolSet provides default no-op implementations for common ToolSet methods.
// Embed this in tool implementations to reduce boilerplate.
type BaseToolSet struct{}

// Start is a no-op implementation.
func (BaseToolSet) Start(context.Context) error { return nil }

// Stop is a no-op implementation.
func (BaseToolSet) Stop(context.Context) error { return nil }

// Instructions returns an empty string by default.
func (BaseToolSet) Instructions() string { return "" }

// SetElicitationHandler is a no-op for tools that don't use elicitation.
func (BaseToolSet) SetElicitationHandler(ElicitationHandler) {}

// SetOAuthSuccessHandler is a no-op for tools that don't use OAuth.
func (BaseToolSet) SetOAuthSuccessHandler(func()) {}

// SetManagedOAuth is a no-op for tools that don't use OAuth.
func (BaseToolSet) SetManagedOAuth(bool) {}

type ToolSet interface {
	Tools(ctx context.Context) ([]Tool, error)
	Instructions() string
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	SetElicitationHandler(handler ElicitationHandler)
	SetOAuthSuccessHandler(handler func())
	SetManagedOAuth(managed bool)
}

// NewHandler creates a type-safe tool handler from a function that accepts typed parameters.
// It handles JSON unmarshaling of the tool call arguments into the specified type T.
func NewHandler[T any](fn func(context.Context, T) (*ToolCallResult, error)) ToolHandler {
	return func(ctx context.Context, toolCall ToolCall) (*ToolCallResult, error) {
		var params T
		if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &params); err != nil {
			return nil, err
		}
		return fn(ctx, params)
	}
}
