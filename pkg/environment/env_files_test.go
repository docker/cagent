package environment

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandAll(t *testing.T) {
	t.Setenv("USER", "alice")
	t.Setenv("HOME", "/home/alice")

	expanded, err := ExpandAll(t.Context(), []string{"Hello $USER", "Your home is at $HOME", "No variable here"}, NewOsEnvProvider())

	require.NoError(t, err)
	assert.Equal(t, []string{"Hello alice", "Your home is at /home/alice", "No variable here"}, expanded)
}

func TestExpandAll_Error(t *testing.T) {
	t.Setenv("UNKNOWN_VAR", "")

	expanded, err := ExpandAll(t.Context(), []string{"$UNKNOWN_VAR"}, NewOsEnvProvider())

	require.Error(t, err)
	assert.Empty(t, expanded)
}

func TestAbsolutePath(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	tests := []struct {
		name        string
		input       string
		expected    string
		expectError bool
	}{
		{
			name:     "no tilde",
			input:    "/absolute/path",
			expected: "/absolute/path",
		},
		{
			name:     "relative path",
			input:    "relative/path",
			expected: "/root/relative/path",
		},
		{
			name:     "tilde only",
			input:    "~",
			expected: homeDir,
		},
		{
			name:     "tilde with slash",
			input:    "~/env/slack.env",
			expected: filepath.Join(homeDir, "env", "slack.env"),
		},
		{
			name:     "tilde with deeper path",
			input:    "~/config/app/settings.env",
			expected: filepath.Join(homeDir, "config", "app", "settings.env"),
		},
		{
			name:        "unsupported tilde format",
			input:       "~user/path",
			expected:    "",
			expectError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := AbsolutePath("/root", test.input)
			if test.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "unsupported tilde expansion format")
			} else {
				require.NoError(t, err)
				assert.Equal(t, test.expected, result)
			}
		})
	}
}

func TestReadEnvFilesEmpty(t *testing.T) {
	lines, err := ReadEnvFiles([]string{})

	require.NoError(t, err)
	assert.Empty(t, lines)
}

func TestReadEnvFiles(t *testing.T) {
	temp := t.TempDir()
	write(t, filepath.Join(temp, ".env1"), "KEY1=VALUE1\n# Comment\nKEY2=VALUE2\n")
	write(t, filepath.Join(temp, ".env2"), "\n\nKEY3=\"VALUE3\"\n")

	lines, err := ReadEnvFiles([]string{filepath.Join(temp, ".env1"), filepath.Join(temp, ".env2")})

	require.NoError(t, err)
	assert.Len(t, lines, 3)
	assert.Equal(t, "KEY1", lines[0].Key)
	assert.Equal(t, "VALUE1", lines[0].Value)
	assert.Equal(t, "KEY2", lines[1].Key)
	assert.Equal(t, "VALUE2", lines[1].Value)
	assert.Equal(t, "KEY3", lines[2].Key)
	assert.Equal(t, "VALUE3", lines[2].Value)
}

func TestReadEnvFileNotFound(t *testing.T) {
	temp := t.TempDir()

	lines, err := ReadEnvFile(filepath.Join(temp, ".notfound"))

	require.Error(t, err)
	assert.Empty(t, lines)
}

func TestReadEnvFileInvalid(t *testing.T) {
	temp := t.TempDir()
	write(t, filepath.Join(temp, ".invalid"), "The is not a valid env file")

	lines, err := ReadEnvFile(filepath.Join(temp, ".invalid"))

	require.Error(t, err)
	assert.Empty(t, lines)
}

func write(t *testing.T, path, content string) {
	t.Helper()
	err := os.WriteFile(path, []byte(content), 0o644)
	require.NoError(t, err)
}
