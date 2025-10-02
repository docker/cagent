package tools

import (
	"context"
	"encoding/json"
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
	Function *FunctionDefinition `json:"function,omitempty"`
	Handler  ToolHandler         `json:"-"`
}

type FunctionDefinition struct {
	Name         string             `json:"name"`
	Description  string             `json:"description,omitempty"`
	Strict       bool               `json:"strict,omitempty"`
	Parameters   FunctionParameters `json:"parameters"`
	Annotations  ToolAnnotation     `json:"annotations"`
	OutputSchema ToolOutputSchema   `json:"outputSchema"`
}

type ToolAnnotation struct {
	Title           string `json:"title,omitempty"`
	ReadOnlyHint    *bool  `json:"readOnlyHint,omitempty"`
	DestructiveHint *bool  `json:"destructiveHint,omitempty"`
	IdempotentHint  *bool  `json:"idempotentHint,omitempty"`
	OpenWorldHint   *bool  `json:"openWorldHint,omitempty"`
}

type ToolOutputSchema struct {
	Ref        string         `json:"$ref,omitempty"`
	Type       string         `json:"type"`
	Items      map[string]any `json:"items,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`
	Required   []string       `json:"required,omitempty"`
}

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

type ToolSet interface {
	Tools(ctx context.Context) ([]Tool, error)
	Instructions() string
	Start(ctx context.Context) error
	Stop() error
}
