package a2a

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	a2av1 "github.com/a2aproject/a2a-go/v2/a2a"
	a2asrvv1 "github.com/a2aproject/a2a-go/v2/a2asrv"

	"github.com/docker/docker-agent/pkg/tools"
)

func TestToolSetDiscoversCardUnderBaseURLPath(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	card := testV1Card(a2av1.NewAgentInterface(server.URL+"/subagent/invoke", a2av1.TransportProtocolJSONRPC))
	mux.Handle("/subagent"+a2asrvv1.WellKnownAgentCardPath, a2asrvv1.NewStaticAgentCardHandler(card))
	mux.Handle("/subagent/invoke", a2asrvv1.NewJSONRPCHandler(a2asrvv1.NewHandler(testV1Executor{})))

	assertToolSetCalls(t, NewToolset("test", server.URL+"/subagent", nil, WithAllowPrivateIPs(true)))
}

func TestToolSetRejectsNullInterfaces(t *testing.T) {
	t.Parallel()

	for name, interfaces := range map[string]string{
		"first": `[null]`,
		"later": `[{"url":"http://127.0.0.1:1/invoke","protocolBinding":"JSONRPC","protocolVersion":"1.0"},null]`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			url := serveCardJSON(t, func(string) string {
				return fmt.Sprintf(`{"name":"bad-card","supportedInterfaces":%s}`, interfaces)
			})

			err := NewToolset("test", url, nil, WithAllowPrivateIPs(true)).Start(t.Context())
			if err == nil {
				t.Fatal("Start() expected error")
			}
		})
	}
}

// serveCardJSON serves the card returned by cardJSON(serverURL) at the well-known path.
func serveCardJSON(t *testing.T, cardJSON func(serverURL string) string) string {
	t.Helper()

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	mux.HandleFunc(a2asrvv1.WellKnownAgentCardPath, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, cardJSON(server.URL))
	})
	return server.URL
}

func testV1Card(interfaces ...*a2av1.AgentInterface) *a2av1.AgentCard {
	return &a2av1.AgentCard{
		Name:                "test",
		Description:         "test",
		Version:             "1.0.0",
		SupportedInterfaces: interfaces,
		Capabilities:        a2av1.AgentCapabilities{Streaming: true},
		DefaultInputModes:   []string{"text/plain"},
		DefaultOutputModes:  []string{"text/plain"},
		Skills:              []a2av1.AgentSkill{{ID: "test", Name: "test", Description: "test", Tags: []string{"test"}}},
	}
}

func assertToolSetCalls(t *testing.T, toolSet *Toolset) {
	t.Helper()

	if err := toolSet.Start(t.Context()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	toolList, err := toolSet.Tools(t.Context())
	if err != nil {
		t.Fatalf("Tools() error = %v", err)
	}
	if len(toolList) != 1 {
		t.Fatalf("Tools() returned %d tools, want 1", len(toolList))
	}

	result, err := toolList[0].Handler(t.Context(), tools.ToolCall{Function: tools.FunctionCall{Arguments: `{"message":"hello"}`}}, tools.NopRuntime{})
	if err != nil {
		t.Fatalf("Handler() error = %v", err)
	}
	if result == nil || result.Output != "ok" {
		t.Fatalf("Handler() result = %+v, want output %q", result, "ok")
	}
}
