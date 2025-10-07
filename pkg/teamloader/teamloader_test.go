package teamloader

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/config"
	latest "github.com/docker/cagent/pkg/config/v2"
	"github.com/docker/cagent/pkg/environment"
)

type noEnvProvider struct{}

func (p *noEnvProvider) Get(context.Context, string) string { return "" }

func TestCheckRequiredEnvVars(t *testing.T) {
	tests := []struct {
		yaml            string
		expectedMissing []string
	}{
		{
			yaml:            "openai_inline.yaml",
			expectedMissing: []string{"OPENAI_API_KEY"},
		},
		{
			yaml:            "anthropic_inline.yaml",
			expectedMissing: []string{"ANTHROPIC_API_KEY"},
		},
		{
			yaml:            "google_inline.yaml",
			expectedMissing: []string{"GOOGLE_API_KEY"},
		},
		{
			yaml:            "dmr_inline.yaml",
			expectedMissing: []string{},
		},
		{
			yaml:            "openai_model.yaml",
			expectedMissing: []string{"OPENAI_API_KEY"},
		},
		{
			yaml:            "anthropic_model.yaml",
			expectedMissing: []string{"ANTHROPIC_API_KEY"},
		},
		{
			yaml:            "google_model.yaml",
			expectedMissing: []string{"GOOGLE_API_KEY"},
		},
		{
			yaml:            "dmr_model.yaml",
			expectedMissing: []string{},
		},
		{
			yaml:            "all.yaml",
			expectedMissing: []string{"ANTHROPIC_API_KEY", "GOOGLE_API_KEY", "OPENAI_API_KEY"},
		},
	}
	for _, test := range tests {
		t.Run(test.yaml, func(t *testing.T) {
			t.Parallel()

			root := openRoot(t, "testdata")

			cfg, err := config.LoadConfig(test.yaml, root)
			require.NoError(t, err)

			err = checkRequiredEnvVars(t.Context(), cfg, &noEnvProvider{}, config.RuntimeConfig{})

			if len(test.expectedMissing) == 0 {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Equal(t, test.expectedMissing, err.(*environment.RequiredEnvError).Missing)
			}
		})
	}
}

func TestCheckRequiredEnvVarsWithModelGateway(t *testing.T) {
	t.Parallel()

	root := openRoot(t, "testdata")

	cfg, err := config.LoadConfig("all.yaml", root)
	require.NoError(t, err)

	err = checkRequiredEnvVars(t.Context(), cfg, &noEnvProvider{}, config.RuntimeConfig{
		ModelsGateway: "gateway:8080",
	})

	require.NoError(t, err)
}

func TestLoadExamples(t *testing.T) {
	// Collect the missing env vars.
	missingEnvs := map[string]bool{}

	var runtimeConfig config.RuntimeConfig

	for _, file := range collectExamples(t) {
		t.Run(file, func(t *testing.T) {
			_, err := Load(t.Context(), file, runtimeConfig)
			if err != nil {
				envErr := &environment.RequiredEnvError{}
				require.ErrorAs(t, err, &envErr)

				for _, env := range envErr.Missing {
					missingEnvs[env] = true
				}
			}
		})
	}

	for name := range missingEnvs {
		t.Setenv(name, "dummy")
	}

	// Load all the examples.
	for _, file := range collectExamples(t) {
		t.Run(file, func(t *testing.T) {
			t.Parallel()

			teams, err := Load(t.Context(), file, runtimeConfig)
			require.NoError(t, err)
			require.NotEmpty(t, teams)
		})
	}
}

func openRoot(t *testing.T, dir string) *os.Root {
	t.Helper()

	root, err := os.OpenRoot(dir)
	require.NoError(t, err)
	t.Cleanup(func() { root.Close() })

	return root
}

func collectExamples(t *testing.T) []string {
	t.Helper()

	var files []string
	err := filepath.WalkDir(filepath.Join("..", "..", "examples"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && filepath.Ext(path) == ".yaml" {
			files = append(files, path)
		}
		return nil
	})
	require.NoError(t, err)
	assert.NotEmpty(t, files)

	return files
}

func TestApplyModelOverrides(t *testing.T) {
	tests := []struct {
		name        string
		agents      map[string]latest.AgentConfig
		overrides   []string
		expected    map[string]string // agent name -> expected model
		expectError bool
		errorMsg    string
	}{
		{
			name: "global override",
			agents: map[string]latest.AgentConfig{
				"root":  {Model: "openai/gpt-4"},
				"other": {Model: "anthropic/claude-3"},
			},
			overrides: []string{"google/gemini-pro"},
			expected: map[string]string{
				"root":  "google/gemini-pro",
				"other": "google/gemini-pro",
			},
		},
		{
			name: "single per-agent override",
			agents: map[string]latest.AgentConfig{
				"root":  {Model: "openai/gpt-4"},
				"other": {Model: "anthropic/claude-3"},
			},
			overrides: []string{"other=google/gemini-pro"},
			expected: map[string]string{
				"root":  "openai/gpt-4",
				"other": "google/gemini-pro",
			},
		},
		{
			name: "multiple separate flags",
			agents: map[string]latest.AgentConfig{
				"root":  {Model: "openai/gpt-4"},
				"other": {Model: "anthropic/claude-3"},
			},
			overrides: []string{"root=openai/gpt-5", "other=anthropic/claude-sonnet-4-0"},
			expected: map[string]string{
				"root":  "openai/gpt-5",
				"other": "anthropic/claude-sonnet-4-0",
			},
		},
		{
			name: "comma-separated format",
			agents: map[string]latest.AgentConfig{
				"root":  {Model: "openai/gpt-4"},
				"other": {Model: "anthropic/claude-3"},
				"third": {Model: "google/gemini-pro"},
			},
			overrides: []string{"root=openai/gpt-5,other=anthropic/claude-sonnet-4-0"},
			expected: map[string]string{
				"root":  "openai/gpt-5",
				"other": "anthropic/claude-sonnet-4-0",
				"third": "google/gemini-pro",
			},
		},
		{
			name: "mixed formats",
			agents: map[string]latest.AgentConfig{
				"root":     {Model: "openai/gpt-4"},
				"other":    {Model: "anthropic/claude-3"},
				"third":    {Model: "google/gemini-pro"},
				"reviewer": {Model: "openai/gpt-3.5-turbo"},
			},
			overrides: []string{"root=openai/gpt-5,other=anthropic/claude-4", "reviewer=google/gemini-1.5-pro"},
			expected: map[string]string{
				"root":     "openai/gpt-5",
				"other":    "anthropic/claude-4",
				"third":    "google/gemini-pro",
				"reviewer": "google/gemini-1.5-pro",
			},
		},
		{
			name: "last override wins",
			agents: map[string]latest.AgentConfig{
				"root": {Model: "openai/gpt-4"},
			},
			overrides: []string{"root=openai/gpt-5", "root=anthropic/claude-4"},
			expected: map[string]string{
				"root": "anthropic/claude-4",
			},
		},
		{
			name: "unknown agent error",
			agents: map[string]latest.AgentConfig{
				"root": {Model: "openai/gpt-4"},
			},
			overrides:   []string{"nonexistent=openai/gpt-5"},
			expectError: true,
			errorMsg:    "unknown agent 'nonexistent'",
		},
		{
			name: "empty model spec error",
			agents: map[string]latest.AgentConfig{
				"root": {Model: "openai/gpt-4"},
			},
			overrides:   []string{"root="},
			expectError: true,
			errorMsg:    "empty model specification in override: root=",
		},
		{
			name: "empty global model spec is skipped",
			agents: map[string]latest.AgentConfig{
				"root": {Model: "openai/gpt-4"},
			},
			overrides: []string{""},
			expected: map[string]string{
				"root": "openai/gpt-4",
			},
		},
		{
			name: "whitespace handling",
			agents: map[string]latest.AgentConfig{
				"root":  {Model: "openai/gpt-4"},
				"other": {Model: "anthropic/claude-3"},
			},
			overrides: []string{" root = openai/gpt-5 , other = anthropic/claude-4 "},
			expected: map[string]string{
				"root":  "openai/gpt-5",
				"other": "anthropic/claude-4",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := &latest.Config{
				Agents: tt.agents,
				Models: make(map[string]latest.ModelConfig),
			}

			err := config.ApplyModelOverrides(cfg, tt.overrides)

			if tt.expectError {
				require.ErrorContains(t, err, tt.errorMsg)
			} else {
				require.NoError(t, err)
				for agentName, expectedModel := range tt.expected {
					assert.Equal(t, expectedModel, cfg.Agents[agentName].Model)
				}
			}
		})
	}
}
