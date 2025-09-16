package mcp

import (
	"log/slog"
	"sync"

	"github.com/mark3labs/mcp-go/client"
)

// TokenManager manages token stores per URL
type TokenManager struct {
	stores map[string]client.TokenStore
	mu     sync.RWMutex
}

var (
	tokenManager *TokenManager
	managerOnce  sync.Once
)

// getTokenManager returns the global token manager instance, creating it if necessary
func getTokenManager() *TokenManager {
	managerOnce.Do(func() {
		tokenManager = &TokenManager{
			stores: make(map[string]client.TokenStore),
		}
		slog.Debug("Created global token manager")
	})
	return tokenManager
}

// GetTokenStoreForServer returns a token store for the given URL, creating it if necessary
func (m *TokenManager) GetTokenStoreForServer(url string) client.TokenStore {
	m.mu.RLock()
	store, exists := m.stores[url]
	m.mu.RUnlock()

	if exists {
		return store
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check in case another goroutine created it while we were waiting for the lock
	if store, exists := m.stores[url]; exists {
		return store
	}

	store = client.NewMemoryTokenStore()
	m.stores[url] = store
	slog.Debug("Created token store for URL", "url", url)
	return store
}

// GetTokenStore returns the tokenStore instance for the given URL
func GetTokenStore(url string) client.TokenStore {
	manager := getTokenManager()
	return manager.GetTokenStoreForServer(url)
}
