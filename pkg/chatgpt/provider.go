package chatgpt

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

const (
	// TokenEnvVar is the virtual environment variable name used for ChatGPT auth.
	// This is used by the provider system to check/resolve the ChatGPT token.
	TokenEnvVar = "CHATGPT_ACCESS_TOKEN"
)

// Provider implements environment.Provider for ChatGPT tokens.
// It loads the stored token from disk, refreshes it if expired,
// and returns the access token when TokenEnvVar is queried.
type Provider struct {
	mu    sync.Mutex
	token *Token
}

// NewProvider creates a new ChatGPT environment provider.
func NewProvider() *Provider {
	return &Provider{}
}

// Get retrieves the ChatGPT access token when the requested variable
// matches TokenEnvVar. For all other variables, it returns ("", false).
func (p *Provider) Get(ctx context.Context, name string) (string, bool) {
	if name != TokenEnvVar {
		return "", false
	}

	token, err := p.resolveToken(ctx)
	if err != nil {
		slog.Debug("ChatGPT token not available", "error", err)
		return "", false
	}

	return token, true
}

// GetAccessToken returns the current access token, refreshing if needed.
// Unlike Get, this returns an error on failure for direct use by the provider.
func (p *Provider) GetAccessToken(ctx context.Context) (string, error) {
	return p.resolveToken(ctx)
}

// resolveToken loads the token from disk (if not cached), refreshes it if
// expired, persists the refreshed token, and returns the access token.
func (p *Provider) resolveToken(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Load token from disk if not cached
	if p.token == nil {
		token, err := LoadToken()
		if err != nil {
			return "", fmt.Errorf("failed to load ChatGPT token: %w", err)
		}
		if token == nil {
			return "", fmt.Errorf("not logged in to ChatGPT - run 'cagent login chatgpt' first")
		}
		p.token = token
	}

	// Refresh if expired
	if p.token.IsExpired() {
		if p.token.RefreshToken == "" {
			return "", fmt.Errorf("ChatGPT token expired - run 'cagent login chatgpt' to re-authenticate")
		}

		newToken, err := RefreshAccessToken(ctx, p.token.RefreshToken)
		if err != nil {
			return "", fmt.Errorf("failed to refresh ChatGPT token: %w", err)
		}

		p.token = newToken
		if err := SaveToken(newToken); err != nil {
			slog.Warn("Failed to save refreshed ChatGPT token", "error", err)
		}
	}

	return p.token.AccessToken, nil
}
