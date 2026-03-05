package paths

import (
	"os"
	"path/filepath"
	"sync/atomic"
)

// overridable holds an optional directory override backed by an atomic pointer.
// A nil pointer (the zero value) means "use the default".
type overridable struct{ p atomic.Pointer[string] }

// Set stores an override directory. An empty value clears the override.
func (o *overridable) Set(dir string) {
	if dir == "" {
		o.p.Store(nil)
	} else {
		o.p.Store(&dir)
	}
}

// get returns the override if set, or falls back to the result of defaultFn.
func (o *overridable) get(defaultFn func() string) string {
	if p := o.p.Load(); p != nil {
		return filepath.Clean(*p)
	}
	return defaultFn()
}

var (
	cacheDirOverride  overridable
	configDirOverride overridable
	dataDirOverride   overridable
)

// SetCacheDir overrides the default cache directory returned by [GetCacheDir].
// An empty value restores the default behaviour.
// This should be called early (e.g. during CLI flag processing) before any
// goroutine calls the corresponding getter.
func SetCacheDir(dir string) { cacheDirOverride.Set(dir) }

// SetConfigDir overrides the default config directory returned by [GetConfigDir].
// An empty value restores the default behaviour.
func SetConfigDir(dir string) { configDirOverride.Set(dir) }

// SetDataDir overrides the default data directory returned by [GetDataDir].
// An empty value restores the default behaviour.
func SetDataDir(dir string) { dataDirOverride.Set(dir) }

// GetCacheDir returns the user's cache directory for cagent.
//
// If an override has been set via [SetCacheDir] it is returned instead.
//
// The default location follows the XDG Base Directory Specification:
//   - $XDG_CACHE_HOME/cagent (Linux, default ~/.cache/cagent)
//   - ~/Library/Caches/cagent (macOS)
//   - %LocalAppData%/cagent (Windows)
//
// For backward compatibility, if the legacy ~/.cagent directory exists and
// the XDG directory does not, the legacy path is used instead.
func GetCacheDir() string {
	return cacheDirOverride.get(func() string {
		return resolveWithLegacyFallback(xdgCacheDir())
	})
}

// GetConfigDir returns the user's config directory for cagent.
//
// If an override has been set via [SetConfigDir] it is returned instead.
//
// The default location is the OS-standard user config directory
// (as returned by [os.UserConfigDir]) with a "cagent" subdirectory:
//   - $XDG_CONFIG_HOME/cagent on Linux (default ~/.config/cagent)
//   - ~/Library/Application Support/cagent on macOS
//   - %AppData%/cagent on Windows
//
// For backward compatibility, if the legacy ~/.cagent directory exists and
// the standard directory does not, the legacy path is used instead.
func GetConfigDir() string {
	return configDirOverride.get(func() string {
		return resolveWithLegacyFallback(xdgConfigDir())
	})
}

// GetDataDir returns the user's data directory for cagent (sessions, history,
// installed tools, OCI store, etc.).
//
// If an override has been set via [SetDataDir] it is returned instead.
//
// The default location follows the XDG Base Directory Specification on Linux:
//   - $XDG_DATA_HOME/cagent (default ~/.local/share/cagent)
//
// On macOS and Windows the same Linux-style path is used for consistency
// (~/.local/share/cagent), since Go does not provide an os.UserDataDir.
//
// For backward compatibility, if the legacy ~/.cagent directory exists and
// the XDG directory does not, the legacy path is used instead.
func GetDataDir() string {
	return dataDirOverride.get(func() string {
		return resolveWithLegacyFallback(xdgDataDir())
	})
}

// GetHomeDir returns the user's home directory.
//
// Returns an empty string if the home directory cannot be determined.
func GetHomeDir() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Clean(homeDir)
}

// --- XDG directory helpers ---

func xdgCacheDir() string {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return filepath.Join(os.TempDir(), ".cagent-cache")
	}
	return filepath.Join(cacheDir, "cagent")
}

func xdgConfigDir() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return filepath.Join(os.TempDir(), ".cagent-config")
	}
	return filepath.Join(configDir, "cagent")
}

func xdgDataDir() string {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, "cagent")
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), ".cagent")
	}
	return filepath.Join(homeDir, ".local", "share", "cagent")
}

// --- Legacy fallback ---

// resolveWithLegacyFallback returns the legacy ~/.cagent path when it exists
// and xdgDir does not yet exist, preserving data for existing users.
// Otherwise it returns xdgDir.
func resolveWithLegacyFallback(xdgDir string) string {
	if legacy := legacyDir(); legacy != "" && dirExists(legacy) && !dirExists(xdgDir) {
		return filepath.Clean(legacy)
	}
	return filepath.Clean(xdgDir)
}

// legacyDir returns the legacy ~/.cagent directory path, or empty string
// if the home directory cannot be determined.
func legacyDir() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(homeDir, ".cagent")
}

// dirExists reports whether dir exists and is a directory.
func dirExists(dir string) bool {
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}
