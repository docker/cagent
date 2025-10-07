package server

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/config"
	latest "github.com/docker/cagent/pkg/config/v2"
	"github.com/docker/cagent/pkg/session"
)

func TestServerTODO(t *testing.T) {
	ctx := t.Context()

	agentsDir := prepareAgentsDir(t, "pirate.yaml")
	lnPath := startServer(t, ctx, agentsDir)

	t.Run("list agents", func(t *testing.T) {
		var agents []any
		httpGET(t, ctx, lnPath, "/api/agents", &agents)
		assert.NotEmpty(t, agents)
	})

	t.Run("get agent (no extension)", func(t *testing.T) {
		var cfg latest.Config
		httpGET(t, ctx, lnPath, "/api/agents/pirate", &cfg)
		assert.NotEmpty(t, cfg.Version)
		require.NotEmpty(t, cfg.Agents)
		assert.Contains(t, cfg.Agents["root"].Instruction, "pirate")
	})

	t.Run("get agent", func(t *testing.T) {
		var cfg latest.Config
		httpGET(t, ctx, lnPath, "/api/agents/pirate.yaml", &cfg)
		assert.NotEmpty(t, cfg.Version)
		require.NotEmpty(t, cfg.Agents)
		assert.Contains(t, cfg.Agents["root"].Instruction, "pirate")
	})
}

func prepareAgentsDir(t *testing.T, testFiles ...string) string {
	t.Helper()

	agentsDir := filepath.Join(t.TempDir(), "agents")
	err := os.MkdirAll(agentsDir, 0o700)
	require.NoError(t, err)

	for _, file := range testFiles {
		buf, err := os.ReadFile(filepath.Join("testdata", file))
		require.NoError(t, err)

		err = os.WriteFile(filepath.Join(agentsDir, filepath.Base(file)), buf, 0o600)
		require.NoError(t, err)
	}

	return agentsDir
}

func startServer(t *testing.T, ctx context.Context, agentsDir string) string {
	t.Helper()

	var store mockStore
	var runConfig config.RuntimeConfig

	srv, err := New(store, runConfig, nil, WithAgentsDir(agentsDir))
	require.NoError(t, err)

	socketPath := "unix://" + filepath.Join(t.TempDir(), "test.sock")
	ln, err := Listen(ctx, socketPath)
	require.NoError(t, err)
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	go srv.Serve(ctx, ln)

	return socketPath
}

func httpGET(t *testing.T, ctx context.Context, socketPath, path string, v any) {
	t.Helper()

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", strings.TrimPrefix(socketPath, "unix://"))
			},
		},
	}

	url := "http://_" + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	buf, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	err = json.Unmarshal(buf, &v)
	require.NoError(t, err)
}

type mockStore struct {
	session.Store
}
