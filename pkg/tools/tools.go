package tools

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ToolHandler = func(ctx context.Context, toolCall ToolCall) (*ToolCallResult, error)

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
	Output string `json:"output"`
}

// OpenAI-like Tool Types

type ToolType string

type Tool struct {
	Name         string             `json:"name"`
	Description  string             `json:"description,omitempty"`
	Parameters   FunctionParameters `json:"parameters"`
	Annotations  ToolAnnotations    `json:"annotations"`
	OutputSchema any                `json:"outputSchema"`
	Handler      ToolHandler        `json:"-"`
}

type ToolAnnotations mcp.ToolAnnotations

type FunctionParameters struct {
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties"`
	Required   []string       `json:"required,omitempty"`
}

func (fp FunctionParameters) MarshalJSON() ([]byte, error) {
	type Alias FunctionParameters
	if fp.Type == "" {
		fp.Type = "object"
	}
	if fp.Properties == nil {
		fp.Properties = map[string]any{}
	}
	return json.Marshal(Alias(fp))
}

type ElicitationResult struct {
	Action  string         `json:"action"` // "accept", "decline", or "cancel"
	Content map[string]any `json:"content,omitempty"`
}

// ElicitationHandler is a function type that handles elicitation requests from the MCP server
// This allows the runtime to handle elicitation requests and propagate them to its own client
type ElicitationHandler func(ctx context.Context, req *mcp.ElicitParams) (ElicitationResult, error)

type ToolSet interface {
	Tools(ctx context.Context) ([]Tool, error)
	Instructions() string
	Start(ctx context.Context) error
	Stop() error
	SetElicitationHandler(handler ElicitationHandler)
	SetOAuthSuccessHandler(handler func())
}
