package teamloader

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/model/provider/dmr"
	"github.com/docker/cagent/pkg/tools"
)

// skipExamples contains example files that require cloud-specific configurations
// (e.g., AWS profiles, GCP credentials) that can't be mocked with dummy env vars.
var skipExamples = map[string]string{
	"pr-reviewer-bedrock.yaml": "requires AWS profile configuration",
}

func collectExamples(t *testing.T) []string {
	t.Helper()

	var files []string
	err := filepath.WalkDir(filepath.Join("..", "..", "examples"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && filepath.Ext(path) == ".yaml" {
			if reason, skip := skipExamples[filepath.Base(path)]; skip {
				t.Logf("Skipping %s: %s", path, reason)
				return nil
			}
			files = append(files, path)
		}
		return nil
	})
	require.NoError(t, err)
	assert.NotEmpty(t, files)

	return files
}

type noEnvProvider struct{}

func (p *noEnvProvider) Get(context.Context, string) (string, bool) { return "", false }

func TestGetToolsForAgent_ContinuesOnCreateToolError(t *testing.T) {
	t.Parallel()

	// Agent with a bogus toolset type to force createTool error without network access
	a := &latest.AgentConfig{
		Instruction: "test",
		Toolsets:    []latest.Toolset{{Type: "does-not-exist"}},
	}

	runConfig := config.RuntimeConfig{
		EnvProviderForTests: &noEnvProvider{},
	}

	got, warnings := getToolsForAgent(t.Context(), a, ".", &runConfig, NewToolsetRegistry())

	require.Empty(t, got)
	require.NotEmpty(t, warnings)
	require.Contains(t, warnings[0], "toolset does-not-exist failed")
}

func TestLoadExamples(t *testing.T) {
	examples := collectExamples(t)

	// Collect required env vars from all examples by checking configs directly.
	// This avoids calling Load() twice for each example.
	missingEnvs := make(map[string]bool)
	for _, agentFilename := range examples {
		agentSource, err := config.Resolve(agentFilename)
		require.NoError(t, err)

		cfg, err := config.Load(t.Context(), agentSource)
		require.NoError(t, err)

		for _, env := range config.GatherEnvVarsForModels(cfg) {
			missingEnvs[env] = true
		}

		toolEnvs, _ := config.GatherEnvVarsForTools(t.Context(), cfg)
		for _, env := range toolEnvs {
			missingEnvs[env] = true
		}
	}

	for name := range missingEnvs {
		t.Setenv(name, "dummy")
	}

	runConfig := &config.RuntimeConfig{}

	type versioned struct {
		Version string `yaml:"version"`
	}

	// Load all the examples.
	// Note: don't use t.Parallel() to avoid SQLite lock contention when
	// multiple RAG examples share the same relative database paths (e.g., ./bm25.db).
	for _, agentFilename := range examples {
		t.Run(agentFilename, func(t *testing.T) {
			agentSource, err := config.Resolve(agentFilename)
			require.NoError(t, err)

			// First make sure it doesn't define a version
			data, err := agentSource.Read(t.Context())
			require.NoError(t, err)

			var v versioned
			err = yaml.Unmarshal(data, &v)
			require.NoError(t, err)
			require.Empty(t, v.Version, "example %s should not define a version", agentFilename)

			// Then make sure the config loads successfully
			teams, err := Load(t.Context(), agentSource, runConfig)
			if err != nil {
				if errors.Is(err, dmr.ErrNotInstalled) && filepath.Base(agentFilename) == "dmr.yaml" {
					t.Skip("Skipping DMR example: Docker Model Runner not installed")
				}
			}
			require.NoError(t, err)
			assert.NotEmpty(t, teams)
		})
	}
}

func TestLoadDefaultAgent(t *testing.T) {
	t.Parallel()

	agentSource, err := config.Resolve("../../pkg/config/default-agent.yaml")
	require.NoError(t, err)

	runConfig := &config.RuntimeConfig{
		EnvProviderForTests: environment.NewEnvListProvider([]string{
			"OPENAI_API_KEY=dummy",
		}),
	}

	teams, err := Load(t.Context(), agentSource, runConfig)
	require.NoError(t, err)
	require.NotEmpty(t, teams)
}

func TestOverrideModel(t *testing.T) {
	tests := []struct {
		overrides   []string
		expected    string
		expectedErr string
	}{
		{
			overrides: []string{"anthropic/claude-4-6"},
			expected:  "anthropic/claude-4-6",
		},
		{
			overrides: []string{"root=anthropic/claude-4-6"},
			expected:  "anthropic/claude-4-6",
		},
		{
			overrides:   []string{"missing=anthropic/claude-4-6"},
			expectedErr: "unknown agent 'missing'",
		},
	}

	t.Setenv("OPENAI_API_KEY", "asdf")
	t.Setenv("ANTHROPIC_API_KEY", "asdf")

	for _, test := range tests {
		t.Run(test.expected, func(t *testing.T) {
			t.Parallel()

			agentSource, err := config.Resolve("testdata/basic.yaml")
			require.NoError(t, err)

			team, err := Load(t.Context(), agentSource, &config.RuntimeConfig{}, WithModelOverrides(test.overrides))
			if test.expectedErr != "" {
				require.Contains(t, err.Error(), test.expectedErr)
			} else {
				require.NoError(t, err)
				rootAgent, err := team.Agent("root")
				require.NoError(t, err)
				require.Equal(t, test.expected, rootAgent.Model().ID())
			}
		})
	}
}

func TestToolsetInstructions(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "dummy")

	agentSource, err := config.Resolve("testdata/tool-instruction.yaml")
	require.NoError(t, err)

	team, err := Load(t.Context(), agentSource, &config.RuntimeConfig{})
	require.NoError(t, err)

	agent, err := team.Agent("root")
	require.NoError(t, err)

	toolsets := agent.ToolSets()
	require.Len(t, toolsets, 1)

	instructions := tools.GetInstructions(toolsets[0])
	expected := "Dummy fetch tool instruction"
	require.Equal(t, expected, instructions)
}

func TestAutoModelFallbackError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping docker CLI shim test on Windows")
	}

	tempDir := t.TempDir()
	dockerPath := filepath.Join(tempDir, "docker")
	script := "#!/bin/sh\n" +
		"printf 'unknown flag: --json\\n\\nUsage:  docker [OPTIONS] COMMAND [ARG...]\\n\\nRun '\\''docker --help'\\'' for more information\\n' >&2\n" +
		"exit 1\n"
	require.NoError(t, os.WriteFile(dockerPath, []byte(script), 0o755))

	t.Setenv("PATH", tempDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("MODEL_RUNNER_HOST", "")

	agentSource, err := config.Resolve("testdata/auto-model.yaml")
	require.NoError(t, err)

	// Use noEnvProvider to ensure no API keys are available,
	// so DMR is the only fallback option.
	runConfig := &config.RuntimeConfig{
		EnvProviderForTests: &noEnvProvider{},
	}

	_, err = Load(t.Context(), agentSource, runConfig)
	require.Error(t, err)

	var autoErr *config.ErrAutoModelFallback
	require.ErrorAs(t, err, &autoErr, "expected ErrAutoModelFallback when auto model selection fails")
}

func TestIsThinkingBudgetDisabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		budget   *latest.ThinkingBudget
		expected bool
	}{
		{"nil budget", nil, false},
		{"Tokens=0 (disabled)", &latest.ThinkingBudget{Tokens: 0}, true},
		{"Effort=none (disabled)", &latest.ThinkingBudget{Effort: "none"}, true},
		{"Tokens=8192 (enabled)", &latest.ThinkingBudget{Tokens: 8192}, false},
		{"Effort=medium (enabled)", &latest.ThinkingBudget{Effort: "medium"}, false},
		{"Effort=high (enabled)", &latest.ThinkingBudget{Effort: "high"}, false},
		{"Effort=low (enabled)", &latest.ThinkingBudget{Effort: "low"}, false},
		{"Tokens=-1 (dynamic)", &latest.ThinkingBudget{Tokens: -1}, false},
		{"Tokens=0 with Effort=medium", &latest.ThinkingBudget{Tokens: 0, Effort: "medium"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isThinkingBudgetDisabled(tt.budget)
			assert.Equal(t, tt.expected, got)
		})
	}
}
