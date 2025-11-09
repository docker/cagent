package builtin

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	latest "github.com/docker/cagent/pkg/config/v2"
	"github.com/docker/cagent/pkg/tools"
)

type testServer struct {
	serverURL       string
	receivedURL     string
	receivedHeaders http.Header
	receivedMethod  string
	receivedBody    []byte
}

func getTestServer(t *testing.T) *testServer {
	t.Helper()

	ts := &testServer{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ts.receivedHeaders = r.Header.Clone()
		ts.receivedURL = r.URL.String()
		ts.receivedMethod = r.Method

		var err error
		ts.receivedBody, err = io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))

	ts.serverURL = server.URL

	t.Cleanup(func() {
		server.Close()
	})

	return ts
}

func TestAPITool_GET(t *testing.T) {
	t.Parallel()
	ts := getTestServer(t)

	tool := NewAPITool(latest.APIToolConfig{
		Method:   http.MethodGet,
		Endpoint: ts.serverURL + "/api?key=${key}&value=${value}",
	})

	result, err := tool.handler.CallTool(t.Context(), tools.ToolCall{
		Function: tools.FunctionCall{
			Arguments: `{"key": "mykey", "value": "myvalue"}`,
		},
	})

	require.NoError(t, err)
	assert.JSONEq(t, `{"status":"ok"}`, result.Output)
	assert.Equal(t, http.MethodGet, ts.receivedMethod)
	assert.Equal(t, "/api?key=mykey&value=myvalue", ts.receivedURL)
}

func TestAPITool_POST(t *testing.T) {
	t.Parallel()
	ts := getTestServer(t)

	tool := NewAPITool(latest.APIToolConfig{
		Method:   http.MethodPost,
		Endpoint: ts.serverURL,
	})

	result, err := tool.handler.CallTool(t.Context(), tools.ToolCall{
		Function: tools.FunctionCall{
			Arguments: `{"name":"John Doe","age":30}`,
		},
	})

	require.NoError(t, err)
	assert.JSONEq(t, `{"status":"ok"}`, result.Output)
	assert.Equal(t, http.MethodPost, ts.receivedMethod)
	assert.Equal(t, "application/json", ts.receivedHeaders.Get("Content-Type"))

	var receivedData map[string]any
	err = json.Unmarshal(ts.receivedBody, &receivedData)
	require.NoError(t, err)
	assert.Equal(t, "John Doe", receivedData["name"])
	assert.InEpsilon(t, 30.0, receivedData["age"], 0.01)
}

func TestAPITool_Headers(t *testing.T) {
	t.Parallel()
	ts := getTestServer(t)

	tool := NewAPITool(latest.APIToolConfig{
		Method:   http.MethodGet,
		Endpoint: ts.serverURL,
		Headers: map[string]string{
			"X-Custom-Header":  "custom-value",
			"X-API-Key":        "secret-key",
			"X-Another-Header": "another-value",
		},
	})

	result, err := tool.handler.CallTool(t.Context(), tools.ToolCall{})

	require.NoError(t, err)
	assert.JSONEq(t, `{"status":"ok"}`, result.Output)
	assert.Equal(t, "custom-value", ts.receivedHeaders.Get("X-Custom-Header"))
	assert.Equal(t, "secret-key", ts.receivedHeaders.Get("X-API-Key"))
	assert.Equal(t, "another-value", ts.receivedHeaders.Get("X-Another-Header"))
}
