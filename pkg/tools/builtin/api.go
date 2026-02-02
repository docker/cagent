package builtin

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/js"
	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/useragent"
)

type APITool struct {
	config latest.APIToolConfig
}

// Verify interface compliance
var (
	_ tools.ToolSet      = (*APITool)(nil)
	_ tools.Instructable = (*APITool)(nil)
)

func (t *APITool) callTool(ctx context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	endpoint := t.config.Endpoint
	var reqBody io.Reader = http.NoBody
	switch t.config.Method {
	case http.MethodGet:
		if toolCall.Function.Arguments != "" {
			var params map[string]string

			if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &params); err != nil {
				return nil, fmt.Errorf("invalid arguments: %w", err)
			}
			expanded, err := js.ExpandString(ctx, endpoint, params)
			if err != nil {
				return nil, fmt.Errorf("failed to expand endpoint: %w", err)
			}
			endpoint = expanded
		}
	case http.MethodPost:
		var params map[string]any

		if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &params); err != nil {
			return nil, fmt.Errorf("invalid arguments: %w", err)
		}
		jsonData, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %v", err)
		}
		reqBody = bytes.NewReader(jsonData)
	}

	req, err := http.NewRequestWithContext(ctx, t.config.Method, endpoint, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("User-Agent", useragent.Header)
	if t.config.Method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}

	for key, value := range t.config.Headers {
		req.Header.Set(key, value)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %v", err)
	}
	defer resp.Body.Close()

	maxSize := int64(1 << 20)
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSize))
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %v", err)
	}

	return tools.ResultSuccess(limitOutput(string(body))), nil
}

func NewAPITool(config latest.APIToolConfig) *APITool {
	return &APITool{
		config: config,
	}
}

func (t *APITool) Instructions() string {
	return t.config.Instruction
}

func (t *APITool) Tools(context.Context) ([]tools.Tool, error) {
	inputSchema, err := tools.SchemaToMap(map[string]any{
		"type":       "object",
		"properties": t.config.Args,
		"required":   t.config.Required,
	})
	if err != nil {
		return nil, fmt.Errorf("invalid schema: %w", err)
	}

	parsedURL, err := url.Parse(t.config.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	if parsedURL.Scheme == "" || parsedURL.Host == "" {
		return nil, fmt.Errorf("invalid URL: missing scheme or host")
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return nil, fmt.Errorf("only HTTP and HTTPS URLs are supported")
	}

	outputSchema := tools.MustSchemaFor[string]()
	if t.config.OutputSchema != nil {
		var err error
		outputSchema, err = tools.SchemaToMap(t.config.OutputSchema)
		if err != nil {
			return nil, fmt.Errorf("invalid output_schema: %w", err)
		}
	}

	return []tools.Tool{
		{
			Name:         t.config.Name,
			Category:     "api",
			Description:  t.config.Instruction,
			Parameters:   inputSchema,
			OutputSchema: outputSchema,
			Handler:      t.callTool,
			Annotations: tools.ToolAnnotations{
				ReadOnlyHint: true,
				Title:        cmp.Or(t.config.Name, "Query API"),
			},
		},
	}, nil
}
