package environment

import (
	"context"
	"log/slog"

	"github.com/docker/cagent/pkg/auth/oca"
)

// OCATokenProvider provides OCA access tokens from the local token store,
// with automatic refresh when expired.
type OCATokenProvider struct {
	store *oca.TokenStore
}

// NewOCATokenProvider creates a new OCA token environment provider.
func NewOCATokenProvider() *OCATokenProvider {
	return &OCATokenProvider{
		store: oca.NewTokenStore(),
	}
}

// Get returns the OCA access token if the requested name matches.
func (p *OCATokenProvider) Get(ctx context.Context, name string) (string, bool) {
	if name != oca.EnvAccessToken {
		return "", false
	}

	// DefaultIDCSConfig already resolves env var overrides for IDCS endpoints
	cfg := oca.DefaultIDCSConfig()
	token, err := oca.GetValidToken(ctx, cfg, p.store)
	if err != nil {
		slog.Debug("OCA token provider: no valid token available", "error", err)
		return "", false
	}

	return token, true
}
