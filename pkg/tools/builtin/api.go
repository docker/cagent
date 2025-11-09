package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	latest "github.com/docker/cagent/pkg/config/v2"
	"github.com/docker/cagent/pkg/js"
	"github.com/docker/cagent/pkg/tools"
)

const (
	ToolNameAPI = "api"
)

type APITool struct {
	tools.ElicitationTool
	handler *apiHandler
	config  latest.APIToolConfig
}

var _ tools.ToolSet = (*APITool)(nil)

type apiHandler struct {
	config latest.APIToolConfig
}

func (h *apiHandler) CallTool(ctx context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	endpoint := h.config.Endpoint
	var reqBody io.Reader = http.NoBody
	switch h.config.Method {
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

	req, err := http.NewRequestWithContext(ctx, h.config.Method, endpoint, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("User-Agent", userAgent)
	if h.config.Method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}

	for key, value := range h.config.Headers {
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

	return &tools.ToolCallResult{Output: string(body)}, nil
}

type APIToolOption func(*APITool)

func NewAPITool(config latest.APIToolConfig) *APITool {
	return &APITool{
		config: config,
		handler: &apiHandler{
			config: config,
		},
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

	return []tools.Tool{
		{
			Name:         t.config.Name,
			Category:     "api",
			Description:  t.config.Instruction,
			Parameters:   inputSchema,
			OutputSchema: tools.MustSchemaFor[string](),
			Handler:      t.handler.CallTool,
			Annotations: tools.ToolAnnotations{
				ReadOnlyHint: true,
				Title:        "API URLs",
			},
		},
	}, nil
}

func (t *APITool) Start(context.Context) error {
	return nil
}

func (t *APITool) Stop(context.Context) error {
	return nil
}
