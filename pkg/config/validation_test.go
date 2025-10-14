package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidatePathInDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.yaml")
	err := os.WriteFile(testFile, []byte("test: value"), 0o644)
	require.NoError(t, err)

	tests := []struct {
		name        string
		path        string
		allowedDir  string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid path within directory",
			path:        filepath.Join(tmpDir, "test.yaml"),
			allowedDir:  tmpDir,
			expectError: false,
		},
		{
			name:        "valid relative path",
			path:        "test.yaml",
			allowedDir:  tmpDir,
			expectError: false,
		},
		{
			name:        "path traversal attempt with ../",
			path:        "../../../etc/passwd",
			allowedDir:  tmpDir,
			expectError: true,
			errorMsg:    "path outside allowed directory",
		},
		{
			name:        "path traversal attempt with subdirectory",
			path:        filepath.Join(tmpDir, "..", "..", "..", "etc", "passwd"),
			allowedDir:  tmpDir,
			expectError: true,
			errorMsg:    "path outside allowed directory",
		},
		{
			name:        "path with .. in middle",
			path:        "subdir/../test.yaml",
			allowedDir:  tmpDir,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := ValidatePathInDirectory(tt.path, tt.allowedDir)

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errorMsg)
				require.Empty(t, result)
			} else {
				require.NoError(t, err)
				require.NotEmpty(t, result)
				absAllowedDir, _ := filepath.Abs(tt.allowedDir)
				relPath, err := filepath.Rel(absAllowedDir, result)
				require.NoError(t, err)
				require.False(t, strings.HasPrefix(relPath, ".."))
			}
		})
	}
}

func TestLoadConfig_InvalidPath(t *testing.T) {
	tmp := openRoot(t, t.TempDir())

	validConfig := `version: 1
agents:
  root:
    model: "openai/gpt-4"
`

	err := tmp.WriteFile("valid.yaml", []byte(validConfig), 0o644)
	require.NoError(t, err)

	cfg, err := LoadConfig("valid.yaml", tmp)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	_, err = LoadConfig("../../../etc/passwd", tmp)
	require.Error(t, err)
}

func TestLoadConfigSecureDeprecated_InvalidPath(t *testing.T) {
	tmp := t.TempDir()

	validConfig := `version: 1
agents:
  root:
    model: "openai/gpt-4"
`

	err := os.WriteFile(filepath.Join(tmp, "valid.yaml"), []byte(validConfig), 0o644)
	require.NoError(t, err)

	cfg, err := LoadConfigSecureDeprecated("valid.yaml", tmp)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	_, err = LoadConfigSecureDeprecated("../../../etc/passwd", tmp)
	require.Error(t, err)
}

func TestValidationErrors(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{
			name: "memory toolset missing path",
			path: "missing_memory_path_v2.yaml",
		},
		{
			name: "path in non memory toolset",
			path: "invalid_path_v2.yaml",
		},
		{
			name: "post_edit in non filesystem toolset",
			path: "invalid_post_edit_v2.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := openRoot(t, "testdata")

			_, err := LoadConfig(tt.path, root)
			require.Error(t, err)
		})
	}
}
