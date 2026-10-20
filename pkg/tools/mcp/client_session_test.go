package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/tools/lifecycle"
)

func TestClientSessionCloseAfterWait(t *testing.T) {
	t.Parallel()

	crashErr := errors.New("process exited")
	cleanupErr := errors.New("cleanup failed")
	wrappedCleanup := fmt.Errorf("closing connection: %w", cleanupErr)
	tests := []struct {
		name     string
		waitErr  error
		closeErr error
		wantErr  error
	}{
		{name: "clean", waitErr: crashErr},
		{name: "repeated", waitErr: crashErr, closeErr: crashErr},
		{name: "wrapped repeated", waitErr: crashErr, closeErr: fmt.Errorf("close: %w", crashErr)},
		{name: "wrapped wait", waitErr: fmt.Errorf("wait: %w", crashErr), closeErr: crashErr},
		{name: "joined wait", waitErr: errors.Join(crashErr, errors.New("read failed")), closeErr: crashErr},
		{name: "distinct", waitErr: crashErr, closeErr: cleanupErr, wantErr: cleanupErr},
		{name: "distinct wrapped", waitErr: crashErr, closeErr: wrappedCleanup, wantErr: wrappedCleanup},
		{name: "clean wait", closeErr: cleanupErr, wantErr: cleanupErr},
		{name: "joined", waitErr: crashErr, closeErr: errors.Join(crashErr, cleanupErr), wantErr: cleanupErr},
		{name: "wrapped join", waitErr: crashErr, closeErr: fmt.Errorf("close: %w", errors.Join(crashErr, cleanupErr)), wantErr: cleanupErr},
		{name: "nested join", waitErr: crashErr, closeErr: errors.Join(fmt.Errorf("close: %w", crashErr), errors.Join(crashErr, cleanupErr)), wantErr: cleanupErr},
		{name: "same message is distinct", waitErr: errors.New(cleanupErr.Error()), closeErr: cleanupErr, wantErr: cleanupErr},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			session := &clientSession{client: &sessionErrorClient{
				waitFn:  func() error { return tt.waitErr },
				closeFn: func() error { return tt.closeErr },
			}}
			err := session.Wait()
			if tt.waitErr == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tt.waitErr)
			}
			for range 2 {
				err := session.Close(t.Context())
				if tt.wantErr == nil {
					require.NoError(t, err)
				} else {
					require.ErrorIs(t, err, tt.wantErr)
					if tt.waitErr != nil {
						assert.NotErrorIs(t, err, tt.waitErr)
					}
				}
			}
		})
	}
}

func TestClientSessionConcurrentWaitClose(t *testing.T) {
	t.Parallel()

	for _, waitFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("waitFirst=%t", waitFirst), func(t *testing.T) {
			t.Parallel()
			crashErr := errors.New("process exited")
			waitReady := make(chan struct{})
			closeStarted := make(chan struct{})
			waitDone := make(chan error, 1)
			session := &clientSession{client: &sessionErrorClient{
				waitFn: func() error {
					close(waitReady)
					if !waitFirst {
						<-closeStarted
					}
					return crashErr
				},
				closeFn: func() error {
					close(closeStarted)
					if !waitFirst {
						// A failure first observed during shutdown must not be hidden.
						require.ErrorIs(t, <-waitDone, crashErr)
					}
					return crashErr
				},
			}}
			go func() { waitDone <- session.Wait() }()
			<-waitReady
			if waitFirst {
				require.ErrorIs(t, <-waitDone, crashErr)
			}
			err := session.Close(t.Context())
			if waitFirst {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, crashErr)
			}
		})
	}
}

func TestClientSessionConcurrentCalls(t *testing.T) {
	t.Parallel()

	crashErr := errors.New("process exited")
	cleanupErr := errors.New("cleanup failed")
	session := &clientSession{client: &sessionErrorClient{
		waitFn:  func() error { return crashErr },
		closeFn: func() error { return errors.Join(crashErr, cleanupErr) },
	}}
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() { assert.ErrorIs(t, session.Wait(), crashErr) })
		wg.Go(func() { assert.ErrorIs(t, session.Close(t.Context()), cleanupErr) })
	}
	wg.Wait()
	err := session.Close(t.Context())
	require.ErrorIs(t, err, cleanupErr)
	assert.NotErrorIs(t, err, crashErr)
}

type sessionErrorClient struct {
	mockMCPClient

	waitFn  func() error
	closeFn func() error
}

func (c *sessionErrorClient) Wait() error                 { return c.waitFn() }
func (c *sessionErrorClient) Close(context.Context) error { return c.closeFn() }

func TestClientSessionStdioCrashHelper(t *testing.T) {
	if os.Getenv("MCP_CRASH_HELPER") != "1" {
		return
	}
	marker, err := os.OpenFile(os.Getenv("MCP_CRASH_MARKER"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	first := err == nil
	if first {
		require.NoError(t, marker.Close())
	} else {
		require.ErrorIs(t, err, os.ErrExist)
	}

	server := gomcp.NewServer(&gomcp.Implementation{Name: "crash-helper", Version: "1"}, nil)
	gomcp.AddTool(server, &gomcp.Tool{Name: "ping"}, func(context.Context, *gomcp.CallToolRequest, struct{}) (*gomcp.CallToolResult, any, error) {
		text := "reconnected"
		if first {
			text = "first"
		}
		return &gomcp.CallToolResult{Content: []gomcp.Content{&gomcp.TextContent{Text: text}}}, nil, nil
	})
	gomcp.AddTool(server, &gomcp.Tool{Name: "crash"}, func(context.Context, *gomcp.CallToolRequest, struct{}) (*gomcp.CallToolResult, any, error) {
		os.Exit(7)
		return nil, nil, nil
	})
	_ = server.Run(t.Context(), &gomcp.StdioTransport{})
	os.Exit(0)
}

func TestClientSessionStdioCrashReconnectStop(t *testing.T) {
	t.Parallel()

	executable, err := os.Executable()
	require.NoError(t, err)
	wd := t.TempDir()
	env := append(os.Environ(), "MCP_CRASH_HELPER=1", "MCP_CRASH_MARKER="+filepath.Join(wd, "started"))
	ts := NewToolsetCommand("crash", executable, []string{"-test.run=^TestClientSessionStdioCrashHelper$"}, env, wd, lifecycle.Policy{
		Backoff: lifecycle.Backoff{Initial: time.Millisecond},
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 10*time.Second)
		defer cancel()
		assert.NoError(t, ts.Stop(ctx))
	})
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	require.NoError(t, ts.Start(ctx))
	result, err := ts.mcpClient.CallTool(ctx, &gomcp.CallToolParams{Name: "ping"})
	require.NoError(t, err)
	require.Equal(t, "first", processMCPContent(result).Output)

	// Call the client directly so only the supervisor's automatic restart can recover.
	_, err = ts.mcpClient.CallTool(ctx, &gomcp.CallToolParams{Name: "crash"})
	require.Error(t, err)
	require.Eventually(t, func() bool {
		if !ts.IsStarted() {
			return false
		}
		result, err := ts.mcpClient.CallTool(ctx, &gomcp.CallToolParams{Name: "ping"})
		return err == nil && processMCPContent(result).Output == "reconnected"
	}, 10*time.Second, 10*time.Millisecond)

	require.NoError(t, ts.Stop(ctx), "recovered process exit must not become a cleanup error")
}
