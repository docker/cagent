package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAutoRegisterModels(t *testing.T) {
	t.Parallel()

	root := openRoot(t, "testdata")

	cfg, err := LoadConfig("autoregister.yaml", root)
	require.NoError(t, err)

	assert.Len(t, cfg.Models, 2)
	assert.Equal(t, "openai", cfg.Models["openai/gpt-4o"].Provider)
	assert.Equal(t, "gpt-4o", cfg.Models["openai/gpt-4o"].Model)
	assert.Equal(t, "anthropic", cfg.Models["anthropic/claude-sonnet-4-0"].Provider)
	assert.Equal(t, "claude-sonnet-4-0", cfg.Models["anthropic/claude-sonnet-4-0"].Model)
}

func TestAutoRegisterAlloy(t *testing.T) {
	t.Parallel()

	root := openRoot(t, "testdata")

	cfg, err := LoadConfig("autoregister_alloy.yaml", root)
	require.NoError(t, err)

	assert.Len(t, cfg.Models, 2)
	assert.Equal(t, "openai", cfg.Models["openai/gpt-4o"].Provider)
	assert.Equal(t, "gpt-4o", cfg.Models["openai/gpt-4o"].Model)
	assert.Equal(t, "anthropic", cfg.Models["anthropic/claude-sonnet-4-0"].Provider)
	assert.Equal(t, "claude-sonnet-4-0", cfg.Models["anthropic/claude-sonnet-4-0"].Model)
}

func TestMigrate_v0_v1_provider(t *testing.T) {
	t.Parallel()

	root := openRoot(t, "testdata")

	cfg, err := LoadConfig("provider_v0.yaml", root)
	require.NoError(t, err)

	assert.Equal(t, "openai", cfg.Models["openai"].Provider)
}

func TestMigrate_v1_provider(t *testing.T) {
	t.Parallel()

	root := openRoot(t, "testdata")

	cfg, err := LoadConfig("provider_v1.yaml", root)
	require.NoError(t, err)

	assert.Equal(t, "openai", cfg.Models["openai"].Provider)
}

func TestMigrate_v0_v1_todo(t *testing.T) {
	t.Parallel()

	root := openRoot(t, "testdata")

	cfg, err := LoadConfig("todo_v0.yaml", root)
	require.NoError(t, err)

	assert.Len(t, cfg.Agents["root"].Toolsets, 2)
	assert.Equal(t, "todo", cfg.Agents["root"].Toolsets[0].Type)
	assert.False(t, cfg.Agents["root"].Toolsets[0].Shared)
	assert.Equal(t, "mcp", cfg.Agents["root"].Toolsets[1].Type)
}

func TestMigrate_v1_todo(t *testing.T) {
	t.Parallel()

	root := openRoot(t, "testdata")

	cfg, err := LoadConfig("todo_v1.yaml", root)
	require.NoError(t, err)

	assert.Len(t, cfg.Agents["root"].Toolsets, 2)
	assert.Equal(t, "todo", cfg.Agents["root"].Toolsets[0].Type)
	assert.False(t, cfg.Agents["root"].Toolsets[0].Shared)
	assert.Equal(t, "mcp", cfg.Agents["root"].Toolsets[1].Type)
}

func TestMigrate_v0_v1_shared_todo(t *testing.T) {
	t.Parallel()

	root := openRoot(t, "testdata")

	cfg, err := LoadConfig("shared_todo_v0.yaml", root)
	require.NoError(t, err)

	assert.Len(t, cfg.Agents["root"].Toolsets, 2)
	assert.Equal(t, "todo", cfg.Agents["root"].Toolsets[0].Type)
	assert.True(t, cfg.Agents["root"].Toolsets[0].Shared)
	assert.Equal(t, "mcp", cfg.Agents["root"].Toolsets[1].Type)
}

func TestMigrate_v1_shared_todo(t *testing.T) {
	t.Parallel()

	root := openRoot(t, "testdata")

	cfg, err := LoadConfig("shared_todo_v1.yaml", root)
	require.NoError(t, err)

	assert.Len(t, cfg.Agents["root"].Toolsets, 2)
	assert.Equal(t, "todo", cfg.Agents["root"].Toolsets[0].Type)
	assert.True(t, cfg.Agents["root"].Toolsets[0].Shared)
	assert.Equal(t, "mcp", cfg.Agents["root"].Toolsets[1].Type)
}

func TestMigrate_v0_v1_think(t *testing.T) {
	t.Parallel()

	root := openRoot(t, "testdata")

	cfg, err := LoadConfig("think_v0.yaml", root)
	require.NoError(t, err)

	assert.Len(t, cfg.Agents["root"].Toolsets, 2)
	assert.Equal(t, "think", cfg.Agents["root"].Toolsets[0].Type)
	assert.Equal(t, "mcp", cfg.Agents["root"].Toolsets[1].Type)
}

func TestMigrate_v1_think(t *testing.T) {
	t.Parallel()

	root := openRoot(t, "testdata")

	cfg, err := LoadConfig("think_v1.yaml", root)
	require.NoError(t, err)

	assert.Len(t, cfg.Agents["root"].Toolsets, 2)
	assert.Equal(t, "think", cfg.Agents["root"].Toolsets[0].Type)
	assert.Equal(t, "mcp", cfg.Agents["root"].Toolsets[1].Type)
}

func TestMigrate_v0_v1_memory(t *testing.T) {
	t.Parallel()

	root := openRoot(t, "testdata")

	cfg, err := LoadConfig("memory_v0.yaml", root)
	require.NoError(t, err)

	assert.Len(t, cfg.Agents["root"].Toolsets, 2)
	assert.Equal(t, "memory", cfg.Agents["root"].Toolsets[0].Type)
	assert.Equal(t, "dev_memory.db", cfg.Agents["root"].Toolsets[0].Path)
	assert.Equal(t, "mcp", cfg.Agents["root"].Toolsets[1].Type)
}

func TestMigrate_v1_memory(t *testing.T) {
	t.Parallel()

	root := openRoot(t, "testdata")

	cfg, err := LoadConfig("memory_v1.yaml", root)
	require.NoError(t, err)

	assert.Len(t, cfg.Agents["root"].Toolsets, 2)
	assert.Equal(t, "memory", cfg.Agents["root"].Toolsets[0].Type)
	assert.Equal(t, "dev_memory.db", cfg.Agents["root"].Toolsets[0].Path)
	assert.Equal(t, "mcp", cfg.Agents["root"].Toolsets[1].Type)
}

func TestMigrate_v1(t *testing.T) {
	t.Parallel()

	root := openRoot(t, "testdata")

	_, err := LoadConfig("v1.yaml", root)
	require.NoError(t, err)
}

func openRoot(t *testing.T, dir string) *os.Root {
	t.Helper()

	root, err := os.OpenRoot(dir)
	require.NoError(t, err)
	t.Cleanup(func() { root.Close() })

	return root
}
