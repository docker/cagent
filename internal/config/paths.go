package config

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
