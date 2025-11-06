package teamloader

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/docker/cagent/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBangCommandsIntegration(t *testing.T) {
	// Set a fake API key for testing
	t.Setenv("OPENAI_API_KEY", "fake-key-for-testing")

	t.Run("load config with bang commands enabled", func(t *testing.T) {
		yamlContent := `version: "2"
agents:
  root:
    model: openai/gpt-4o
    enable_bang_commands: true
    toolsets:
      - type: think
`
		// Create temp file
		tmpDir := t.TempDir()
		tmpFile := filepath.Join(tmpDir, "test.yaml")
		err := os.WriteFile(tmpFile, []byte(yamlContent), 0644)
		require.NoError(t, err)

		// Load team
		team, err := Load(context.Background(), tmpFile, config.RuntimeConfig{}, WithModelOverrides(nil))
		require.NoError(t, err)
		require.NotNil(t, team)

		// Get root agent
		agent, err := team.Agent("root")
		require.NoError(t, err)

		// Verify bang commands are enabled
		assert.True(t, agent.EnableBangCommands())
	})

	t.Run("load config with bang commands disabled", func(t *testing.T) {
		yamlContent := `version: "2"
agents:
  root:
    model: openai/gpt-4o
    enable_bang_commands: false
    toolsets:
      - type: think
`
		// Create temp file
		tmpDir := t.TempDir()
		tmpFile := filepath.Join(tmpDir, "test.yaml")
		err := os.WriteFile(tmpFile, []byte(yamlContent), 0644)
		require.NoError(t, err)

		// Load team
		team, err := Load(context.Background(), tmpFile, config.RuntimeConfig{}, WithModelOverrides(nil))
		require.NoError(t, err)
		require.NotNil(t, team)

		// Get root agent
		agent, err := team.Agent("root")
		require.NoError(t, err)

		// Verify bang commands are disabled
		assert.False(t, agent.EnableBangCommands())
	})

	t.Run("default is false when omitted", func(t *testing.T) {
		yamlContent := `version: "2"
agents:
  root:
    model: openai/gpt-4o
    toolsets:
      - type: think
`
		// Create temp file
		tmpDir := t.TempDir()
		tmpFile := filepath.Join(tmpDir, "test.yaml")
		err := os.WriteFile(tmpFile, []byte(yamlContent), 0644)
		require.NoError(t, err)

		// Load team
		team, err := Load(context.Background(), tmpFile, config.RuntimeConfig{}, WithModelOverrides(nil))
		require.NoError(t, err)
		require.NotNil(t, team)

		// Get root agent
		agent, err := team.Agent("root")
		require.NoError(t, err)

		// Verify bang commands default to false
		assert.False(t, agent.EnableBangCommands())
	})
}
