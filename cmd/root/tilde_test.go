package root

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/paths"
)

func TestExpandTilde(t *testing.T) {
	t.Parallel()

	homeDir := paths.GetHomeDir()
	require.NotEmpty(t, homeDir, "Home directory should be available for tests")

	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name:     "expands_tilde_prefix",
			input:    "~/session.db",
			expected: filepath.Join(homeDir, "session.db"),
		},
		{
			name:     "expands_tilde_with_nested_path",
			input:    "~/.cagent/session.db",
			expected: filepath.Join(homeDir, ".cagent", "session.db"),
		},
		{
			name:     "expands_tilde_with_deep_path",
			input:    "~/path/to/some/file.db",
			expected: filepath.Join(homeDir, "path", "to", "some", "file.db"),
		},
		{
			name:     "absolute_path_unchanged",
			input:    "/absolute/path/session.db",
			expected: "/absolute/path/session.db",
		},
		{
			name:     "relative_path_unchanged",
			input:    "relative/path/session.db",
			expected: "relative/path/session.db",
		},
		{
			name:     "tilde_in_middle_unchanged",
			input:    "/some/~/path/session.db",
			expected: "/some/~/path/session.db",
		},
		{
			name:     "tilde_without_slash_unchanged",
			input:    "~something",
			expected: "~something",
		},
		{
			name:     "just_tilde_slash_expands",
			input:    "~/",
			expected: homeDir,
		},
		{
			name:     "empty_string_unchanged",
			input:    "",
			expected: "",
		},
		{
			name:     "dot_path_unchanged",
			input:    "./session.db",
			expected: "./session.db",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := expandTilde(tt.input)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}
