package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/docker/cagent/pkg/tools"
)

func TestNewFetchTool(t *testing.T) {
	tool := NewFetchTool()
	if tool == nil {
		t.Fatal("NewFetchTool() returned nil")
	}

	if tool.timeout != 30*time.Second {
		t.Errorf("Expected default timeout 30s, got %v", tool.timeout)
	}

	if tool.client == nil {
		t.Fatal("HTTP client not initialized")
	}
}

func TestFetchToolWithOptions(t *testing.T) {
	customTimeout := 60 * time.Second
	tool := NewFetchTool(WithTimeout(customTimeout))

	if tool.timeout != customTimeout {
		t.Errorf("Expected timeout %v, got %v", customTimeout, tool.timeout)
	}
}

func TestFetchTool_Tools(t *testing.T) {
	tool := NewFetchTool()
	ctx := context.TODO()

	toolSet, err := tool.Tools(ctx)
	if err != nil {
		t.Fatalf("Tools() error: %v", err)
	}

	if len(toolSet) != 1 {
		t.Fatalf("Expected 1 tool, got %d", len(toolSet))
	}

	fetchTool := toolSet[0]
	if fetchTool.Function.Name != "fetch" {
		t.Errorf("Expected tool name 'fetch', got %s", fetchTool.Function.Name)
	}

	if fetchTool.Handler == nil {
		t.Fatal("Tool handler is nil")
	}
}

func TestFetchTool_Instructions(t *testing.T) {
	tool := NewFetchTool()
	instructions := tool.Instructions()

	if instructions == "" {
		t.Fatal("Instructions should not be empty")
	}

	if !containsAllSubstrings(instructions, []string{"Fetch Tool Instructions", "HTTP", "HTTPS", "URLs"}) {
		t.Error("Instructions missing expected content")
	}
}

func TestFetchTool_StartStop(t *testing.T) {
	tool := NewFetchTool()
	ctx := context.TODO()

	if err := tool.Start(ctx); err != nil {
		t.Errorf("Start() error: %v", err)
	}

	if err := tool.Stop(); err != nil {
		t.Errorf("Stop() error: %v", err)
	}
}

func TestFetchHandler_CallTool_Success(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "Hello, World!")
	}))
	defer server.Close()

	tool := NewFetchTool()
	ctx := context.TODO()

	args := map[string]any{
		"urls": []string{server.URL},
	}
	argsJSON, _ := json.Marshal(args)

	toolCall := tools.ToolCall{
		Function: tools.FunctionCall{
			Arguments: string(argsJSON),
		},
	}

	handler := &fetchHandler{tool: tool}
	result, err := handler.CallTool(ctx, toolCall)
	if err != nil {
		t.Fatalf("CallTool() error: %v", err)
	}

	if result == nil {
		t.Fatal("Result is nil")
	}

	if !containsAllSubstrings(result.Output, []string{"Successfully fetched", "Status: 200", "Hello, World!"}) {
		t.Errorf("Unexpected output: %s", result.Output)
	}
}

func TestFetchHandler_CallTool_MultipleURLs(t *testing.T) {
	// Create test servers
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "Server 1")
	}))
	defer server1.Close()

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "Server 2")
	}))
	defer server2.Close()

	tool := NewFetchTool()
	ctx := context.TODO()

	args := map[string]any{
		"urls": []string{server1.URL, server2.URL},
	}
	argsJSON, _ := json.Marshal(args)

	toolCall := tools.ToolCall{
		Function: tools.FunctionCall{
			Arguments: string(argsJSON),
		},
	}

	handler := &fetchHandler{tool: tool}
	result, err := handler.CallTool(ctx, toolCall)
	if err != nil {
		t.Fatalf("CallTool() error: %v", err)
	}

	// Should return JSON for multiple URLs
	var results []FetchResult
	if err := json.Unmarshal([]byte(result.Output), &results); err != nil {
		t.Fatalf("Failed to unmarshal results: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("Expected 2 results, got %d", len(results))
	}

	if results[0].Body != "Server 1" {
		t.Errorf("Expected 'Server 1', got %s", results[0].Body)
	}

	if results[1].Body != "Server 2" {
		t.Errorf("Expected 'Server 2', got %s", results[1].Body)
	}
}

func TestFetchHandler_CallTool_CustomHeaders(t *testing.T) {
	// Create test server that checks headers
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token123" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Header.Get("X-Custom-Header") != "custom-value" {
			http.Error(w, "Missing custom header", http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, "Authorized!")
	}))
	defer server.Close()

	tool := NewFetchTool()
	ctx := context.TODO()

	args := map[string]any{
		"urls": []string{server.URL},
		"headers": map[string]string{
			"Authorization":   "Bearer token123",
			"X-Custom-Header": "custom-value",
		},
	}
	argsJSON, _ := json.Marshal(args)

	toolCall := tools.ToolCall{
		Function: tools.FunctionCall{
			Arguments: string(argsJSON),
		},
	}

	handler := &fetchHandler{tool: tool}
	result, err := handler.CallTool(ctx, toolCall)
	if err != nil {
		t.Fatalf("CallTool() error: %v", err)
	}

	if !containsAllSubstrings(result.Output, []string{"Status: 200", "Authorized!"}) {
		t.Errorf("Headers not properly sent: %s", result.Output)
	}
}

func TestFetchHandler_CallTool_InvalidURL(t *testing.T) {
	tool := NewFetchTool()
	ctx := context.TODO()

	args := map[string]any{
		"urls": []string{"not-a-url"},
	}
	argsJSON, _ := json.Marshal(args)

	toolCall := tools.ToolCall{
		Function: tools.FunctionCall{
			Arguments: string(argsJSON),
		},
	}

	handler := &fetchHandler{tool: tool}
	result, err := handler.CallTool(ctx, toolCall)
	if err != nil {
		t.Fatalf("CallTool() error: %v", err)
	}

	if !containsAllSubstrings(result.Output, []string{"Error fetching", "invalid URL: missing scheme or host"}) {
		t.Errorf("Expected URL validation error: %s", result.Output)
	}
}

func TestFetchHandler_CallTool_UnsupportedProtocol(t *testing.T) {
	tool := NewFetchTool()
	ctx := context.TODO()

	args := map[string]any{
		"urls": []string{"ftp://example.com"},
	}
	argsJSON, _ := json.Marshal(args)

	toolCall := tools.ToolCall{
		Function: tools.FunctionCall{
			Arguments: string(argsJSON),
		},
	}

	handler := &fetchHandler{tool: tool}
	result, err := handler.CallTool(ctx, toolCall)
	if err != nil {
		t.Fatalf("CallTool() error: %v", err)
	}

	if !containsAllSubstrings(result.Output, []string{"Error fetching", "only HTTP and HTTPS URLs are supported"}) {
		t.Errorf("Expected protocol validation error: %s", result.Output)
	}
}

func TestFetchHandler_CallTool_NoURLs(t *testing.T) {
	tool := NewFetchTool()
	ctx := context.TODO()

	args := map[string]any{
		"urls": []string{},
	}
	argsJSON, _ := json.Marshal(args)

	toolCall := tools.ToolCall{
		Function: tools.FunctionCall{
			Arguments: string(argsJSON),
		},
	}

	handler := &fetchHandler{tool: tool}
	_, err := handler.CallTool(ctx, toolCall)
	if err == nil {
		t.Fatal("Expected error for empty URLs")
	}

	if err.Error() != "at least one URL is required" {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestFetchHandler_CallTool_InvalidJSON(t *testing.T) {
	tool := NewFetchTool()
	ctx := context.TODO()

	toolCall := tools.ToolCall{
		Function: tools.FunctionCall{
			Arguments: "invalid json",
		},
	}

	handler := &fetchHandler{tool: tool}
	_, err := handler.CallTool(ctx, toolCall)
	if err == nil {
		t.Fatal("Expected error for invalid JSON")
	}
}

func TestFetchHandler_CallTool_CustomMethod(t *testing.T) {
	// Create test server that checks method
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		fmt.Fprint(w, "POST received")
	}))
	defer server.Close()

	tool := NewFetchTool()
	ctx := context.TODO()

	args := map[string]any{
		"urls":   []string{server.URL},
		"method": "POST",
	}
	argsJSON, _ := json.Marshal(args)

	toolCall := tools.ToolCall{
		Function: tools.FunctionCall{
			Arguments: string(argsJSON),
		},
	}

	handler := &fetchHandler{tool: tool}
	result, err := handler.CallTool(ctx, toolCall)
	if err != nil {
		t.Fatalf("CallTool() error: %v", err)
	}

	if !containsAllSubstrings(result.Output, []string{"Status: 200", "POST received"}) {
		t.Errorf("POST method not working: %s", result.Output)
	}
}

func TestFetchHandler_CallTool_CustomUserAgent(t *testing.T) {
	customUA := "MyBot/1.0"

	// Create test server that checks User-Agent
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != customUA {
			http.Error(w, "Wrong User-Agent", http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, "User-Agent OK")
	}))
	defer server.Close()

	tool := NewFetchTool()
	ctx := context.TODO()

	args := map[string]any{
		"urls":      []string{server.URL},
		"userAgent": customUA,
	}
	argsJSON, _ := json.Marshal(args)

	toolCall := tools.ToolCall{
		Function: tools.FunctionCall{
			Arguments: string(argsJSON),
		},
	}

	handler := &fetchHandler{tool: tool}
	result, err := handler.CallTool(ctx, toolCall)
	if err != nil {
		t.Fatalf("CallTool() error: %v", err)
	}

	if !containsAllSubstrings(result.Output, []string{"Status: 200", "User-Agent OK"}) {
		t.Errorf("Custom User-Agent not working: %s", result.Output)
	}
}

// Helper function to check if a string contains all required substrings
func containsAllSubstrings(text string, substrings []string) bool {
	for _, substr := range substrings {
		if !containsSubstring(text, substr) {
			return false
		}
	}
	return true
}

func containsSubstring(text, substr string) bool {
	return len(text) >= len(substr) && findSubstring(text, substr)
}

func findSubstring(text, substr string) bool {
	for i := 0; i <= len(text)-len(substr); i++ {
		if text[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
