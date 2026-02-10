package oca

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/docker/cagent/pkg/paths"
)

const tokenFileName = "oca_tokens.json"

// TokenStore persists OCA OAuth tokens to disk.
type TokenStore struct {
	dir string
}

// NewTokenStore creates a store using the default cagent data directory.
func NewTokenStore() *TokenStore {
	return &TokenStore{dir: paths.GetDataDir()}
}

// NewTokenStoreWithDir creates a store using a custom directory (for testing).
func NewTokenStoreWithDir(dir string) *TokenStore {
	return &TokenStore{dir: dir}
}

func (s *TokenStore) path() string {
	return filepath.Join(s.dir, tokenFileName)
}

// Load reads the token from disk. Returns nil if no token file exists.
func (s *TokenStore) Load() (*Token, error) {
	data, err := os.ReadFile(s.path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading token file: %w", err)
	}
	var t Token
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("parsing token file: %w", err)
	}
	return &t, nil
}

// Save writes the token to disk atomically.
func (s *TokenStore) Save(t *Token) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("creating token directory: %w", err)
	}
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling token: %w", err)
	}
	// Atomic write: write to temp file then rename.
	tmp := s.path() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("writing token file: %w", err)
	}
	if err := os.Rename(tmp, s.path()); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("renaming token file: %w", err)
	}
	return nil
}

// Delete removes the token file from disk.
func (s *TokenStore) Delete() error {
	if err := os.Remove(s.path()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing token file: %w", err)
	}
	return nil
}

// HasValidToken returns true if a non-expired token is stored on disk.
func (s *TokenStore) HasValidToken() bool {
	t, err := s.Load()
	if err != nil || t == nil {
		return false
	}
	return !t.IsExpired() || t.CanRefresh()
}
