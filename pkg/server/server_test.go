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

	"github.com/docker/cagent/pkg/api"
	"github.com/docker/cagent/pkg/config"
	latest "github.com/docker/cagent/pkg/config/v2"
	"github.com/docker/cagent/pkg/session"
)

func TestServerTODO(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")
	ctx := t.Context()

	agentsDir := prepareAgentsDir(t, "pirate.yaml")
	lnPath := startServer(t, ctx, agentsDir)

	t.Run("list agents", func(t *testing.T) {
		buf := httpGET(t, ctx, lnPath, "/api/agents")

		var agents []any
		unmarshal(t, buf, &agents)

		assert.NotEmpty(t, agents)
	})

	t.Run("get agent (no extension)", func(t *testing.T) {
		buf := httpGET(t, ctx, lnPath, "/api/agents/pirate")

		var cfg latest.Config
		unmarshal(t, buf, &cfg)

		assert.NotEmpty(t, cfg.Version)
		require.NotEmpty(t, cfg.Agents)
		assert.Contains(t, cfg.Agents["root"].Instruction, "pirate")
	})

	t.Run("get agent", func(t *testing.T) {
		buf := httpGET(t, ctx, lnPath, "/api/agents/pirate.yaml")

		var cfg latest.Config
		unmarshal(t, buf, &cfg)

		assert.NotEmpty(t, cfg.Version)
		require.NotEmpty(t, cfg.Agents)
		assert.Contains(t, cfg.Agents["root"].Instruction, "pirate")
	})

	t.Run("get agent's yaml (no extension)", func(t *testing.T) {
		content := httpGET(t, ctx, lnPath, "/api/agents/pirate/yaml")
		assert.Contains(t, string(content), "pirate")
	})

	t.Run("get agent's yaml", func(t *testing.T) {
		content := httpGET(t, ctx, lnPath, "/api/agents/pirate.yaml/yaml")
		assert.Contains(t, string(content), "pirate")
	})

	t.Run("set agent's yaml", func(t *testing.T) {
		httpPUT(t, ctx, lnPath, "/api/agents/pirate.yaml/yaml", `version: "2"`)

		content := httpGET(t, ctx, lnPath, "/api/agents/pirate.yaml/yaml")
		assert.Equal(t, `version: "2"`, string(content))
	})

	t.Run("list sessions", func(t *testing.T) {
		buf := httpGET(t, ctx, lnPath, "/api/sessions")

		var sessions []api.SessionsResponse
		unmarshal(t, buf, &sessions)

		assert.Empty(t, sessions)
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

func httpGET(t *testing.T, ctx context.Context, socketPath, path string) []byte {
	t.Helper()
	return httpDo(t, ctx, http.MethodGet, socketPath, path, http.NoBody)
}

func httpPUT(t *testing.T, ctx context.Context, socketPath, path, payload string) {
	t.Helper()
	httpDo(t, ctx, http.MethodPut, socketPath, path, strings.NewReader(payload))
}

func httpDo(t *testing.T, ctx context.Context, method, socketPath, path string, payload io.Reader) []byte {
	t.Helper()

	req, err := http.NewRequestWithContext(ctx, method, "http://_"+path, payload)
	require.NoError(t, err)

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", strings.TrimPrefix(socketPath, "unix://"))
			},
		},
	}
	resp, err := client.Do(req)
	require.NoError(t, err)
	require.Less(t, resp.StatusCode, 400)
	defer resp.Body.Close()

	buf, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	return buf
}

func unmarshal(t *testing.T, buf []byte, v any) {
	t.Helper()
	err := json.Unmarshal(buf, &v)
	require.NoError(t, err)
}

type mockStore struct {
	session.Store
}

func (s mockStore) GetSessions(ctx context.Context) ([]*session.Session, error) {
	return nil, nil
}
