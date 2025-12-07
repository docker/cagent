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
	"github.com/docker/cagent/pkg/session"
)

func TestServer_ListAgents(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")
	t.Setenv("ANTHROPIC_API_KEY", "dummy")

	ctx := t.Context()
	lnPath := startServer(t, ctx, prepareAgentsDir(t, "contradict.yaml", "multi_agents.yaml", "pirate.yaml"))

	buf := httpGET(t, ctx, lnPath, "/api/agents")

	var agents []api.Agent
	unmarshal(t, buf, &agents)

	assert.Len(t, agents, 3)

	assert.Contains(t, agents[0].Name, "contradict.yaml")
	assert.Equal(t, "Contrarian viewpoint provider", agents[0].Description)
	assert.False(t, agents[0].Multi)

	assert.Contains(t, agents[1].Name, "multi_agents.yaml")
	assert.Equal(t, "Multi Agent", agents[1].Description)
	assert.True(t, agents[1].Multi)

	assert.Contains(t, agents[2].Name, "pirate.yaml")
	assert.Equal(t, "Talk like a pirate", agents[2].Description)
	assert.False(t, agents[2].Multi)
}

func TestServer_EmptyList(t *testing.T) {
	ctx := t.Context()
	lnPath := startServer(t, ctx, prepareAgentsDir(t))

	buf := httpGET(t, ctx, lnPath, "/api/agents")
	assert.Equal(t, "[]\n", string(buf)) // We don't want null, but an empty array
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

func TestServer_WildcardAgentRouting(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")
	t.Setenv("ANTHROPIC_API_KEY", "dummy")

	ctx := t.Context()

	// Create test agent files
	agentsDir := filepath.Join(t.TempDir(), "agents")
	err := os.MkdirAll(agentsDir, 0o700)
	require.NoError(t, err)

	// Copy test files
	testFiles := []string{"pirate.yaml", "multi_agents.yaml", "contradict.yaml"}
	for _, file := range testFiles {
		buf, err := os.ReadFile(filepath.Join("testdata", file))
		require.NoError(t, err)
		err = os.WriteFile(filepath.Join(agentsDir, file), buf, 0o600)
		require.NoError(t, err)
	}

	// Manually create sources with keys that contain slashes to test wildcard routing
	store := &mockStore{}
	runConfig := config.RuntimeConfig{}

	sources := make(config.Sources)
	sources["pirate.yaml"] = config.NewFileSource(filepath.Join(agentsDir, "pirate.yaml"))
	sources["teams/multi.yaml"] = config.NewFileSource(filepath.Join(agentsDir, "multi_agents.yaml"))
	sources["deep/nested/path/contradict.yaml"] = config.NewFileSource(filepath.Join(agentsDir, "contradict.yaml"))

	srv, err := New(store, &runConfig, sources)
	require.NoError(t, err)

	socketPath := "unix://" + filepath.Join(t.TempDir(), "sock")
	ln, err := Listen(ctx, socketPath)
	require.NoError(t, err)
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	go func() {
		_ = srv.Serve(ctx, ln)
	}()
	lnPath := socketPath

	// Verify agents are available
	buf := httpGET(t, ctx, lnPath, "/api/agents")
	var agents []api.Agent
	unmarshal(t, buf, &agents)
	require.Len(t, agents, 3, "Expected 3 agents to be available")

	// Test various wildcard routing patterns
	tests := []struct {
		name        string
		agentPath   string
		expectError bool
	}{
		{
			name:        "simple agent path",
			agentPath:   "pirate.yaml",
			expectError: false,
		},
		{
			name:        "agent path with single slash",
			agentPath:   "teams/multi.yaml",
			expectError: false,
		},
		{
			name:        "agent path with multiple slashes",
			agentPath:   "deep/nested/path/contradict.yaml",
			expectError: false,
		},
		{
			name:        "simple agent path with leading slash",
			agentPath:   "/pirate.yaml",
			expectError: false,
		},
		{
			name:        "nested agent path with leading slash",
			agentPath:   "/teams/multi.yaml",
			expectError: false,
		},
		{
			name:        "agent path with agent name",
			agentPath:   "pirate.yaml/root",
			expectError: false,
		},
		{
			name:        "nested agent path with agent name",
			agentPath:   "teams/multi.yaml/root",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test session
			payload := session.Session{
				WorkingDir: t.TempDir(),
			}
			sessionBuf := httpDo(t, ctx, http.MethodPost, lnPath, "/api/sessions", payload)
			var sess session.Session
			unmarshal(t, sessionBuf, &sess)

			// Attempt to call the agent endpoint
			// Note: This will fail because we don't have a full runtime setup,
			// but it should at least validate that the route is matched and
			// basic parameter parsing works
			url := "/api/sessions/" + sess.ID + "/agent/" + strings.TrimPrefix(tt.agentPath, "/")

			// We expect this to fail in a specific way (not a 404 route error)
			// A 404 would indicate the route wasn't matched
			// Other errors are expected due to missing runtime components
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://_"+url, strings.NewReader(`[{"content":"test"}]`))
			require.NoError(t, err)
			req.Header.Set("Content-Type", "application/json")

			client := &http.Client{
				Transport: &http.Transport{
					DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
						var d net.Dialer
						return d.DialContext(ctx, "unix", strings.TrimPrefix(lnPath, "unix://"))
					},
				},
			}
			resp, err := client.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)

			// The route should be matched (not a 404)
			// It may fail with 500 due to runtime setup, but that's okay for this test
			// We're mainly testing that the wildcard routing works
			if resp.StatusCode == http.StatusNotFound {
				t.Errorf("Route not matched for path %s, got 404. Body: %s", tt.agentPath, string(body))
			}
		})
	}
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

	store := &mockStore{}
	runConfig := config.RuntimeConfig{}

	sources, err := config.ResolveSources(agentsDir)
	require.NoError(t, err)
	srv, err := New(store, &runConfig, sources)
	require.NoError(t, err)

	socketPath := "unix://" + filepath.Join(t.TempDir(), "sock")
	ln, err := Listen(ctx, socketPath)
	require.NoError(t, err)
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	go func() {
		_ = srv.Serve(ctx, ln)
	}()

	return socketPath
}

func httpGET(t *testing.T, ctx context.Context, socketPath, path string) []byte {
	t.Helper()
	return httpDo(t, ctx, http.MethodGet, socketPath, path, nil)
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
	sessions map[string]*session.Session
}

func (s *mockStore) init() {
	if s.sessions == nil {
		s.sessions = make(map[string]*session.Session)
	}
}

func (s *mockStore) AddSession(_ context.Context, sess *session.Session) error {
	s.init()
	s.sessions[sess.ID] = sess
	return nil
}

func (s *mockStore) GetSession(_ context.Context, id string) (*session.Session, error) {
	s.init()
	sess, ok := s.sessions[id]
	if !ok {
		return nil, session.ErrNotFound
	}
	return sess, nil
}

func (s *mockStore) GetSessions(_ context.Context) ([]*session.Session, error) {
	s.init()
	var sessions []*session.Session
	for _, sess := range s.sessions {
		sessions = append(sessions, sess)
	}
	return sessions, nil
}

func (s *mockStore) DeleteSession(_ context.Context, id string) error {
	s.init()
	delete(s.sessions, id)
	return nil
}

func (s *mockStore) UpdateSession(_ context.Context, sess *session.Session) error {
	s.init()
	s.sessions[sess.ID] = sess
	return nil
}
