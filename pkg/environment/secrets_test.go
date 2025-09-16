package environment

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeSecret(value string) func(string) error {
	return func(path string) error {
		return os.WriteFile(path, []byte(value), 0o700)
	}
}

func writeNothing() func(string) error {
	return func(string) error {
		return nil
	}
}

func TestRunSecretsProvider(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		writer   func(string) error
		expected string
	}{
		{
			name:     "env var KEY",
			key:      "KEY",
			writer:   writeSecret("VALUE"),
			expected: "VALUE",
		},
		{
			name:     "empty",
			key:      "KEY",
			writer:   writeSecret(""),
			expected: "",
		},
		{
			name:     "none",
			key:      "KEY",
			writer:   writeNothing(),
			expected: "",
		},
		{
			name:     "ignore new lines",
			key:      "KEY",
			writer:   writeSecret("VALUE\n\n"),
			expected: "VALUE",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			tmp := t.TempDir()

			err := test.writer(filepath.Join(tmp, test.key))
			require.NoError(t, err)

			provider := &RunSecretsProvider{
				root: tmp,
			}
			actual := provider.Get(t.Context(), test.key)

			assert.Equal(t, test.expected, actual)
		})
	}
}
