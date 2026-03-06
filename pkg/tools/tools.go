package tools

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ToolSet defines the interface for a set of tools.
type ToolSet interface {
	Tools(ctx context.Context) ([]Tool, error)
}

// NewHandler creates a type-safe tool handler from a function that accepts typed parameters.
// It handles JSON unmarshaling of the tool call arguments into the specified type T.
func NewHandler[T any](fn func(context.Context, T) (*ToolCallResult, error)) ToolHandler {
	return func(ctx context.Context, toolCall ToolCall) (*ToolCallResult, error) {
		var params T
		args := toolCall.Function.Arguments
		if args == "" {
			args = "{}"
		}
		if err := json.Unmarshal([]byte(args), &params); err != nil {
			return nil, err
		}
		return fn(ctx, params)
	}
}

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

// ImageContent represents a base64-encoded image returned by a tool.
type ImageContent struct {
	// Data is the base64-encoded image data.
	Data string `json:"data"`
	// MimeType is the MIME type of the image (e.g. "image/png", "image/jpeg").
	MimeType string `json:"mimeType"`
}

type ToolCallResult struct {
	Output  string `json:"output"`
	IsError bool   `json:"isError,omitempty"`
	Meta    any    `json:"meta,omitempty"`
	// Images contains optional image attachments returned by the tool.
	// When present, these are forwarded to the LLM as image content alongside
	// the text output.
	Images []ImageContent `json:"images,omitempty"`
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
