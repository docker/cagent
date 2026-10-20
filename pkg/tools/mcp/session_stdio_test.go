package mcp

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/tools"
)

func TestSessionStdioHelper(t *testing.T) {
	if os.Getenv("MCP_SESSION_HELPER") != "1" {
		return
	}
	marker := os.Getenv("MCP_MARKER")
	_ = os.WriteFile(marker+".started", []byte("started"), 0o600)
	if os.Getenv("MCP_MODE") == "hang-init" {
		_, _ = io.Copy(io.Discard, os.Stdin)
		os.Exit(0)
	}
	server := gomcp.NewServer(&gomcp.Implementation{Name: "test", Version: "1"}, nil)
	gomcp.AddTool(server, &gomcp.Tool{Name: "ping"}, func(context.Context, *gomcp.CallToolRequest, struct{}) (*gomcp.CallToolResult, any, error) {
		return &gomcp.CallToolResult{Content: []gomcp.Content{&gomcp.TextContent{Text: "pong"}}}, nil, nil
	})
	gomcp.AddTool(server, &gomcp.Tool{Name: "exit"}, func(context.Context, *gomcp.CallToolRequest, struct{}) (*gomcp.CallToolResult, any, error) {
		os.Exit(0)
		return nil, nil, nil
	})
	if mode := os.Getenv("MCP_MODE"); mode == "block-pipe" || mode == "sampling" || mode == "sampling-init" {
		decoder, encoder := json.NewDecoder(os.Stdin), json.NewEncoder(os.Stdout)
		for {
			var req struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
			}
			if err := decoder.Decode(&req); err != nil {
				os.Exit(2)
			}
			if len(req.ID) == 0 {
				continue
			}
			if (mode == "sampling-init" && req.Method == "initialize") || (mode == "sampling" && req.Method == "tools/call") {
				// Keep the triggering request pending while sampling is handled.
				_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": "sample", "method": "sampling/createMessage", "params": map[string]any{"maxTokens": 1, "messages": []any{}}})
				continue
			}
			response := map[string]any{"jsonrpc": "2.0", "id": req.ID}
			switch req.Method {
			case "server/discover":
				response["error"] = map[string]any{"code": -32601, "message": "not found"}
			case "initialize":
				response["result"] = map[string]any{"protocolVersion": "2025-11-25", "serverInfo": map[string]any{"name": "blocked", "version": "1"}, "capabilities": map[string]any{"tools": map[string]any{}}}
			case "tools/list":
				response["result"] = map[string]any{"tools": []any{map[string]any{"name": "ping", "inputSchema": map[string]any{"type": "object"}}}}
			default:
				response["result"] = map[string]any{}
			}
			_ = encoder.Encode(response)
			if mode == "block-pipe" && req.Method == "tools/list" {
				_ = os.WriteFile(marker+".blocked", []byte("blocked"), 0o600)
				// Keep the helper alive without reading stdin until the parent kills it.
				<-time.After(time.Hour)
			}
		}
	}
	_ = server.Run(t.Context(), &gomcp.StdioTransport{})
	_ = os.WriteFile(marker+".stopped", []byte("stopped"), 0o600)
	os.Exit(0)
}

func sessionTestToolset(t *testing.T, mode string) (*Toolset, string) {
	t.Helper()
	executable, err := os.Executable()
	require.NoError(t, err)
	wd := t.TempDir()
	marker := filepath.Join(wd, "server")
	env := append(os.Environ(), "MCP_SESSION_HELPER=1", "MCP_MODE="+mode, "MCP_MARKER="+marker)
	ts := NewSessionToolsetCommand("session", executable, []string{"-test.run=^TestSessionStdioHelper$"}, env, wd)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), time.Second)
		defer cancel()
		_ = ts.Stop(ctx)
	})
	return ts, marker
}

func TestSessionStdioSetupCancellation(t *testing.T) {
	t.Parallel()
	ts, marker := sessionTestToolset(t, "hang-init")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- ts.Start(ctx) }()
	require.Eventually(t, func() bool { _, err := os.Stat(marker + ".started"); return err == nil }, 5*time.Second, time.Millisecond)
	cancel()
	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("startup cancellation did not reap the child")
	}
	require.NoError(t, ts.Stop(t.Context()))
}

func TestSessionStdioSurvivesSetupAndDoesNotReconnect(t *testing.T) {
	t.Parallel()
	ts, marker := sessionTestToolset(t, "normal")
	ctx, cancel := context.WithCancel(t.Context())
	require.NoError(t, ts.Start(ctx))
	cancel()
	available, err := ts.Tools(t.Context())
	require.NoError(t, err)
	for _, name := range []string{"session_ping", "session_exit"} {
		for _, tool := range available {
			if tool.Name != name {
				continue
			}
			result, err := tool.Handler(t.Context(), tools.ToolCall{Function: tools.FunctionCall{Name: name, Arguments: "{}"}}, tools.NopRuntime{})
			if name == "session_ping" {
				require.NoError(t, err)
				assert.Equal(t, "pong", result.Output)
			} else {
				require.Error(t, err)
			}
		}
	}
	require.Eventually(t, func() bool { return !ts.IsStarted() }, 5*time.Second, time.Millisecond)
	_, err = ts.Tools(t.Context())
	require.Error(t, err)
	assert.FileExists(t, marker+".started")
	require.NoError(t, ts.Stop(t.Context()))
}

func TestSessionStdioStopUnblocksPipeWrite(t *testing.T) {
	t.Parallel()
	ts, marker := sessionTestToolset(t, "block-pipe")
	require.NoError(t, ts.Start(t.Context()))
	available, err := ts.Tools(t.Context())
	require.NoError(t, err)
	require.Len(t, available, 1)
	tool := available[0]
	require.Eventually(t, func() bool { _, err := os.Stat(marker + ".blocked"); return err == nil }, 5*time.Second, time.Millisecond)
	args, err := json.Marshal(map[string]any{"payload": strings.Repeat("x", 2*1024*1024)})
	require.NoError(t, err)
	called := make(chan error, 1)
	go func() {
		_, err := tool.Handler(t.Context(), tools.ToolCall{Function: tools.FunctionCall{Name: tool.Name, Arguments: string(args)}}, tools.NopRuntime{})
		called <- err
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	stopped := make(chan error, 1)
	go func() { stopped <- ts.Stop(ctx) }()
	select {
	case err := <-stopped:
		require.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(5 * time.Second):
		t.Fatal("stop did not kill the blocked subprocess")
	}
	select {
	case err := <-called:
		require.Error(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("tool call remained blocked after stop")
	}
}

//nolint:staticcheck // Legacy server-initiated sampling remains supported.
func TestSessionStdioStopJoinsSamplingCleanup(t *testing.T) {
	for _, mode := range []string{"sampling", "sampling-init"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			ts, _ := sessionTestToolset(t, mode)
			entered, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			releaseCleanup := sync.OnceFunc(func() { close(release) })
			defer releaseCleanup()
			var live atomic.Int32
			ts.SetSamplingWithToolsHandler(func(ctx context.Context, _ *gomcp.CreateMessageWithToolsParams) (*gomcp.CreateMessageWithToolsResult, error) {
				live.Add(1)
				defer live.Add(-1)
				close(entered)
				<-ctx.Done()
				close(canceled)
				<-release
				return nil, ctx.Err()
			})

			started := make(chan error, 1)
			go func() { started <- ts.Start(t.Context()) }()
			var called chan error
			if mode == "sampling" {
				select {
				case err := <-started:
					require.NoError(t, err)
				case <-time.After(5 * time.Second):
					t.Fatal("startup did not complete")
				}
				called = make(chan error, 1)
				go func() {
					_, err := ts.mcpClient.CallTool(t.Context(), &gomcp.CallToolParams{Name: "sample", Arguments: map[string]any{}})
					called <- err
				}()
			}
			select {
			case <-entered:
			case err := <-called:
				t.Fatalf("tool call returned before sampling: %v", err)
			case <-time.After(5 * time.Second):
				t.Fatal("sampling handler not entered")
			}
			if mode == "sampling-init" {
				select {
				case err := <-started:
					t.Fatalf("startup completed before initialize response: %v", err)
				default:
				}
			}

			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()
			stopped := make(chan error, 1)
			go func() { stopped <- ts.Stop(ctx) }()
			select {
			case <-canceled:
			case <-time.After(5 * time.Second):
				t.Fatal("stop did not cancel the sampling handler")
			}
			<-ctx.Done()
			require.EqualValues(t, 1, live.Load())
			select {
			case err := <-stopped:
				t.Fatalf("stop returned before sampling cleanup finished: %v", err)
			case <-time.After(50 * time.Millisecond):
			}

			releaseCleanup()
			select {
			case err := <-stopped:
				require.ErrorIs(t, err, context.DeadlineExceeded)
				if mode == "sampling" {
					var exitErr *exec.ExitError
					require.ErrorAs(t, err, &exitErr, "retain subprocess cleanup errors alongside the deadline")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("stop did not join sampling cleanup")
			}
			require.Zero(t, live.Load(), "stop must not leave live sampling handlers")
			if mode == "sampling-init" {
				select {
				case err := <-started:
					require.Error(t, err)
				case <-time.After(5 * time.Second):
					t.Fatal("startup remained blocked after stop")
				}
			} else {
				select {
				case err := <-called:
					require.Error(t, err)
				case <-time.After(5 * time.Second):
					t.Fatal("tool call remained blocked after stop")
				}
			}
		})
	}
}
