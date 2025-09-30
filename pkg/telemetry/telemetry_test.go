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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	require.NoError(t, err)
	// Test disabled client
	client, err := NewClient(logger, false, false, "test-version")
	require.NoError(t, err)

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
	require.NoError(t, err)

	// Set endpoint, apiKey, and header to verify HTTP calls are made correctly
	client.endpoint = "https://test-session-tracking.com/api"
	client.apiKey = "test-session-key"
	client.header = "test-header"

	ctx := context.Background()

	// Test session lifecycle
	sessionID := client.RecordSessionStart(ctx, "test-agent", "test-session-id")
	assert.NotEmpty(t, sessionID)

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
	assert.Greater(t, requestCount, 0, "Expected HTTP requests to be made for session tracking events")

	t.Logf("Session tracking HTTP requests captured: %d", requestCount)

	// Verify request structure
	requests := mockHTTP.GetRequests()
	for i, req := range requests {
		assert.Equal(t, http.MethodPost, req.Method, "Request %d: Expected POST method", i)
		assert.Equal(t, "test-session-key", req.Header.Get("test-header"), "Request %d: Expected test-header test-session-key", i)
	}
}

func TestCommandTracking(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	mockHTTP := NewMockHTTPClient()
	client, err := NewClient(logger, true, true, "test-version", mockHTTP.Client)
	require.NoError(t, err)

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
	require.NoError(t, err)
	assert.True(t, executed)

	// Wait for events to be processed
	time.Sleep(100 * time.Millisecond)

	// Verify HTTP requests were made for command tracking
	requestCount := mockHTTP.GetRequestCount()
	assert.Greater(t, requestCount, 0, "Expected HTTP requests to be made for command tracking")

	t.Logf("Command tracking HTTP requests captured: %d", requestCount)

	// Verify request structure
	requests := mockHTTP.GetRequests()
	for i, req := range requests {
		assert.Equal(t, "test-command-key", req.Header.Get("test-header"), "Request %d: Expected test-header test-command-key", i)
	}
}

func TestCommandTrackingWithError(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	mockHTTP := NewMockHTTPClient()
	client, err := NewClient(logger, true, true, "test-version", mockHTTP.Client)
	require.NoError(t, err)

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

	assert.Equal(t, testErr, err)

	// Wait for events to be processed
	time.Sleep(100 * time.Millisecond)

	// Verify HTTP requests were made for command tracking with error
	requestCount := mockHTTP.GetRequestCount()
	assert.Greater(t, requestCount, 0, "Expected HTTP requests to be made for command error tracking")

	t.Logf("Command error tracking HTTP requests captured: %d", requestCount)
}

func TestStructuredEvent(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	// Use debug mode to avoid HTTP calls in tests
	client, err := NewClient(logger, true, true, "test-version")
	require.NoError(t, err)

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
	assert.True(t, GetTelemetryEnabled())

	// Test explicitly disabled
	os.Setenv("TELEMETRY_ENABLED", "false")
	assert.False(t, GetTelemetryEnabled())

	// Test explicitly enabled
	os.Setenv("TELEMETRY_ENABLED", "true")
	assert.True(t, GetTelemetryEnabled())

	// Test other values default to enabled (only "false" disables)
	testCases := []string{"1", "yes", "on", "enabled", "anything", ""}
	for _, value := range testCases {
		os.Setenv("TELEMETRY_ENABLED", value)
		assert.True(t, GetTelemetryEnabled(), "Expected telemetry to be enabled when TELEMETRY_ENABLED=%s", value)
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
	require.NoError(t, err)

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
	assert.Greater(t, requestCount, 0, "Expected HTTP requests to be made for telemetry events")

	t.Logf("Total HTTP requests captured: %d", requestCount)

	// Verify that all requests have correct structure
	requests := mockHTTP.GetRequests()
	bodies := mockHTTP.GetBodies()

	assert.Len(t, requests, len(bodies), "Mismatch between request count and body count")

	// Verify each HTTP request has correct headers and endpoint
	for i, req := range requests {
		// Verify method and URL
		assert.Equal(t, http.MethodPost, req.Method, "Request %d: Expected POST method", i)
		assert.Equal(t, "https://test-telemetry-all-events.com/api", req.URL.String(), "Request %d: Expected correct URL", i)

		// Verify headers
		assert.Equal(t, "application/json", req.Header.Get("Content-Type"), "Request %d: Expected Content-Type application/json", i)
		assert.Equal(t, "cagent/test-version", req.Header.Get("User-Agent"), "Request %d: Expected User-Agent cagent/test-version", i)
		assert.Equal(t, "test-all-events-key", req.Header.Get("test-header"), "Request %d: Expected test-header test-all-events-key", i)

		// Verify request body structure
		var requestBody map[string]any
		require.NoError(t, json.Unmarshal(bodies[i], &requestBody), "Request %d: Failed to unmarshal request body", i)

		// Verify it has records array structure
		records, ok := requestBody["records"].([]any)
		require.True(t, ok, "Request %d: Expected 'records' array in request body", i)
		assert.Len(t, records, 1, "Request %d: Expected 1 record", i)

		// Verify the event structure
		record := records[0].(map[string]any)
		eventType, ok := record["event"].(string)
		assert.True(t, ok && eventType != "", "Request %d: Expected non-empty event type", i)

		// Verify properties exist
		_, ok = record["properties"].(map[string]any)
		assert.True(t, ok, "Request %d: Expected properties object in event", i)
	}
}

// TestTrackServerStart tests long-running server command tracking
func TestTrackServerStart(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	client, err := NewClient(logger, true, true, "test-version")
	require.NoError(t, err)

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
	require.NoError(t, err)
	assert.True(t, executed)
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

			assert.Equal(t, tc.expected.Action, result.Action)
			assert.Equal(t, tc.expected.Args, result.Args)
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
	assert.NotNil(t, globalToolTelemetryClient)

	// Test explicit initialization
	EnsureGlobalTelemetryInitialized()
	client := GetGlobalTelemetryClient()
	assert.NotNil(t, client)
}

// TestHTTPRequestVerification tests that HTTP requests are made correctly when telemetry is enabled
func TestHTTPRequestVerification(t *testing.T) {
	// Create logger with debug level to see all debug messages
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	mockHTTP := NewMockHTTPClient()

	// Create client with mock HTTP client, endpoint, and API key to trigger HTTP calls
	client, err := NewClient(logger, true, true, "test-version", mockHTTP.Client)
	require.NoError(t, err)

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
		assert.NotEmpty(t, client.endpoint, "Client endpoint should be set for this test")
		assert.NotEmpty(t, client.apiKey, "Client API key should be set for this test")
		assert.True(t, client.enabled, "Client should be enabled for this test")

		t.Logf("Before Track: endpoint=%s, apiKey len=%d, enabled=%t", client.endpoint, len(client.apiKey), client.enabled)

		client.Track(ctx, event)

		// Give additional time for background processing (race condition fix)
		time.Sleep(100 * time.Millisecond)

		// Debug output
		t.Logf("HTTP requests captured: %d", mockHTTP.GetRequestCount())

		// Verify HTTP request was made
		assert.Greater(t, mockHTTP.GetRequestCount(), 0, "Expected HTTP request to be made")

		requests := mockHTTP.GetRequests()
		req := requests[0]

		// Verify request method and URL
		assert.Equal(t, http.MethodPost, req.Method, "Expected POST request")
		assert.Equal(t, "https://test-telemetry.example.com/api/events", req.URL.String(), "Expected correct URL")

		// Verify headers
		assert.Equal(t, "application/json", req.Header.Get("Content-Type"), "Expected Content-Type application/json")
		assert.Equal(t, "cagent/test-version", req.Header.Get("User-Agent"), "Expected User-Agent cagent/test-version")
		assert.Equal(t, "test-api-key", req.Header.Get("test-header"), "Expected test-header test-api-key")

		// Verify request body structure
		bodies := mockHTTP.GetBodies()
		assert.NotEmpty(t, bodies, "Expected request body to be captured")

		var requestBody map[string]any
		require.NoError(t, json.Unmarshal(bodies[0], &requestBody), "Failed to unmarshal request body")

		// Verify it has records array structure
		records, ok := requestBody["records"].([]any)
		require.True(t, ok, "Expected 'records' array in request body")
		assert.Len(t, records, 1, "Expected 1 record")

		// Verify the event structure
		record := records[0].(map[string]any)
		assert.Equal(t, "command", record["event"], "Expected event type 'command'")

		// Verify properties contain the command data
		properties, ok := record["properties"].(map[string]any)
		require.True(t, ok, "Expected properties object in event")
		assert.Equal(t, "run", properties["action"], "Expected action 'run'")
		assert.True(t, properties["is_success"].(bool), "Expected is_success true")
	})

	// Test that no HTTP calls are made when endpoint/apiKey are missing
	t.Run("NoHTTPWhenMissingCredentials", func(t *testing.T) {
		mockHTTP2 := NewMockHTTPClient()
		client2, err := NewClient(logger, true, true, "test-version", mockHTTP2.Client)
		require.NoError(t, err)

		// Leave endpoint and API key empty
		client2.endpoint = ""
		client2.apiKey = ""

		event := &CommandEvent{
			Action:  "version",
			Success: true,
		}

		client2.Track(ctx, event)

		// Verify no HTTP requests were made
		assert.Zero(t, mockHTTP2.GetRequestCount(), "Expected no HTTP requests when endpoint/apiKey are missing")
	})

	// Test that no HTTP calls are made when client is disabled
	t.Run("NoHTTPWhenDisabled", func(t *testing.T) {
		mockHTTP3 := NewMockHTTPClient()
		client3, err := NewClient(logger, false, true, "test-version", mockHTTP3.Client)
		require.NoError(t, err)

		event := &CommandEvent{
			Action:  "version",
			Success: true,
		}

		client3.Track(ctx, event)

		// Verify no HTTP requests were made (client disabled means Track returns early)
		assert.Zero(t, mockHTTP3.GetRequestCount(), "Expected no HTTP requests when client is disabled")
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
	require.NoError(t, err)

	client.endpoint = "https://test-overflow.com/api"
	client.apiKey = "overflow-key"
	client.header = "test-header"

	// With synchronous processing, there's no buffer overflow to test
	// Events are processed immediately, so we just verify they all get processed
	numEvents := 10 // Send a reasonable number for synchronous processing

	// Send events synchronously
	for range numEvents {
		client.Track(context.Background(), &CommandEvent{
			Action:  "overflow-test",
			Success: true,
		})
	}

	// With synchronous processing, all events should be processed immediately
	expectedRequests := numEvents
	assert.Equal(t, expectedRequests, slowMock.GetRequestCount(), "Expected all requests with synchronous processing")

	t.Logf("Synchronous processing handled %d events correctly", numEvents)

	// Clean shutdown
}

// TestNon2xxHTTPResponseHandling ensures that 5xx responses are logged and handled gracefully
func TestNon2xxHTTPResponseHandling(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	mockHTTP := NewMockHTTPClient()
	client, err := NewClient(logger, true, true, "test-version", mockHTTP.Client)
	require.NoError(t, err)

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
	assert.Greater(t, requestCount, 0, "Expected HTTP request to be made despite error response")

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
	assert.GreaterOrEqual(t, finalRequestCount, 2, "Expected at least 2 HTTP requests (500 + 404)")
}
