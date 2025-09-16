package paths

import (
	"os"
	"path/filepath"
)

// GetConfigDir returns the user's config directory for cagent
// Falls back to temp directory if home directory cannot be determined
func GetConfigDir() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		// Fallback to temp directory
		return filepath.Join(os.TempDir(), ".cagent-config")
	}
	return filepath.Join(homeDir, ".config", "cagent")
}

// GetDataDir returns the user's data directory for cagent (caches, content, logs)
// Falls back to temp directory if home directory cannot be determined
func GetDataDir() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), ".cagent")
	}
	return filepath.Join(homeDir, ".cagent")
}
