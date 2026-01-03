package toolserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/agent"
	"github.com/docker/cagent/pkg/tools"
)

// mockToolSet is a simple toolset for testing.
type mockToolSet struct {
	tools.BaseToolSet
	toolList []tools.Tool
}

func (m *mockToolSet) Tools(context.Context) ([]tools.Tool, error) {
	return m.toolList, nil
}

func TestServer_HandleHealth(t *testing.T) {
	t.Parallel()

	s := &Server{
		agents: make(map[string]*agent.Agent),
	}

	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	w := httptest.NewRecorder()

	s.handleHealth(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "ok")
}

func TestServer_HandleCallAgentTool(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	mockTools := []tools.Tool{
		{
			Name:        "greet",
			Description: "Greet tool",
			Handler: func(_ context.Context, tc tools.ToolCall) (*tools.ToolCallResult, error) {
				var args struct {
					Name string `json:"name"`
				}
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
				return tools.ResultSuccess("Hello, " + args.Name), nil
			},
		},
	}

	testAgent := agent.New("greeter", "Greeter agent",
		agent.WithToolSets(&mockToolSet{toolList: mockTools}),
	)

	s := &Server{
		agents: map[string]*agent.Agent{
			"greeter": testAgent,
		},
	}

	body := `{"arguments": "{\"name\": \"World\"}"}`
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/agents/greeter/tools/greet", strings.NewReader(body))
	req.SetPathValue("agent", "greeter")
	req.SetPathValue("tool", "greet")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.handleCallAgentTool(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp CallToolResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "Hello, World", resp.Output)
}

func TestServer_HandleCallAgentTool_AgentNotFound(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	s := &Server{
		agents: make(map[string]*agent.Agent),
	}

	body := `{"arguments": "{}"}`
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/agents/nonexistent/tools/greet", strings.NewReader(body))
	req.SetPathValue("agent", "nonexistent")
	req.SetPathValue("tool", "greet")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.handleCallAgentTool(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)

	var resp ErrorResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "not found")
}

func TestServer_HandleCallAgentTool_ToolNotFound(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	testAgent := agent.New("test", "Test agent",
		agent.WithToolSets(&mockToolSet{toolList: []tools.Tool{}}),
	)

	s := &Server{
		agents: map[string]*agent.Agent{
			"test": testAgent,
		},
	}

	body := `{"arguments": "{}"}`
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/agents/test/tools/nonexistent", strings.NewReader(body))
	req.SetPathValue("agent", "test")
	req.SetPathValue("tool", "nonexistent")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.handleCallAgentTool(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)

	var resp ErrorResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "not found")
}

func TestServer_HandleCallAgentTool_InvalidJSON(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	testAgent := agent.New("test", "Test agent")

	s := &Server{
		agents: map[string]*agent.Agent{
			"test": testAgent,
		},
	}

	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/agents/test/tools/foo", strings.NewReader("not json"))
	req.SetPathValue("agent", "test")
	req.SetPathValue("tool", "foo")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.handleCallAgentTool(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServer_HandleCallAgentTool_ToolError(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	mockTools := []tools.Tool{
		{
			Name:        "failing",
			Description: "A failing tool",
			Handler: func(_ context.Context, _ tools.ToolCall) (*tools.ToolCallResult, error) {
				return tools.ResultError("something went wrong"), nil
			},
		},
	}

	testAgent := agent.New("test", "Test agent",
		agent.WithToolSets(&mockToolSet{toolList: mockTools}),
	)

	s := &Server{
		agents: map[string]*agent.Agent{
			"test": testAgent,
		},
	}

	body := `{"arguments": "{}"}`
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/agents/test/tools/failing", strings.NewReader(body))
	req.SetPathValue("agent", "test")
	req.SetPathValue("tool", "failing")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.handleCallAgentTool(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp CallToolResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "something went wrong", resp.Output)
	assert.True(t, resp.IsError)
}
