package paths_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/paths"
)

func TestOverrides(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		set    func(string)
		get    func() string
		custom string
	}{
		{"CacheDir", paths.SetCacheDir, paths.GetCacheDir, "/custom/cache"},
		{"ConfigDir", paths.SetConfigDir, paths.GetConfigDir, "/custom/config"},
		{"DataDir", paths.SetDataDir, paths.GetDataDir, "/custom/data"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Restore default after the test.
			t.Cleanup(func() { tt.set("") })

			original := tt.get()
			assert.NotEmpty(t, original)

			tt.set(tt.custom)
			assert.Equal(t, tt.custom, tt.get())

			// Empty string restores the default.
			tt.set("")
			assert.Equal(t, original, tt.get())
		})
	}
}

func TestGetHomeDir(t *testing.T) {
	t.Parallel()

	assert.NotEmpty(t, paths.GetHomeDir())
}

func TestXDGDirs(t *testing.T) {
	tmpDir := t.TempDir()

	t.Setenv("HOME", tmpDir)
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmpDir, "xdg"))

	// DataDir always respects XDG_DATA_HOME on all platforms.
	paths.SetDataDir("")
	assert.Equal(t, filepath.Join(tmpDir, "xdg", "cagent"), paths.GetDataDir())

	// ConfigDir uses os.UserConfigDir which respects XDG_CONFIG_HOME
	// only on Linux. On macOS it returns ~/Library/Application Support.
	paths.SetConfigDir("")
	expectedConfigDir, err := os.UserConfigDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(expectedConfigDir, "cagent"), paths.GetConfigDir())
}

func TestLegacyFallback(t *testing.T) {
	tests := []struct {
		name string
		set  func(string)
		get  func() string
	}{
		{"DataDir", paths.SetDataDir, paths.GetDataDir},
		{"ConfigDir", paths.SetConfigDir, paths.GetConfigDir},
		{"CacheDir", paths.SetCacheDir, paths.GetCacheDir},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			// Create legacy ~/.cagent dir, but no XDG dir.
			legacyDir := filepath.Join(tmpDir, ".cagent")
			require.NoError(t, os.MkdirAll(legacyDir, 0o755))

			t.Setenv("HOME", tmpDir)
			// Force XDG vars to a non-existent path so the fallback triggers.
			t.Setenv("XDG_DATA_HOME", filepath.Join(tmpDir, "nonexistent"))
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, "nonexistent"))
			tt.set("") // clear any override

			assert.Equal(t, legacyDir, tt.get())
		})
	}
}

func TestXDGOverridesLegacy_WhenBothExist(t *testing.T) {
	tmpDir := t.TempDir()

	// Create both legacy and XDG dirs.
	legacyDir := filepath.Join(tmpDir, ".cagent")
	xdgDataDir := filepath.Join(tmpDir, "xdg_data", "cagent")
	require.NoError(t, os.MkdirAll(legacyDir, 0o755))
	require.NoError(t, os.MkdirAll(xdgDataDir, 0o755))

	t.Setenv("HOME", tmpDir)
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmpDir, "xdg_data"))
	paths.SetDataDir("") // clear any override

	// When both exist, XDG wins.
	assert.Equal(t, xdgDataDir, paths.GetDataDir())
}
