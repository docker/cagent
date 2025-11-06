package v2

import (
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnableBangCommandsParsing(t *testing.T) {
	t.Run("parse enable_bang_commands true", func(t *testing.T) {
		yamlConfig := `
version: "2"
agents:
  root:
    model: openai/gpt-4o
    enable_bang_commands: true
`
		var cfg Config
		err := yaml.Unmarshal([]byte(yamlConfig), &cfg)
		require.NoError(t, err)
		assert.True(t, cfg.Agents["root"].EnableBangCommands)
	})

	t.Run("parse enable_bang_commands false", func(t *testing.T) {
		yamlConfig := `
version: "2"
agents:
  root:
    model: openai/gpt-4o
    enable_bang_commands: false
`
		var cfg Config
		err := yaml.Unmarshal([]byte(yamlConfig), &cfg)
		require.NoError(t, err)
		assert.False(t, cfg.Agents["root"].EnableBangCommands)
	})

	t.Run("default is false when omitted", func(t *testing.T) {
		yamlConfig := `
version: "2"
agents:
  root:
    model: openai/gpt-4o
`
		var cfg Config
		err := yaml.Unmarshal([]byte(yamlConfig), &cfg)
		require.NoError(t, err)
		assert.False(t, cfg.Agents["root"].EnableBangCommands)
	})
}
