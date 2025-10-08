package server

import (
	"bytes"
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

func TestServer_ListAgents(t *testing.T) {
	// t.Parallel()
	t.Setenv("OPENAI_API_KEY", "dummy")
	t.Setenv("ANTHROPIC_API_KEY", "dummy")

	ctx := t.Context()
	lnPath := startServer(t, ctx, prepareAgentsDir(t, "pirate.yaml", "contradict.yaml", "multi_agents.yaml"))

	buf := httpGET(t, ctx, lnPath, "/api/agents")

	var agents []api.Agent
	unmarshal(t, buf, &agents)

	assert.Len(t, agents, 3)

	assert.Equal(t, "contradict.yaml", agents[0].Name)
	assert.Equal(t, "Contrarian viewpoint provider", agents[0].Description)
	assert.False(t, agents[0].Multi)

	assert.Equal(t, "multi_agents.yaml", agents[1].Name)
	assert.Equal(t, "Multi Agent", agents[1].Description)
	assert.True(t, agents[1].Multi)

	assert.Equal(t, "pirate.yaml", agents[2].Name)
	assert.Equal(t, "Talk like a pirate", agents[2].Description)
	assert.False(t, agents[2].Multi)
}

func TestServer_GetAgent_NoExtension(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	lnPath := startServer(t, ctx, prepareAgentsDir(t, "pirate.yaml"))

	buf := httpGET(t, ctx, lnPath, "/api/agents/pirate")

	var cfg latest.Config
	unmarshal(t, buf, &cfg)

	assert.NotEmpty(t, cfg.Version)
	require.Len(t, cfg.Agents, 1)
	assert.Contains(t, cfg.Agents["root"].Instruction, "pirate")
}

func TestServer_GetAgent(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	lnPath := startServer(t, ctx, prepareAgentsDir(t, "pirate.yaml"))

	buf := httpGET(t, ctx, lnPath, "/api/agents/pirate.yaml")

	var cfg latest.Config
	unmarshal(t, buf, &cfg)

	assert.NotEmpty(t, cfg.Version)
	require.Len(t, cfg.Agents, 1)
	assert.Contains(t, cfg.Agents["root"].Instruction, "pirate")
}

func TestServer_GetSetYaml_NoExtension(t *testing.T) {
	// t.Parallel()
	t.Setenv("OPENAI_API_KEY", "dummy")

	ctx := t.Context()
	lnPath := startServer(t, ctx, prepareAgentsDir(t, "pirate.yaml"))

	url := "/api/agents/pirate/yaml"
	origContent := httpGET(t, ctx, lnPath, url)
	assert.Contains(t, string(origContent), "pirate")

	httpPUT(t, ctx, lnPath, url, origContent)
	assert.Equal(t, origContent, httpGET(t, ctx, lnPath, url))

	httpPUT(t, ctx, lnPath, url, []byte(`version: "2"`))
	assert.Equal(t, []byte(`version: "2"`), httpGET(t, ctx, lnPath, url))
}

func TestServer_GetSetYaml(t *testing.T) {
	// t.Parallel()
	t.Setenv("OPENAI_API_KEY", "dummy")

	ctx := t.Context()
	lnPath := startServer(t, ctx, prepareAgentsDir(t, "pirate.yaml"))

	url := "/api/agents/pirate.yaml/yaml"
	origContent := httpGET(t, ctx, lnPath, url)
	assert.Contains(t, string(origContent), "pirate")

	httpPUT(t, ctx, lnPath, url, origContent)
	assert.Equal(t, origContent, httpGET(t, ctx, lnPath, url))

	httpPUT(t, ctx, lnPath, url, []byte(`version: "2"`))
	assert.Equal(t, []byte(`version: "2"`), httpGET(t, ctx, lnPath, url))
}

func TestServer_Edit_Noop(t *testing.T) {
	// t.Parallel()
	t.Setenv("OPENAI_API_KEY", "dummy")

	ctx := t.Context()
	lnPath := startServer(t, ctx, prepareAgentsDir(t, "pirate.yaml"))

	edit := api.EditAgentConfigRequest{
		Filename:    "pirate.yaml",
		AgentConfig: latest.Config{},
	}
	httpPUT(t, ctx, lnPath, "/api/agents/config", edit)

	buf := httpGET(t, ctx, lnPath, "/api/agents/pirate.yaml")
	var cfg latest.Config
	unmarshal(t, buf, &cfg)
	assert.NotEmpty(t, cfg.Version)
	require.Len(t, cfg.Agents, 1)
	assert.Contains(t, cfg.Agents["root"].Instruction, "pirate")
}

func TestServer_Edit_Instruction(t *testing.T) {
	// t.Parallel()
	t.Setenv("OPENAI_API_KEY", "dummy")

	ctx := t.Context()
	lnPath := startServer(t, ctx, prepareAgentsDir(t, "pirate.yaml"))

	edit := api.EditAgentConfigRequest{
		Filename: "pirate.yaml",
		AgentConfig: latest.Config{
			Agents: map[string]latest.AgentConfig{
				"root": {
					Instruction: "New Instructions",
					Model:       "openai/gpt-4o",
				},
			},
		},
	}
	httpPUT(t, ctx, lnPath, "/api/agents/config", edit)

	buf := httpGET(t, ctx, lnPath, "/api/agents/pirate.yaml")
	var cfg latest.Config
	unmarshal(t, buf, &cfg)
	assert.NotEmpty(t, cfg.Version)
	require.Len(t, cfg.Agents, 1)
	require.Len(t, cfg.Models, 1)
	assert.Equal(t, "New Instructions", cfg.Agents["root"].Instruction)
	assert.Empty(t, cfg.Agents["root"].Description)
}

func TestServer_Edit_OnlyOneSubAgent(t *testing.T) {
	// t.Parallel()
	t.Setenv("OPENAI_API_KEY", "dummy")
	t.Setenv("ANTHROPIC_API_KEY", "dummy")

	ctx := t.Context()
	lnPath := startServer(t, ctx, prepareAgentsDir(t, "multi_agents.yaml"))

	edit := api.EditAgentConfigRequest{
		Filename: "multi_agents.yaml",
		AgentConfig: latest.Config{
			Agents: map[string]latest.AgentConfig{
				"root": {
					Instruction: "New Instructions",
					Model:       "openai/gpt-4o",
				},
			},
		},
	}
	httpPUT(t, ctx, lnPath, "/api/agents/config", edit)

	buf := httpGET(t, ctx, lnPath, "/api/agents/multi_agents.yaml")
	var cfg latest.Config
	unmarshal(t, buf, &cfg)
	assert.NotEmpty(t, cfg.Version)
	require.Len(t, cfg.Agents, 3)
	require.Len(t, cfg.Models, 2)
	assert.Equal(t, "New Instructions", cfg.Agents["root"].Instruction)
	assert.Empty(t, cfg.Agents["root"].Description)
	assert.Equal(t, "Pirate", cfg.Agents["pirate"].Description)
	assert.NotEmpty(t, cfg.Agents["pirate"].Instruction)
	assert.Equal(t, "Contradict", cfg.Agents["contradict"].Description)
	assert.NotEmpty(t, cfg.Agents["contradict"].Instruction)
}

func TestServer_ListSessions(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	lnPath := startServer(t, ctx, prepareAgentsDir(t, "pirate.yaml"))

	buf := httpGET(t, ctx, lnPath, "/api/sessions")

	var sessions []api.SessionsResponse
	unmarshal(t, buf, &sessions)

	assert.Empty(t, sessions)
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

	socketPath := "unix://" + filepath.Join(t.TempDir(), "sock")
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
	return httpDo(t, ctx, http.MethodGet, socketPath, path, nil)
}

func httpPUT(t *testing.T, ctx context.Context, socketPath, path string, payload any) {
	t.Helper()
	httpDo(t, ctx, http.MethodPut, socketPath, path, payload)
}

func httpDo(t *testing.T, ctx context.Context, method, socketPath, path string, payload any) []byte {
	t.Helper()

	var (
		body        io.Reader
		contentType string
	)
	switch v := payload.(type) {
	case nil:
		body = http.NoBody
	case []byte:
		body = bytes.NewReader(v)
	case string:
		body = strings.NewReader(v)
	default:
		buf, err := json.Marshal(payload)
		require.NoError(t, err)
		body = bytes.NewReader(buf)
		contentType = "application/json"
	}

	req, err := http.NewRequestWithContext(ctx, method, "http://_"+path, body)
	require.NoError(t, err)

	req.Header.Set("Content-Type", contentType)

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
	defer resp.Body.Close()

	buf, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	require.Less(t, resp.StatusCode, 400, string(buf))
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
