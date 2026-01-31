package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/k3a/html2text"
	"github.com/temoto/robotstxt"

	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/useragent"
)

const (
	ToolNameFetch = "fetch"
)

type FetchTool struct {
	handler *fetchHandler
}

// Verify interface compliance
var (
	_ tools.ToolSet      = (*FetchTool)(nil)
	_ tools.Instructable = (*FetchTool)(nil)
)

type fetchHandler struct {
	timeout time.Duration
}

type FetchToolArgs struct {
	URLs    []string `json:"urls"`
	Timeout int      `json:"timeout,omitempty"`
	Format  string   `json:"format,omitempty"`
}

func (h *fetchHandler) CallTool(ctx context.Context, params FetchToolArgs) (*tools.ToolCallResult, error) {
	if len(params.URLs) == 0 {
		return nil, fmt.Errorf("at least one URL is required")
	}

	// Set timeout if specified
	client := &http.Client{
		Timeout: h.timeout,
	}
	if params.Timeout > 0 {
		client.Timeout = time.Duration(params.Timeout) * time.Second
	}

	var results []FetchResult

	// Group URLs by host to fetch robots.txt once per host
	robotsCache := make(map[string]bool)

	for _, urlStr := range params.URLs {
		result := h.fetchURL(ctx, client, urlStr, params.Format, robotsCache)
		results = append(results, result)
	}

	// If only one URL, return simpler format
	if len(params.URLs) == 1 {
		result := results[0]
		if result.Error != "" {
			return tools.ResultError(fmt.Sprintf("Error fetching %s: %s", result.URL, result.Error)), nil
		}
		return tools.ResultSuccess(fmt.Sprintf("Successfully fetched %s (Status: %d, Length: %d bytes):\n\n%s",
			result.URL, result.StatusCode, result.ContentLength, result.Body)), nil
	}

	// Multiple URLs - return structured results
	output, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal results: %w", err)
	}

	return tools.ResultSuccess(string(output)), nil
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

func (h *fetchHandler) fetchURL(ctx context.Context, client *http.Client, urlStr, format string, robotsCache map[string]bool) FetchResult {
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

	// Check robots.txt (with caching per host)
	host := parsedURL.Host
	allowed, cached := robotsCache[host]
	if !cached {
		allowed = h.checkRobotsAllowed(ctx, client, parsedURL, useragent.Header)
		robotsCache[host] = allowed
	}

	if !allowed {
		result.Error = "URL blocked by robots.txt"
		return result
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, http.NoBody)
	if err != nil {
		result.Error = fmt.Sprintf("failed to create request: %v", err)
		return result
	}

	req.Header.Set("User-Agent", useragent.Header)

	switch format {
	case "markdown":
		req.Header.Set("Accept", "text/markdown;q=1.0, text/plain;q=0.9, text/html;q=0.7, */*;q=0.1")
	case "html":
		req.Header.Set("Accept", "text/html;q=1.0, text/plain;q=0.8, */*;q=0.1")
	case "text":
		req.Header.Set("Accept", "text/plain;q=1.0,  text/markdown;q=0.9, text/html;q=0.8, */*;q=0.1")
	default:
		req.Header.Set("Accept", "text/plain;q=1.0, */*;q=0.1")
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
	maxSize := int64(1 << 20) // 1MB
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSize))
	if err != nil {
		result.Error = fmt.Sprintf("failed to read response body: %v", err)
		return result
	}

	contentType := resp.Header.Get("Content-Type")

	switch format {
	case "markdown":
		if strings.Contains(contentType, "text/html") {
			result.Body = htmlToMarkdown(string(body))
		} else {
			result.Body = string(body)
		}
	case "html":
		result.Body = string(body)
	case "text":
		if strings.Contains(contentType, "text/html") {
			result.Body = htmlToText(string(body))
		} else {
			result.Body = string(body)
		}
	default:
		result.Body = string(body)
	}

	result.ContentLength = len(result.Body)

	return result
}

func (h *fetchHandler) checkRobotsAllowed(ctx context.Context, client *http.Client, targetURL *url.URL, userAgent string) bool {
	// Build robots.txt URL
	robotsURL := &url.URL{
		Scheme: targetURL.Scheme,
		Host:   targetURL.Host,
		Path:   "/robots.txt",
	}

	// Create request for robots.txt
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, robotsURL.String(), http.NoBody)
	if err != nil {
		// If we can't create request, allow the fetch
		return true
	}

	req.Header.Set("User-Agent", userAgent)

	// Create robots client with same timeout and transport as main client
	robotsClient := &http.Client{
		Timeout:   client.Timeout,   // Same timeout as main client
		Transport: client.Transport, // Inherit proxy/transport settings
	}

	resp, err := robotsClient.Do(req)
	if err != nil {
		// If robots.txt is unreachable, allow the fetch
		return true
	}
	defer resp.Body.Close()

	// If robots.txt doesn't exist (404), allow the fetch
	if resp.StatusCode == http.StatusNotFound {
		return true
	}

	// For other non-200 status codes, fail the fetch
	if resp.StatusCode != http.StatusOK {
		return false
	}

	// Read robots.txt content (limit to 64KB)
	robotsBody, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		// If we can't read robots.txt, fail the fetch
		return false
	}

	// Parse robots.txt
	robots, err := robotstxt.FromBytes(robotsBody)
	if err != nil {
		// If we can't parse robots.txt, fail the fetch
		return false
	}

	// Check if the target URL path is allowed for our user agent
	return robots.TestAgent(targetURL.Path, userAgent)
}

func htmlToMarkdown(html string) string {
	markdown, err := htmltomarkdown.ConvertString(html)
	if err != nil {
		return html
	}
	return markdown
}

func htmlToText(html string) string {
	return html2text.HTML2Text(html)
}

func NewFetchTool(options ...FetchToolOption) *FetchTool {
	tool := &FetchTool{
		handler: &fetchHandler{
			timeout: 30 * time.Second,
		},
	}

	for _, opt := range options {
		opt(tool)
	}

	return tool
}

type FetchToolOption func(*FetchTool)

func WithTimeout(timeout time.Duration) FetchToolOption {
	return func(t *FetchTool) {
		t.handler.timeout = timeout
	}
}

func (t *FetchTool) Instructions() string {
	return `## "fetch" tool instructions

This tool allows you to fetch content from HTTP and HTTPS URLs.

FEATURES

- Support for multiple URLs in a single call
- Returns response body and metadata (status code, content type, length)
- Specify the output format (text, markdown, html)
- Respects robots.txt restrictions

USAGE TIPS
- Use single URLs for simple content fetching
- Use multiple URLs for batch operations`
}

func (t *FetchTool) Tools(context.Context) ([]tools.Tool, error) {
	return []tools.Tool{
		{
			Name:        ToolNameFetch,
			Category:    "fetch",
			Description: "Fetch content from one or more HTTP/HTTPS URLs. Returns the response body and metadata.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"urls": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "string",
						},
						"description": "Array of URLs to fetch",
						"minItems":    1,
					},
					"format": map[string]any{
						"type":        "string",
						"description": "The format to return the content in (text, markdown, or html)",
						"enum":        []string{"text", "markdown", "html"},
					},
					"timeout": map[string]any{
						"type":        "integer",
						"description": "Request timeout in seconds (default: 30)",
						"minimum":     1,
						"maximum":     300,
					},
				},
				"required": []string{"urls", "format"},
			},
			OutputSchema: tools.MustSchemaFor[string](),
			Handler:      tools.NewHandler(t.handler.CallTool),
			Annotations: tools.ToolAnnotations{
				ReadOnlyHint: true,
				Title:        "Fetch URLs",
			},
		},
	}, nil
}
