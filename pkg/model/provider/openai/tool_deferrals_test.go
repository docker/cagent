package openai

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/config/latest"
	"github.com/docker/docker-agent/pkg/environment"
	"github.com/docker/docker-agent/pkg/model/provider/base"
	"github.com/docker/docker-agent/pkg/tools"
)

type deferredToolPayload struct {
	Name         string         `json:"name"`
	Type         string         `json:"type"`
	DeferLoading *bool          `json:"defer_loading"`
	Description  string         `json:"description"`
	Parameters   map[string]any `json:"parameters"`
	Strict       *bool          `json:"strict"`
}

type deferredInputPayload struct {
	Type      string                `json:"type"`
	CallID    string                `json:"call_id"`
	Execution string                `json:"execution"`
	Tools     []deferredToolPayload `json:"tools"`
}

type deferredRequestPayload struct {
	Tools []deferredToolPayload  `json:"tools"`
	Input []deferredInputPayload `json:"input"`
}

func TestSupportsDeferredToolsCompatibility(t *testing.T) {
	t.Parallel()

	assert.True(t, (&Client{Config: base.Config{ModelConfig: latest.ModelConfig{Provider: "openai", Model: "gpt-5.4"}}}).supportsDeferredTools())
	assert.False(t, (&Client{Config: base.Config{ModelConfig: latest.ModelConfig{Provider: "openai", Model: "gpt-5.4-nano"}}}).supportsDeferredTools())
	assert.True(t, (&Client{Config: base.Config{ModelConfig: latest.ModelConfig{
		Provider: "custom", Model: "gpt-5.4", ProviderOpts: map[string]any{"supports_deferred_tools": true},
	}}}).supportsDeferredTools())
}

func TestResponsesAPI_LoadsDeferredToolsThroughClientToolSearch(t *testing.T) {
	var (
		mu       sync.Mutex
		requests []deferredRequestPayload
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if !assert.NoError(t, err) {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		var request deferredRequestPayload
		if !assert.NoError(t, json.Unmarshal(body, &request)) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		requests = append(requests, request)
		mu.Unlock()
		writeResponsesSSEResponse(w)
	}))
	defer server.Close()

	client, err := NewClient(t.Context(), &latest.ModelConfig{
		Provider: "openai",
		Model:    "gpt-5.4",
		BaseURL:  server.URL,
		ProviderOpts: map[string]any{
			"api_type": "openai_responses",
		},
	}, environment.NewMapEnvProvider(map[string]string{"OPENAI_API_KEY": "secret"}))
	require.NoError(t, err)

	readTool := tools.Tool{Name: "read", Parameters: map[string]any{"type": "object"}}
	searchTool := tools.Tool{
		Name:                 "search",
		Description:          "Search the workspace",
		Parameters:           map[string]any{"type": "object"},
		Deferred:             true,
		DeferredAtToolCallID: "call-1",
	}
	initialMessages := []chat.Message{{Role: chat.MessageRoleUser, Content: "hi"}}
	loadedMessages := []chat.Message{
		{Role: chat.MessageRoleUser, Content: "hi"},
		{Role: chat.MessageRoleAssistant, ToolCalls: []tools.ToolCall{{
			ID: "call-1", Type: "function", Function: tools.FunctionCall{Name: "add_tool", Arguments: `{}`},
		}}},
		{Role: chat.MessageRoleTool, ToolCallID: "call-1", Content: "activated"},
	}

	for _, request := range []struct {
		messages []chat.Message
		tools    []tools.Tool
	}{{initialMessages, []tools.Tool{readTool}}, {loadedMessages, []tools.Tool{readTool, searchTool}}} {
		stream, err := client.CreateResponseStream(t.Context(), request.messages, request.tools)
		require.NoError(t, err)
		for {
			if _, err := stream.Recv(); err != nil {
				break
			}
		}
		stream.Close()
	}

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, requests, 2)
	require.Len(t, requests[0].Tools, 1)
	assert.Equal(t, "read", requests[0].Tools[0].Name)
	require.Len(t, requests[1].Tools, 1)
	assert.Equal(t, "read", requests[1].Tools[0].Name)

	var call *deferredInputPayload
	var output *deferredInputPayload
	for i := range requests[1].Input {
		switch requests[1].Input[i].Type {
		case "tool_search_call":
			call = &requests[1].Input[i]
		case "tool_search_output":
			output = &requests[1].Input[i]
		}
	}
	require.NotNil(t, call)
	require.NotNil(t, output)
	assert.Equal(t, "client", call.Execution)
	assert.Equal(t, call.CallID, output.CallID)
	require.Len(t, output.Tools, 1)
	assert.Equal(t, "search", output.Tools[0].Name)
	assert.Equal(t, "function", output.Tools[0].Type)
	assert.Equal(t, searchTool.Description, output.Tools[0].Description)
	assert.Equal(t, "object", output.Tools[0].Parameters["type"])
	require.NotNil(t, output.Tools[0].Strict)
	assert.True(t, *output.Tools[0].Strict)
	require.NotNil(t, output.Tools[0].DeferLoading)
	assert.True(t, *output.Tools[0].DeferLoading)
}
