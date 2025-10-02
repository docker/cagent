package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"time"

	"github.com/docker/cagent/pkg/tools"
)

type FetchTool struct {
	timeout time.Duration
	client  *http.Client
}

var _ tools.ToolSet = (*FetchTool)(nil)

type fetchHandler struct {
	tool *FetchTool
}

func (h *fetchHandler) CallTool(ctx context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
	var params struct {
		URLs      []string          `json:"urls"`
		Headers   map[string]string `json:"headers,omitempty"`
		Method    string            `json:"method,omitempty"`
		Timeout   int               `json:"timeout,omitempty"`
		UserAgent string            `json:"userAgent,omitempty"`
	}

	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	if len(params.URLs) == 0 {
		return nil, fmt.Errorf("at least one URL is required")
	}

	// Set defaults
	if params.Method == "" {
		params.Method = "GET"
	}
	if params.UserAgent == "" {
		params.UserAgent = "cagent-fetch/1.0"
	}

	// Set timeout if specified
	client := h.tool.client
	if params.Timeout > 0 {
		timeout := time.Duration(params.Timeout) * time.Second
		client = &http.Client{Timeout: timeout}
	}

	var results []FetchResult
	for _, urlStr := range params.URLs {
		result := h.fetchURL(ctx, client, urlStr, params.Method, params.Headers, params.UserAgent)
		results = append(results, result)
	}

	// If only one URL, return simpler format
	if len(params.URLs) == 1 {
		result := results[0]
		if result.Error != "" {
			return &tools.ToolCallResult{Output: fmt.Sprintf("Error fetching %s: %s", result.URL, result.Error)}, nil
		}
		return &tools.ToolCallResult{
			Output: fmt.Sprintf("Successfully fetched %s (Status: %d, Length: %d bytes):\n\n%s",
				result.URL, result.StatusCode, result.ContentLength, result.Body),
		}, nil
	}

	// Multiple URLs - return structured results
	output, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal results: %w", err)
	}

	return &tools.ToolCallResult{Output: string(output)}, nil
}

type FetchResult struct {
	URL           string `json:"url"`
	StatusCode    int    `json:"statusCode"`
	Status        string `json:"status"`
	ContentType   string `json:"contentType,omitempty"`
	ContentLength int    `json:"contentLength"`
	Body          string `json:"body,omitempty"`
	Error         string `json:"error,omitempty"`
}

func (h *fetchHandler) fetchURL(ctx context.Context, client *http.Client, urlStr, method string, headers map[string]string, userAgent string) FetchResult {
	result := FetchResult{URL: urlStr}

	// Validate URL
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		result.Error = fmt.Sprintf("invalid URL: %v", err)
		return result
	}

	// Check for valid URL structure
	if parsedURL.Scheme == "" || parsedURL.Host == "" {
		result.Error = "invalid URL: missing scheme or host"
		return result
	}

	// Only allow HTTP and HTTPS
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		result.Error = "only HTTP and HTTPS URLs are supported"
		return result
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, method, urlStr, http.NoBody)
	if err != nil {
		result.Error = fmt.Sprintf("failed to create request: %v", err)
		return result
	}

	// Set User-Agent
	req.Header.Set("User-Agent", userAgent)

	// Set custom headers
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		result.Error = fmt.Sprintf("request failed: %v", err)
		return result
	}
	defer resp.Body.Close()

	result.StatusCode = resp.StatusCode
	result.Status = resp.Status
	result.ContentType = resp.Header.Get("Content-Type")

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		result.Error = fmt.Sprintf("failed to read response body: %v", err)
		return result
	}

	result.ContentLength = len(body)
	result.Body = string(body)

	return result
}

func NewFetchTool(options ...FetchToolOption) *FetchTool {
	tool := &FetchTool{
		timeout: 30 * time.Second,
	}

	// Apply options
	for _, opt := range options {
		opt(tool)
	}

	// Create HTTP client with timeout
	tool.client = &http.Client{
		Timeout: tool.timeout,
	}

	return tool
}

type FetchToolOption func(*FetchTool)

func WithTimeout(timeout time.Duration) FetchToolOption {
	return func(t *FetchTool) {
		t.timeout = timeout
	}
}

func (t *FetchTool) Instructions() string {
	return `## Fetch Tool Instructions

This tool allows you to fetch content from HTTP and HTTPS URLs.

### Features
- Support for multiple URLs in a single call
- Customizable HTTP headers
- Configurable request method (GET, POST, etc.)
- Timeout control
- User-Agent customization

### Security
- Only HTTP and HTTPS protocols are supported
- No local file access or other protocols
- Request timeouts prevent hanging requests

### Usage Tips
- Use single URLs for simple content fetching
- Use multiple URLs for batch operations
- Set appropriate headers for APIs that require authentication
- Consider timeout values for slow or large responses`
}

func (t *FetchTool) Tools(context.Context) ([]tools.Tool, error) {
	return []tools.Tool{
		{
			Function: &tools.FunctionDefinition{
				Name:        "fetch",
				Description: "Fetch content from one or more HTTP/HTTPS URLs. Returns the response body and metadata.",
				Annotations: tools.ToolAnnotation{
					ReadOnlyHint: &[]bool{true}[0],
					Title:        "Fetch URLs",
				},
				Parameters: tools.FunctionParameters{
					Type: "object",
					Properties: map[string]any{
						"urls": map[string]any{
							"type": "array",
							"items": map[string]any{
								"type": "string",
							},
							"description": "Array of URLs to fetch",
							"minItems":    1,
						},
						"method": map[string]any{
							"type":        "string",
							"description": "HTTP method to use (default: GET)",
							"default":     "GET",
							"enum":        []string{"GET", "POST", "PUT", "DELETE", "HEAD", "OPTIONS", "PATCH"},
						},
						"headers": map[string]any{
							"type": "object",
							"additionalProperties": map[string]any{
								"type": "string",
							},
							"description": "Optional HTTP headers to send with the request",
						},
						"timeout": map[string]any{
							"type":        "integer",
							"description": "Request timeout in seconds (default: 30)",
							"minimum":     1,
							"maximum":     300,
						},
						"userAgent": map[string]any{
							"type":        "string",
							"description": "Custom User-Agent header (default: cagent-fetch/1.0)",
						},
					},
					Required: []string{"urls"},
				},
				OutputSchema: tools.ToOutputSchemaSchema(reflect.TypeFor[string]()),
			},
			Handler: (&fetchHandler{tool: t}).CallTool,
		},
	}, nil
}

func (t *FetchTool) Start(context.Context) error {
	return nil
}

func (t *FetchTool) Stop() error {
	return nil
}
