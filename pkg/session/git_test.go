package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsGitRepo(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, ".git"), 0o755))

	assert.True(t, isGitRepo(dir))
}

func TestIsGitRepoParent(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(parent, ".git"), 0o755))
	child := filepath.Join(parent, "child")
	require.NoError(t, os.Mkdir(child, 0o755))

	assert.True(t, isGitRepo(child))
}

func TestInvalidGitFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".git"), nil, 0o644))

	assert.False(t, isGitRepo(dir))
}

func TestIsNotGitRepo(t *testing.T) {
	tests := []struct {
		name      string
		setupFunc func() string
	}{
		{
			name: "no git",
			setupFunc: func() string {
				return t.TempDir()
			},
		},
		{
			name: "nonexistent directory",
			setupFunc: func() string {
				return "/path/that/does/not/exist"
			},
		},
		{
			name: "empty path",
			setupFunc: func() string {
				return ""
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := tt.setupFunc()
			assert.False(t, isGitRepo(dir))
		})
	}
}
