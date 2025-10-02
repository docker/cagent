package session

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetEnvironmentInfo(t *testing.T) {
	tests := []struct {
		name      string
		setupFunc func() string
		expectGit bool
	}{
		{
			name: "with git repo",
			setupFunc: func() string {
				tmpDir := t.TempDir()
				require.NoError(t, os.Mkdir(filepath.Join(tmpDir, ".git"), 0o755))
				return tmpDir
			},
			expectGit: true,
		},
		{
			name: "without git repo",
			setupFunc: func() string {
				return t.TempDir()
			},
			expectGit: false,
		},
		{
			name: "nonexistent directory",
			setupFunc: func() string {
				return "/path/that/does/not/exist"
			},
			expectGit: false,
		},
		{
			name: "empty directory path",
			setupFunc: func() string {
				return ""
			},
			expectGit: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := tt.setupFunc()
			info := getEnvironmentInfo(dir)

			gitStatus := "No"
			if tt.expectGit {
				gitStatus = "Yes"
			}

			expected := `Here is useful information about the environment you are running in:
	<env>
	Working directory: ` + dir + `
	Is directory a git repo: ` + gitStatus + `
	Platform: ` + runtime.GOOS + `
	</env>`

			assert.Equal(t, expected, info)
		})
	}
}

func TestBoolToYesNo(t *testing.T) {
	assert.Equal(t, "Yes", boolToYesNo(true))
	assert.Equal(t, "No", boolToYesNo(false))
}

func TestGetEnvironmentInfoIntegration(t *testing.T) {
	wd, err := os.Getwd()
	require.NoError(t, err)

	info := getEnvironmentInfo(wd)

	assert.Contains(t, info, "Here is useful information about the environment you are running in:")
	assert.Contains(t, info, "<env>")
	assert.Contains(t, info, "</env>")
	assert.Contains(t, info, "Working directory: "+wd)
	assert.Contains(t, info, "Platform: "+runtime.GOOS)

	if isGitRepo(wd) {
		assert.Contains(t, info, "Is directory a git repo: Yes")
	} else {
		assert.Contains(t, info, "Is directory a git repo: No")
	}
}
