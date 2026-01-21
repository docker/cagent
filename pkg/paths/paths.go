package paths

import (
	"os"
	"path/filepath"
)

// GetConfigDir returns the user's config directory for cagent.
//
// If the home directory cannot be determined, it falls back to a directory
// under the system temporary directory. This is a best-effort fallback and
// not intended to be a security boundary.
func GetConfigDir() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		// Fallback to temp directory
		return filepath.Clean(filepath.Join(os.TempDir(), ".cagent-config"))
	}
	return filepath.Clean(filepath.Join(homeDir, ".config", "cagent"))
}

// GetDataDir returns the user's data directory for cagent (caches, content, logs).
//
// If the home directory cannot be determined, it falls back to a directory
// under the system temporary directory.
func GetDataDir() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return filepath.Clean(filepath.Join(os.TempDir(), ".cagent"))
	}
	return filepath.Clean(filepath.Join(homeDir, ".cagent"))
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
