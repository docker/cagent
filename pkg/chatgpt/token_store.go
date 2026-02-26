package chatgpt

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/docker/cagent/pkg/paths"
)

// tokenFilePathFunc is overridable for testing.
var tokenFilePathFunc = defaultTokenFilePath

// defaultTokenFilePath returns the path to the stored ChatGPT token file.
func defaultTokenFilePath() string {
	return filepath.Join(paths.GetConfigDir(), "chatgpt_token.json")
}

// tokenFilePath returns the path to the stored ChatGPT token file.
func tokenFilePath() string {
	return tokenFilePathFunc()
}

// LoadToken loads a stored ChatGPT token from disk.
// Returns nil if no token is stored or the file is invalid.
func LoadToken() (*Token, error) {
	data, err := os.ReadFile(tokenFilePath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read token file: %w", err)
	}

	var token Token
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, fmt.Errorf("failed to parse token file: %w", err)
	}

	return &token, nil
}

// SaveToken saves a ChatGPT token to disk.
func SaveToken(token *Token) error {
	path := tokenFilePath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal token: %w", err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("failed to write token file: %w", err)
	}

	return nil
}

// RemoveToken removes the stored ChatGPT token from disk.
func RemoveToken() error {
	if err := os.Remove(tokenFilePath()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove token file: %w", err)
	}
	return nil
}
