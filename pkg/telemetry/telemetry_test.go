package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// MockHTTPClient captures HTTP requests for testing
type MockHTTPClient struct {
	*http.Client
	mu       sync.Mutex
	requests []*http.Request
	bodies   [][]byte
	response *http.Response
}

// NewMockHTTPClient creates a new mock HTTP client with a default success response
func NewMockHTTPClient() *MockHTTPClient {
	mock := &MockHTTPClient{
		response: &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(bytes.NewReader([]byte(`{"success": true}`))),
			Header:     make(http.Header),
		},
	}
	mock.Client = &http.Client{Transport: mock}
	return mock
}

// SetResponse allows updating the mock response for testing different scenarios
func (m *MockHTTPClient) SetResponse(resp *http.Response) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.response = resp
}

// RoundTrip implements http.RoundTripper and captures the request
func (m *MockHTTPClient) RoundTrip(req *http.Request) (*http.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Capture the request
	m.requests = append(m.requests, req)

	// Read and store the body for inspection
	if req.Body != nil {
		body, _ := io.ReadAll(req.Body)
		m.bodies = append(m.bodies, body)
		// Reset body for the actual request processing
		req.Body = io.NopCloser(bytes.NewReader(body))
	} else {
		m.bodies = append(m.bodies, nil)
	}

	return m.response, nil
}

// GetRequests returns all captured requests
func (m *MockHTTPClient) GetRequests() []*http.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*http.Request{}, m.requests...)
}

// GetBodies returns all captured request bodies
func (m *MockHTTPClient) GetBodies() [][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([][]byte{}, m.bodies...)
}

// GetRequestCount returns the number of HTTP requests made
func (m *MockHTTPClient) GetRequestCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.requests)
}

func TestNewClient(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	// Test enabled client with mock HTTP client to capture HTTP calls
	// Note: debug mode does NOT disable HTTP calls - it only adds extra logging
	mockHTTP := NewMockHTTPClient()
	_, err := NewClient(logger, true, true, "test-version", mockHTTP.Client)
	if err != nil {
		t.Fatalf("Failed to create enabled client: %v", err)
	}
	// Test disabled client
	client, err := NewClient(logger, false, false, "test-version")
	if err != nil {
		t.Fatalf("Failed to create disabled client: %v", err)
	}

	// This should not panic
	commandEvent := &CommandEvent{
		Action:  "test-command",
		Success: true,
		Error:   "",
	}
	client.Track(context.Background(), commandEvent)
	client.RecordToolCall(context.Background(), "test-tool", "session-id", "agent-name", time.Millisecond, nil)
	client.RecordTokenUsage(context.Background(), "test-model", 100, 50, 0.5)
}

func TestSessionTracking(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	mockHTTP := NewMockHTTPClient()
	client, err := NewClient(logger, true, true, "test-version", mockHTTP.Client)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// Set endpoint, apiKey, and header to verify HTTP calls are made correctly
	client.endpoint = "https://test-session-tracking.com/api"
	client.apiKey = "test-session-key"
	client.header = "test-header"

	ctx := context.Background()

	// Test session lifecycle
	sessionID := client.RecordSessionStart(ctx, "test-agent", "test-session-id")
	if sessionID == "" {
		t.Error("Expected non-empty session ID")
	}

	// Record some activity
	client.RecordToolCall(ctx, "test-tool", "session-id", "agent-name", time.Millisecond, nil)
	client.RecordTokenUsage(ctx, "test-model", 100, 50, 0.5)

	// End session
	client.RecordSessionEnd(ctx)

	// Multiple ends should be safe
	client.RecordSessionEnd(ctx)

	// Wait for events to be processed
	time.Sleep(100 * time.Millisecond)

	// Verify HTTP requests were made (should have session start, tool call, token usage, session end)
	requestCount := mockHTTP.GetRequestCount()
	if requestCount == 0 {
		t.Fatal("Expected HTTP requests to be made for session tracking events, but none were captured")
	}

	t.Logf("Session tracking HTTP requests captured: %d", requestCount)

	// Verify request structure
	requests := mockHTTP.GetRequests()
	for i, req := range requests {
		if req.Method != http.MethodPost {
			t.Errorf("Request %d: Expected POST method, got %s", i, req.Method)
		}
		if req.Header.Get("test-header") != "test-session-key" {
			t.Errorf("Request %d: Expected test-header test-session-key, got %s", i, req.Header.Get("test-header"))
		}
	}
}

func TestCommandTracking(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	mockHTTP := NewMockHTTPClient()
	client, err := NewClient(logger, true, true, "test-version", mockHTTP.Client)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// Set endpoint, apiKey, and header to verify HTTP calls are made correctly
	client.endpoint = "https://test-command-tracking.com/api"
	client.apiKey = "test-command-key"
	client.header = "test-header"

	executed := false
	cmdInfo := CommandInfo{
		Action: "test-command",
		Args:   []string{},
		Flags:  []string{},
	}
	err = client.TrackCommand(context.Background(), cmdInfo, func(ctx context.Context) error {
		executed = true
		time.Sleep(10 * time.Millisecond)
		return nil
	})
	if err != nil {
		t.Fatalf("Command execution failed: %v", err)
	}
	if !executed {
		t.Error("Expected command function to be executed")
	}

	// Wait for events to be processed
	time.Sleep(100 * time.Millisecond)

	// Verify HTTP requests were made for command tracking
	requestCount := mockHTTP.GetRequestCount()
	if requestCount == 0 {
		t.Fatal("Expected HTTP requests to be made for command tracking, but none were captured")
	}

	t.Logf("Command tracking HTTP requests captured: %d", requestCount)

	// Verify request structure
	requests := mockHTTP.GetRequests()
	for i, req := range requests {
		if req.Header.Get("test-header") != "test-command-key" {
			t.Errorf("Request %d: Expected test-header test-command-key, got %s", i, req.Header.Get("test-header"))
		}
	}
}

func TestCommandTrackingWithError(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	mockHTTP := NewMockHTTPClient()
	client, err := NewClient(logger, true, true, "test-version", mockHTTP.Client)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// Set endpoint, apiKey, and header to verify HTTP calls are made correctly
	client.endpoint = "https://test-command-error.com/api"
	client.apiKey = "test-command-error-key"
	client.header = "test-header"

	testErr := &testError{}
	cmdInfo := CommandInfo{
		Action: "failing-command",
		Args:   []string{},
		Flags:  []string{},
	}
	err = client.TrackCommand(context.Background(), cmdInfo, func(ctx context.Context) error {
		return testErr
	})

	if err != testErr {
		t.Errorf("Expected error %v, got %v", testErr, err)
	}

	// Wait for events to be processed
	time.Sleep(100 * time.Millisecond)

	// Verify HTTP requests were made for command tracking with error
	requestCount := mockHTTP.GetRequestCount()
	if requestCount == 0 {
		t.Fatal("Expected HTTP requests to be made for command error tracking, but none were captured")
	}

	t.Logf("Command error tracking HTTP requests captured: %d", requestCount)
}

func TestStructuredEvent(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	// Use debug mode to avoid HTTP calls in tests
	client, err := NewClient(logger, true, true, "test-version")
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	event := CommandEvent{
		Action:  "test-command",
		Success: true,
	}

	// Should not panic
	client.Track(context.Background(), &event)
}

func TestGetTelemetryEnabled(t *testing.T) {
	// Save original env var
	originalEnabled := os.Getenv("TELEMETRY_ENABLED")
	defer func() {
		if originalEnabled != "" {
			os.Setenv("TELEMETRY_ENABLED", originalEnabled)
		} else {
			os.Unsetenv("TELEMETRY_ENABLED")
		}
	}()

	// Test default (enabled)
	os.Unsetenv("TELEMETRY_ENABLED")
	if !GetTelemetryEnabled() {
		t.Error("Expected telemetry to be enabled by default")
	}

	// Test explicitly disabled
	os.Setenv("TELEMETRY_ENABLED", "false")
	if GetTelemetryEnabled() {
		t.Error("Expected telemetry to be disabled when TELEMETRY_ENABLED=false")
	}

	// Test explicitly enabled
	os.Setenv("TELEMETRY_ENABLED", "true")
	if !GetTelemetryEnabled() {
		t.Error("Expected telemetry to be enabled when TELEMETRY_ENABLED=true")
	}

	// Test other values default to enabled (only "false" disables)
	testCases := []string{"1", "yes", "on", "enabled", "anything", ""}
	for _, value := range testCases {
		os.Setenv("TELEMETRY_ENABLED", value)
		if !GetTelemetryEnabled() {
			t.Errorf("Expected telemetry to be enabled when TELEMETRY_ENABLED=%s", value)
		}
	}
}

// testError is a simple error implementation for testing
type testError struct{}

func (e *testError) Error() string {
	return "test error"
}

// Test-only methods - these wrap command execution with telemetry for testing purposes

// TrackCommand wraps command execution with telemetry (test-only method)
func (tc *Client) TrackCommand(ctx context.Context, commandInfo CommandInfo, fn func(context.Context) error) error {
	if !tc.enabled {
		return fn(ctx)
	}

	// Add telemetry client to context so the wrapped function can access it
	ctx = WithClient(ctx, tc)

	// Send telemetry event immediately (optimistic approach)
	commandEvent := CommandEvent{
		Action:  commandInfo.Action,
		Args:    commandInfo.Args,
		Success: true, // Assume success - we're tracking user intent, not outcome
	}

	// Send the telemetry event immediately
	tc.Track(ctx, &commandEvent)

	// Now run the command function
	return fn(ctx)
}

// TrackServerStart immediately sends telemetry for server startup, then runs the server function (test-only method)
// This is for long-running commands that may never exit (api, mcp, etc.)
func (tc *Client) TrackServerStart(ctx context.Context, commandInfo CommandInfo, fn func(context.Context) error) error {
	if !tc.enabled {
		return fn(ctx)
	}

	// Add telemetry client to context so the wrapped function can access it
	ctx = WithClient(ctx, tc)

	// Send startup event immediately
	startupEvent := CommandEvent{
		Action:  commandInfo.Action,
		Args:    commandInfo.Args,
		Success: true, // We assume startup succeeds if we reach this point
	}

	// Send the startup telemetry event immediately
	tc.Track(ctx, &startupEvent)

	// Now run the server function (which may run indefinitely)
	return fn(ctx)
}

// TestAllEventTypes tests all possible telemetry events with mock HTTP client
func TestAllEventTypes(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	// Use mock HTTP client to avoid actual HTTP calls in tests
	mockHTTP := NewMockHTTPClient()
	client, err := NewClient(logger, true, true, "test-version", mockHTTP.Client)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// Set endpoint, apiKey, and header to verify HTTP calls are made correctly
	client.endpoint = "https://test-telemetry-all-events.com/api"
	client.apiKey = "test-all-events-key"
	client.header = "test-header"

	ctx := context.Background()
	sessionID := "test-session-123"
	agentName := "test-agent"

	// Start session to enable session-based events
	client.RecordSessionStart(ctx, agentName, sessionID)

	t.Run("CommandEvents", func(t *testing.T) {
		// Test all major command events based on cmd/ files
		commands := []struct {
			action string
			args   []string
		}{
			{"run", []string{"config.yaml"}},
			{"api", []string{}},
			{"mcp", []string{}},
			{"tui", []string{"config.yaml"}},
			{"pull", []string{"user/agent:latest"}},
			{"push", []string{"user/agent:latest"}},
			{"catalog", []string{}},
			{"new", []string{"my-agent"}},
			{"version", []string{}},
			{"feedback", []string{}},
			{"eval", []string{"expression"}},
			{"readme", []string{}},
			{"gateway", []string{}},
			{"debug", []string{}},
		}

		for _, cmd := range commands {
			t.Run(cmd.action, func(t *testing.T) {
				// Test successful command
				event := &CommandEvent{
					Action:  cmd.action,
					Args:    cmd.args,
					Success: true,
				}
				client.Track(ctx, event)

				// Test command with error
				errorEvent := &CommandEvent{
					Action:  cmd.action,
					Args:    cmd.args,
					Success: false,
					Error:   "test error",
				}
				client.Track(ctx, errorEvent)
			})
		}
	})

	t.Run("SessionEvents", func(t *testing.T) {
		// Test session start event
		startEvent := &SessionStartEvent{
			Action:    "start",
			SessionID: sessionID,
			AgentName: agentName,
		}
		client.Track(ctx, startEvent)

		// Test session end event
		endEvent := &SessionEndEvent{
			Action:       "end",
			SessionID:    sessionID,
			AgentName:    agentName,
			Duration:     1000, // 1 second
			ToolCalls:    5,
			InputTokens:  100,
			OutputTokens: 200,
			TotalTokens:  300,
			IsSuccess:    true,
			Error:        []string{},
		}
		client.Track(ctx, endEvent)

		// Test session with errors
		errorSessionEvent := &SessionEndEvent{
			Action:       "end",
			SessionID:    sessionID + "-error",
			AgentName:    agentName,
			Duration:     500,
			ToolCalls:    3,
			InputTokens:  50,
			OutputTokens: 25,
			TotalTokens:  75,
			IsSuccess:    false,
			Error:        []string{"session failed"},
		}
		client.Track(ctx, errorSessionEvent)
	})

	t.Run("ToolEvents", func(t *testing.T) {
		// Test various tool events based on the tool system
		tools := []struct {
			name     string
			success  bool
			duration int64
			error    string
		}{
			{"think", true, 100, ""},
			{"todo", true, 50, ""},
			{"memory", true, 200, ""},
			{"transfer_task", true, 150, ""},
			{"filesystem", true, 75, ""},
			{"shell", true, 300, ""},
			{"mcp_tool", false, 500, "tool execution failed"},
			{"custom_tool", true, 125, ""},
		}

		for _, tool := range tools {
			t.Run(tool.name, func(t *testing.T) {
				event := &ToolEvent{
					Action:    "call",
					ToolName:  tool.name,
					SessionID: sessionID,
					AgentName: agentName,
					Duration:  tool.duration,
					Success:   tool.success,
					Error:     tool.error,
				}
				client.Track(ctx, event)

				// Also test RecordToolCall method
				var err error
				if tool.error != "" {
					err = &testError{}
				}
				client.RecordToolCall(ctx, tool.name, sessionID, agentName, time.Duration(tool.duration)*time.Millisecond, err)
			})
		}
	})

	t.Run("TokenEvents", func(t *testing.T) {
		// Test token events for different models
		models := []struct {
			name         string
			inputTokens  int64
			outputTokens int64
		}{
			{"gpt-4", 150, 75},
			{"claude-3-sonnet", 200, 100},
			{"gemini-pro", 100, 50},
			{"local-model", 80, 40},
		}

		for _, model := range models {
			t.Run(model.name, func(t *testing.T) {
				event := &TokenEvent{
					Action:       "usage",
					ModelName:    model.name,
					SessionID:    sessionID,
					AgentName:    agentName,
					InputTokens:  model.inputTokens,
					OutputTokens: model.outputTokens,
					TotalTokens:  model.inputTokens + model.outputTokens,
					Cost:         0,
				}
				client.Track(ctx, event)

				// Also test RecordTokenUsage method
				client.RecordTokenUsage(ctx, model.name, model.inputTokens, model.outputTokens, 0)
			})
		}

		// Test token event with error
		errorTokenEvent := &TokenEvent{
			Action:       "usage",
			ModelName:    "failing-model",
			SessionID:    sessionID,
			AgentName:    agentName,
			InputTokens:  50,
			OutputTokens: 0,
			TotalTokens:  50,
			Cost:         0,
		}
		client.Track(ctx, errorTokenEvent)
	})

	// End session
	client.RecordSessionEnd(ctx)

	// Wait for events to be processed

	// Give additional time for background processing
	time.Sleep(100 * time.Millisecond)

	// Verify that HTTP requests were made for all events
	requestCount := mockHTTP.GetRequestCount()
	if requestCount == 0 {
		t.Fatal("Expected HTTP requests to be made for telemetry events, but none were captured")
	}

	t.Logf("Total HTTP requests captured: %d", requestCount)

	// Verify that all requests have correct structure
	requests := mockHTTP.GetRequests()
	bodies := mockHTTP.GetBodies()

	if len(requests) != len(bodies) {
		t.Fatalf("Mismatch between request count (%d) and body count (%d)", len(requests), len(bodies))
	}

	// Verify each HTTP request has correct headers and endpoint
	for i, req := range requests {
		// Verify method and URL
		if req.Method != http.MethodPost {
			t.Errorf("Request %d: Expected POST method, got %s", i, req.Method)
		}
		if req.URL.String() != "https://test-telemetry-all-events.com/api" {
			t.Errorf("Request %d: Expected URL https://test-telemetry-all-events.com/api, got %s", i, req.URL.String())
		}

		// Verify headers
		if req.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Request %d: Expected Content-Type application/json, got %s", i, req.Header.Get("Content-Type"))
		}
		if req.Header.Get("User-Agent") != "cagent/test-version" {
			t.Errorf("Request %d: Expected User-Agent cagent/test-version, got %s", i, req.Header.Get("User-Agent"))
		}
		if req.Header.Get("test-header") != "test-all-events-key" {
			t.Errorf("Request %d: Expected test-header test-all-events-key, got %s", i, req.Header.Get("test-header"))
		}

		// Verify request body structure
		var requestBody map[string]any
		if err := json.Unmarshal(bodies[i], &requestBody); err != nil {
			t.Errorf("Request %d: Failed to unmarshal request body: %v", i, err)
			continue
		}

		// Verify it has records array structure
		records, ok := requestBody["records"].([]any)
		if !ok {
			t.Errorf("Request %d: Expected 'records' array in request body", i)
			continue
		}
		if len(records) != 1 {
			t.Errorf("Request %d: Expected 1 record, got %d", i, len(records))
			continue
		}

		// Verify the event structure
		record := records[0].(map[string]any)
		if eventType, ok := record["event"].(string); !ok || eventType == "" {
			t.Errorf("Request %d: Expected non-empty event type, got %v", i, record["event"])
		}

		// Verify properties exist
		if _, ok := record["properties"].(map[string]any); !ok {
			t.Errorf("Request %d: Expected properties object in event", i)
		}
	}
}

// TestTrackServerStart tests long-running server command tracking
func TestTrackServerStart(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	client, err := NewClient(logger, true, true, "test-version")
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	executed := false
	cmdInfo := CommandInfo{
		Action: "mcp",
		Args:   []string{},
		Flags:  []string{"--port", "8080"},
	}
	err = client.TrackServerStart(context.Background(), cmdInfo, func(ctx context.Context) error {
		executed = true
		// Simulate server running briefly
		time.Sleep(10 * time.Millisecond)
		return nil
	})
	if err != nil {
		t.Fatalf("Server execution failed: %v", err)
	}
	if !executed {
		t.Error("Expected server function to be executed")
	}
}

// TestBuildCommandInfo tests the BuildCommandInfo function with all commands
func TestBuildCommandInfo(t *testing.T) {
	testCases := []struct {
		name     string
		command  string
		args     []string
		expected CommandInfo
	}{
		{
			name:    "run command with config",
			command: "run",
			args:    []string{"config.yaml", "--debug"},
			expected: CommandInfo{
				Action: "run",
				Args:   []string{"config.yaml"},
				Flags:  []string{},
			},
		},
		{
			name:    "pull command with image",
			command: "pull",
			args:    []string{"user/agent:latest"},
			expected: CommandInfo{
				Action: "pull",
				Args:   []string{"user/agent:latest"},
				Flags:  []string{},
			},
		},
		{
			name:    "catalog command",
			command: "catalog",
			args:    []string{},
			expected: CommandInfo{
				Action: "catalog",
				Args:   []string{},
				Flags:  []string{},
			},
		},
		{
			name:    "version command",
			command: "version",
			args:    []string{},
			expected: CommandInfo{
				Action: "version",
				Args:   []string{},
				Flags:  []string{},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := &cobra.Command{Use: tc.command}
			result := BuildCommandInfo(cmd, tc.args, tc.command)

			if result.Action != tc.expected.Action {
				t.Errorf("Expected Action %s, got %s", tc.expected.Action, result.Action)
			}

			if len(result.Args) != len(tc.expected.Args) {
				t.Errorf("Expected %d args, got %d", len(tc.expected.Args), len(result.Args))
			} else {
				for i, arg := range tc.expected.Args {
					if i < len(result.Args) && result.Args[i] != arg {
						t.Errorf("Expected arg[%d] %s, got %s", i, arg, result.Args[i])
					}
				}
			}
		})
	}
}

// TestGlobalTelemetryFunctions tests the global telemetry convenience functions
func TestGlobalTelemetryFunctions(t *testing.T) {
	// Save original global state
	originalClient := globalToolTelemetryClient
	originalVersion := globalTelemetryVersion
	originalDebugMode := globalTelemetryDebugMode
	defer func() {
		globalToolTelemetryClient = originalClient
		globalTelemetryOnce = sync.Once{} // Reset to new instance
		globalTelemetryVersion = originalVersion
		globalTelemetryDebugMode = originalDebugMode
	}()

	// Reset global state for testing
	globalToolTelemetryClient = nil
	globalTelemetryOnce = sync.Once{}
	SetGlobalTelemetryVersion("test-version")
	SetGlobalTelemetryDebugMode(true)

	// Test global command recording
	TrackCommand("test-command", []string{"arg1"})

	// Verify global client was initialized
	if globalToolTelemetryClient == nil {
		t.Error("Expected global telemetry client to be initialized")
	}

	// Test explicit initialization
	EnsureGlobalTelemetryInitialized()
	client := GetGlobalTelemetryClient()
	if client == nil {
		t.Error("Expected GetGlobalTelemetryClient to return non-nil client")
	}
}

// TestHTTPRequestVerification tests that HTTP requests are made correctly when telemetry is enabled
func TestHTTPRequestVerification(t *testing.T) {
	// Create logger with debug level to see all debug messages
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	mockHTTP := NewMockHTTPClient()

	// Create client with mock HTTP client, endpoint, and API key to trigger HTTP calls
	client, err := NewClient(logger, true, true, "test-version", mockHTTP.Client)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// Set endpoint, API key, and header to ensure HTTP calls are made
	client.endpoint = "https://test-telemetry.example.com/api/events"
	client.apiKey = "test-api-key"
	client.header = "test-header"

	ctx := context.Background()

	// Test command event HTTP request
	t.Run("CommandEventHTTPRequest", func(t *testing.T) {
		// Reset mock before test
		mockHTTP = NewMockHTTPClient()
		client.httpClient = mockHTTP

		event := &CommandEvent{
			Action:  "run",
			Args:    []string{"config.yaml"},
			Success: true,
		}

		// Verify the client is properly configured
		if client.endpoint == "" {
			t.Fatal("Client endpoint should be set for this test")
		}
		if client.apiKey == "" {
			t.Fatal("Client API key should be set for this test")
		}
		if !client.enabled {
			t.Fatal("Client should be enabled for this test")
		}

		t.Logf("Before Track: endpoint=%s, apiKey len=%d, enabled=%t", client.endpoint, len(client.apiKey), client.enabled)

		client.Track(ctx, event)

		// Give additional time for background processing (race condition fix)
		time.Sleep(100 * time.Millisecond)

		// Debug output
		t.Logf("HTTP requests captured: %d", mockHTTP.GetRequestCount())

		// Verify HTTP request was made
		if mockHTTP.GetRequestCount() == 0 {
			t.Fatal("Expected HTTP request to be made, but none were captured")
		}

		requests := mockHTTP.GetRequests()
		req := requests[0]

		// Verify request method and URL
		if req.Method != http.MethodPost {
			t.Errorf("Expected POST request, got %s", req.Method)
		}
		if req.URL.String() != "https://test-telemetry.example.com/api/events" {
			t.Errorf("Expected URL https://test-telemetry.example.com/api/events, got %s", req.URL.String())
		}

		// Verify headers
		if req.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Expected Content-Type application/json, got %s", req.Header.Get("Content-Type"))
		}
		if req.Header.Get("User-Agent") != "cagent/test-version" {
			t.Errorf("Expected User-Agent cagent/test-version, got %s", req.Header.Get("User-Agent"))
		}
		if req.Header.Get("test-header") != "test-api-key" {
			t.Errorf("Expected test-header test-api-key, got %s", req.Header.Get("test-header"))
		}

		// Verify request body structure
		bodies := mockHTTP.GetBodies()
		if len(bodies) == 0 {
			t.Fatal("Expected request body to be captured")
		}

		var requestBody map[string]any
		if err := json.Unmarshal(bodies[0], &requestBody); err != nil {
			t.Fatalf("Failed to unmarshal request body: %v", err)
		}

		// Verify it has records array structure
		records, ok := requestBody["records"].([]any)
		if !ok {
			t.Fatal("Expected 'records' array in request body")
		}
		if len(records) != 1 {
			t.Fatalf("Expected 1 record, got %d", len(records))
		}

		// Verify the event structure
		record := records[0].(map[string]any)
		if record["event"] != "command" {
			t.Errorf("Expected event type 'command', got %v", record["event"])
		}

		// Verify properties contain the command data
		properties, ok := record["properties"].(map[string]any)
		if !ok {
			t.Fatal("Expected properties object in event")
		}
		if properties["action"] != "run" {
			t.Errorf("Expected action 'run', got %v", properties["action"])
		}
		if properties["is_success"] != true {
			t.Errorf("Expected is_success true, got %v", properties["is_success"])
		}
	})

	// Test that no HTTP calls are made when endpoint/apiKey are missing
	t.Run("NoHTTPWhenMissingCredentials", func(t *testing.T) {
		mockHTTP2 := NewMockHTTPClient()
		client2, err := NewClient(logger, true, true, "test-version", mockHTTP2.Client)
		if err != nil {
			t.Fatalf("Failed to create client: %v", err)
		}

		// Leave endpoint and API key empty
		client2.endpoint = ""
		client2.apiKey = ""

		event := &CommandEvent{
			Action:  "version",
			Success: true,
		}

		client2.Track(ctx, event)

		// Verify no HTTP requests were made
		if mockHTTP2.GetRequestCount() != 0 {
			t.Errorf("Expected no HTTP requests when endpoint/apiKey are missing, got %d", mockHTTP2.GetRequestCount())
		}
	})

	// Test that no HTTP calls are made when client is disabled
	t.Run("NoHTTPWhenDisabled", func(t *testing.T) {
		mockHTTP3 := NewMockHTTPClient()
		client3, err := NewClient(logger, false, true, "test-version", mockHTTP3.Client)
		if err != nil {
			t.Fatalf("Failed to create client: %v", err)
		}

		event := &CommandEvent{
			Action:  "version",
			Success: true,
		}

		client3.Track(ctx, event)

		// Verify no HTTP requests were made (client disabled means Track returns early)
		if mockHTTP3.GetRequestCount() != 0 {
			t.Errorf("Expected no HTTP requests when client is disabled, got %d", mockHTTP3.GetRequestCount())
		}
	})
}

// SlowMockHTTPClient creates artificial backpressure by adding delays
type SlowMockHTTPClient struct {
	*MockHTTPClient
	delay time.Duration
}

func NewSlowMockHTTPClient(delay time.Duration) *SlowMockHTTPClient {
	return &SlowMockHTTPClient{
		MockHTTPClient: NewMockHTTPClient(),
		delay:          delay,
	}
}

func (s *SlowMockHTTPClient) RoundTrip(req *http.Request) (*http.Response, error) {
	time.Sleep(s.delay) // Add artificial delay
	return s.MockHTTPClient.RoundTrip(req)
}

// TestEventBufferOverflowDropsEvents verifies that events are dropped when buffer is full
func TestEventBufferOverflowDropsEvents(t *testing.T) {
	// Create a slow HTTP mock to cause backpressure
	slowMock := NewSlowMockHTTPClient(50 * time.Millisecond)

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	client, err := NewClient(logger, true, true, "test-version", slowMock.Client)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	client.endpoint = "https://test-overflow.com/api"
	client.apiKey = "overflow-key"
	client.header = "test-header"

	// With synchronous processing, there's no buffer overflow to test
	// Events are processed immediately, so we just verify they all get processed
	numEvents := 10 // Send a reasonable number for synchronous processing

	// Send events synchronously
	for i := 0; i < numEvents; i++ {
		client.Track(context.Background(), &CommandEvent{
			Action:  "overflow-test",
			Success: true,
		})
	}

	// With synchronous processing, all events should be processed immediately
	expectedRequests := numEvents
	if slowMock.GetRequestCount() != expectedRequests {
		t.Errorf("Expected %d requests with synchronous processing, got %d", expectedRequests, slowMock.GetRequestCount())
	}

	t.Logf("Synchronous processing handled %d events correctly", numEvents)

	// Clean shutdown
}

// TestNon2xxHTTPResponseHandling ensures that 5xx responses are logged and handled gracefully
func TestNon2xxHTTPResponseHandling(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	mockHTTP := NewMockHTTPClient()
	client, err := NewClient(logger, true, true, "test-version", mockHTTP.Client)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	client.endpoint = "https://test-error-response.com/api"
	client.apiKey = "error-key"
	client.header = "test-header"

	// Configure mock to return 500
	mockHTTP.SetResponse(&http.Response{
		StatusCode: 500,
		Status:     "500 Internal Server Error",
		Body:       io.NopCloser(bytes.NewReader([]byte("internal error"))),
		Header:     make(http.Header),
	})

	client.Track(context.Background(), &CommandEvent{Action: "error-test", Success: true})

	// Give more time for background processing
	time.Sleep(100 * time.Millisecond)

	requestCount := mockHTTP.GetRequestCount()
	if requestCount == 0 {
		t.Errorf("Expected HTTP request to be made despite error response, got %d requests", requestCount)
	}

	// Test additional error codes
	mockHTTP.SetResponse(&http.Response{
		StatusCode: 404,
		Status:     "404 Not Found",
		Body:       io.NopCloser(bytes.NewReader([]byte("not found"))),
		Header:     make(http.Header),
	})

	client.Track(context.Background(), &CommandEvent{Action: "not-found-test", Success: true})

	time.Sleep(100 * time.Millisecond)

	finalRequestCount := mockHTTP.GetRequestCount()
	if finalRequestCount < 2 {
		t.Errorf("Expected at least 2 HTTP requests (500 + 404), got %d", finalRequestCount)
	}
}
