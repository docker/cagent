package e2e_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	a2aserver "github.com/docker/cagent/pkg/a2a"
	"github.com/docker/cagent/pkg/config"
)

type Response struct {
	Jsonrpc string  `json:"jsonrpc"`
	ID      string  `json:"id"`
	Result  *Result `json:"result,omitempty"`
	Error   any     `json:"error,omitempty"`
}

type Result struct {
	Artifacts []Artifact `json:"artifacts"`
}

type Artifact struct {
	Parts []Part `json:"parts"`
}

type Part struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

func TestA2AServer_AgentCard(t *testing.T) {
	t.Parallel()

	_, runConfig := startRecordingAIProxy(t)
	agentCard := startA2AServer(t, "testdata/basic.yaml", runConfig)

	assert.Equal(t, "basic", agentCard.Name)
	assert.NotEmpty(t, agentCard.Description)
	assert.Equal(t, a2a.TransportProtocolJSONRPC, agentCard.PreferredTransport)
	assert.Contains(t, agentCard.URL, "/invoke")
	assert.True(t, agentCard.Capabilities.Streaming)
	assert.NotEmpty(t, agentCard.Version)
}

func TestA2AServer_Invoke(t *testing.T) {
	t.Parallel()

	_, runConfig := startRecordingAIProxy(t)
	agentCard := startA2AServer(t, "testdata/basic.yaml", runConfig)

	requestID := "test-request-1"
	jsonRPCRequest := map[string]any{
		"jsonrpc": "2.0",
		"id":      requestID,
		"method":  "message/send",
		"params": map[string]any{
			"message": map[string]any{
				"messageId": "msg-1",
				"role":      "user",
				"parts": []map[string]any{
					{
						"kind": "text",
						"text": "What is 2+2? Answer with just the number.",
					},
				},
			},
		},
	}

	requestBody, err := json.Marshal(jsonRPCRequest)
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, agentCard.URL, bytes.NewReader(requestBody))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	responseBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var jsonRPCResponse Response
	err = json.Unmarshal(responseBody, &jsonRPCResponse)
	require.NoError(t, err)

	assert.Equal(t, "2.0", jsonRPCResponse.Jsonrpc)
	assert.Equal(t, requestID, jsonRPCResponse.ID)
	assert.Nil(t, jsonRPCResponse.Error)
	require.NotNil(t, jsonRPCResponse.Result)
	assert.Len(t, jsonRPCResponse.Result.Artifacts, 1)
	assert.Len(t, jsonRPCResponse.Result.Artifacts[0].Parts, 2)
	assert.Equal(t, "text", jsonRPCResponse.Result.Artifacts[0].Parts[0].Kind)
	assert.Equal(t, "4", jsonRPCResponse.Result.Artifacts[0].Parts[0].Text)
	assert.Equal(t, "text", jsonRPCResponse.Result.Artifacts[0].Parts[1].Kind)
	assert.Equal(t, "4", jsonRPCResponse.Result.Artifacts[0].Parts[1].Text)
}

func TestA2AServer_MultipleRequests(t *testing.T) {
	t.Parallel()

	_, runConfig := startRecordingAIProxy(t)
	agentCard := startA2AServer(t, "testdata/basic.yaml", runConfig)

	messages := []string{
		"Say 'hello' in one word.",
		"Say 'goodbye' in one word.",
	}

	for i, message := range messages {
		t.Run(fmt.Sprintf("request_%d", i), func(t *testing.T) {
			requestID := fmt.Sprintf("test-request-%d", i)
			jsonRPCRequest := map[string]any{
				"jsonrpc": "2.0",
				"id":      requestID,
				"method":  "message/send",
				"params": map[string]any{
					"message": map[string]any{
						"messageId": fmt.Sprintf("msg-%d", i),
						"role":      "user",
						"parts": []map[string]any{
							{
								"kind": "text",
								"text": message,
							},
						},
					},
				},
			}

			requestBody, err := json.Marshal(jsonRPCRequest)
			require.NoError(t, err)

			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, agentCard.URL, bytes.NewReader(requestBody))
			require.NoError(t, err)
			req.Header.Set("Content-Type", "application/json")

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusOK, resp.StatusCode)

			responseBody, err := io.ReadAll(resp.Body)
			require.NoError(t, err)

			var jsonRPCResponse Response
			err = json.Unmarshal(responseBody, &jsonRPCResponse)
			require.NoError(t, err)

			assert.Equal(t, requestID, jsonRPCResponse.ID)
			assert.Nil(t, jsonRPCResponse.Error)
			assert.NotNil(t, jsonRPCResponse.Result)
		})
	}
}

func TestA2AServer_MultiAgent(t *testing.T) {
	t.Parallel()

	_, runConfig := startRecordingAIProxy(t)
	agentCard := startA2AServer(t, "testdata/multi.yaml", runConfig)

	requestID := "test-multi-1"
	jsonRPCRequest := map[string]any{
		"jsonrpc": "2.0",
		"id":      requestID,
		"method":  "message/send",
		"params": map[string]any{
			"message": map[string]any{
				"messageId": "msg-multi-1",
				"role":      "user",
				"parts": []map[string]any{
					{
						"kind": "text",
						"text": "Say hello.",
					},
				},
			},
		},
	}

	requestBody, err := json.Marshal(jsonRPCRequest)
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, agentCard.URL, bytes.NewReader(requestBody))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	responseBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var jsonRPCResponse Response
	err = json.Unmarshal(responseBody, &jsonRPCResponse)
	require.NoError(t, err)

	assert.Equal(t, requestID, jsonRPCResponse.ID)
	assert.Nil(t, jsonRPCResponse.Error)
	require.NotNil(t, jsonRPCResponse.Result)
	require.Len(t, jsonRPCResponse.Result.Artifacts, 1)
	require.NotEmpty(t, jsonRPCResponse.Result.Artifacts[0].Parts)

	// The last part contains the complete response text
	lastPart := jsonRPCResponse.Result.Artifacts[0].Parts[len(jsonRPCResponse.Result.Artifacts[0].Parts)-1]
	assert.Equal(t, "text", lastPart.Kind)
	assert.Contains(t, lastPart.Text, "Hello")
}

func startA2AServer(t *testing.T, agentFile string, runConfig *config.RuntimeConfig) a2a.AgentCard {
	t.Helper()

	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", ":0")

	require.NoError(t, err)

	go func() {
		_ = a2aserver.Run(t.Context(), agentFile, "root", runConfig, ln)
	}()

	port := ln.Addr().(*net.TCPAddr).Port
	serverURL := fmt.Sprintf("http://localhost:%d", port)

	resp, err := http.Get(serverURL + a2asrv.WellKnownAgentCardPath)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var agentCard a2a.AgentCard
	err = json.NewDecoder(resp.Body).Decode(&agentCard)
	require.NoError(t, err)

	return agentCard
}
