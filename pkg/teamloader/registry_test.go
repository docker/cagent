package teamloader

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/tools"
)

type mockEnvProvider struct {
	values map[string]string
}

func (p *mockEnvProvider) Get(_ context.Context, key string) (string, bool) {
	v, ok := p.values[key]
	return v, ok
}

func TestExpandSandboxPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		sandbox       *latest.SandboxConfig
		envVars       map[string]string
		expectedPaths []string
	}{
		{
			name:          "nil sandbox",
			sandbox:       nil,
			expectedPaths: nil,
		},
		{
			name: "no interpolation needed",
			sandbox: &latest.SandboxConfig{
				Image: "alpine:latest",
				Paths: []string{".", "/tmp", "./config:ro"},
			},
			expectedPaths: []string{".", "/tmp", "./config:ro"},
		},
		{
			name: "simple env var interpolation",
			sandbox: &latest.SandboxConfig{
				Image: "alpine:latest",
				Paths: []string{"${env.HOME}:ro", "${env.PROJECT_DIR}"},
			},
			envVars: map[string]string{
				"HOME":        "/home/user",
				"PROJECT_DIR": "/projects/myapp",
			},
			expectedPaths: []string{"/home/user:ro", "/projects/myapp"},
		},
		{
			name: "mixed paths with and without interpolation",
			sandbox: &latest.SandboxConfig{
				Image: "custom:image",
				Paths: []string{".", "${env.HOME}:ro", "/tmp", "${env.CONFIG_PATH}:ro"},
			},
			envVars: map[string]string{
				"HOME":        "/home/testuser",
				"CONFIG_PATH": "/etc/myapp",
			},
			expectedPaths: []string{".", "/home/testuser:ro", "/tmp", "/etc/myapp:ro"},
		},
		{
			name: "default value fallback",
			sandbox: &latest.SandboxConfig{
				Image: "alpine:latest",
				Paths: []string{"${env.CUSTOM_PATH || '/default/path'}:ro"},
			},
			envVars:       map[string]string{},
			expectedPaths: []string{"/default/path:ro"},
		},
		{
			name: "missing env var without default",
			sandbox: &latest.SandboxConfig{
				Image: "alpine:latest",
				Paths: []string{"${env.MISSING_VAR}:ro"},
			},
			envVars:       map[string]string{},
			expectedPaths: []string{":ro"}, // Empty string when env var is missing
		},
		{
			name: "image field preserved",
			sandbox: &latest.SandboxConfig{
				Image: "myregistry/myimage:v1",
				Paths: []string{"${env.HOME}"},
			},
			envVars: map[string]string{
				"HOME": "/home/user",
			},
			expectedPaths: []string{"/home/user"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			envProvider := &mockEnvProvider{values: tt.envVars}
			result := expandSandboxPaths(t.Context(), tt.sandbox, envProvider)

			if tt.sandbox == nil {
				require.Nil(t, result)
				return
			}

			require.NotNil(t, result)
			assert.Equal(t, tt.sandbox.Image, result.Image, "Image should be preserved")
			assert.Equal(t, tt.expectedPaths, result.Paths, "Paths should be expanded correctly")
		})
	}
}

func TestCreateShellToolWithSandboxExpansion(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")
	t.Setenv("WORK_DIR", "/workspace")

	toolset := latest.Toolset{
		Type: "shell",
		Sandbox: &latest.SandboxConfig{
			Image: "alpine:latest",
			Paths: []string{".", "${env.HOME}:ro", "${env.WORK_DIR}"},
		},
	}

	registry := NewDefaultToolsetRegistry()

	runConfig := &config.RuntimeConfig{
		Config:              config.Config{WorkingDir: t.TempDir()},
		EnvProviderForTests: environment.NewOsEnvProvider(),
	}

	tool, err := registry.CreateTool(t.Context(), toolset, ".", runConfig)
	require.NoError(t, err)
	require.NotNil(t, tool)
}

type stopTrackingToolSet struct {
	tools.BaseToolSet
	stopped bool
	stopErr error
}

func (s *stopTrackingToolSet) Tools(context.Context) ([]tools.Tool, error) { return nil, nil }
func (s *stopTrackingToolSet) Stop(context.Context) error {
	s.stopped = true
	return s.stopErr
}

type closeTrackingCloser struct {
	closed bool
	err    error
}

func (c *closeTrackingCloser) Close() error {
	c.closed = true
	return c.err
}

func TestToolSetWithCloser_Stop_AlwaysCloses(t *testing.T) {
	t.Parallel()

	base := &stopTrackingToolSet{stopErr: errors.New("stop failed")}
	closer := &closeTrackingCloser{}

	ts := toolSetWithCloser{ToolSet: base, closer: closer}

	err := ts.Stop(t.Context())
	require.Error(t, err)

	assert.True(t, base.stopped)
	assert.True(t, closer.closed)

	// Ensure we return the stop error (and not swallow it)
	assert.ErrorContains(t, err, "stop failed")

	// Closer should be an io.Closer, as required
	var _ io.Closer = closer
}
