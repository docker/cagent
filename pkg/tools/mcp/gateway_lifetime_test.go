package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/gateway"
	"github.com/docker/docker-agent/pkg/tools/lifecycle"
)

func runGatewayLifetimeHelper(marker string) {
	var config string
	for i, arg := range os.Args {
		if arg == "--config" {
			config = os.Args[i+1]
		}
	}
	if os.WriteFile(marker, []byte(config), 0o600) != nil {
		os.Exit(8)
	}
	for {
		if _, err := os.Stat(marker + ".release"); err == nil {
			break
		}
		time.Sleep(time.Millisecond) //nolint:forbidigo // Subprocess waits for a parent-controlled file barrier.
	}
	if _, err := os.ReadFile(config); err != nil {
		os.Exit(7)
	}
	if os.WriteFile(marker+".read", nil, 0o600) != nil {
		os.Exit(8)
	}
	server := gomcp.NewServer(&gomcp.Implementation{Name: "gateway-lifetime", Version: "1"}, nil)
	_ = server.Run(context.Background(), &gomcp.StdioTransport{}) //nolint:forbidigo // Standalone helper process, no testing context.
	os.Exit(0)
}

func TestGatewayFilesBelongToConnection(t *testing.T) {
	for _, cancelWait := range []bool{false, true} {
		t.Run(strconv.FormatBool(cancelWait), func(t *testing.T) {
			dir := redirectTempDir(t)
			marker := filepath.Join(dir, "started")
			policy := lifecycle.Policy{StartupTimeout: 100 * time.Millisecond}
			if cancelWait {
				policy.StartupTimeout = 0
			}
			ts, err := NewGatewayToolset(t.Context(), "gateway", "github", []gateway.Secret{{Name: "token", Env: "TOKEN"}}, nil, mapEnvProvider{"TOKEN": "secret"}, dir, policy)
			require.NoError(t, err)
			client := ts.mcpClient.(*stdioMCPClient)
			client.command = os.Args[0]
			client.env = append(os.Environ(), "MCP_GATEWAY_LIFETIME_HELPER="+marker)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- ts.Start(ctx) }()
			t.Cleanup(func() { _ = os.WriteFile(marker+".release", nil, 0o600); _ = ts.Stop(t.Context()) })
			require.Eventually(t, func() bool { _, err := os.Stat(marker); return err == nil }, 5*time.Second, time.Millisecond)
			if cancelWait {
				cancel()
			}
			select {
			case err = <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("Start wait did not return")
			}
			if cancelWait {
				require.ErrorIs(t, err, context.Canceled)
			} else {
				require.ErrorIs(t, err, lifecycle.ErrInitTimeout)
			}
			config, err := os.ReadFile(marker)
			require.NoError(t, err)
			require.FileExists(t, string(config), "files must outlive the canceled/timed-out wait")
			require.NoError(t, os.WriteFile(marker+".release", nil, 0o600))
			require.Eventually(t, func() bool { _, err := os.Stat(marker + ".read"); return err == nil }, 5*time.Second, time.Millisecond)
			if !cancelWait {
				require.NoError(t, ts.Start(t.Context()))
				require.FileExists(t, string(config))
			}
			require.NoError(t, ts.Stop(t.Context()))
			require.Eventually(t, func() bool { _, err := os.Stat(string(config)); return os.IsNotExist(err) }, 5*time.Second, time.Millisecond)
			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			for _, entry := range entries {
				assert.False(t, strings.HasPrefix(entry.Name(), secretsFilePrefix))
			}
		})
	}
}
