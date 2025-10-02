package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadPromptFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "agents.md"), []byte("content"), 0o644)
	require.NoError(t, err)

	additionalPrompt, err := readPromptFile(dir, "agents.md")
	require.NoError(t, err)
	assert.Equal(t, "content", additionalPrompt)
}

func TestReadPromptFileParent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "agents.md"), []byte("content"), 0o644)
	require.NoError(t, err)

	child := filepath.Join(dir, "child")
	err = os.Mkdir(child, 0o755)
	require.NoError(t, err)

	additionalPrompt, err := readPromptFile(child, "agents.md")
	require.NoError(t, err)
	assert.Equal(t, "content", additionalPrompt)
}

func TestReadPromptFileReadFirst(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "agents.md"), []byte("parent"), 0o644)
	require.NoError(t, err)

	child := filepath.Join(dir, "child")
	err = os.Mkdir(child, 0o755)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(child, "agents.md"), []byte("child"), 0o644)
	require.NoError(t, err)

	additionalPrompt, err := readPromptFile(child, "agents.md")
	require.NoError(t, err)
	assert.Equal(t, "child", additionalPrompt)
}

func TestReadNoPromptFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	additionalPrompt, err := readPromptFile(dir, "agents.md")
	require.NoError(t, err)
	assert.Empty(t, additionalPrompt)
}
