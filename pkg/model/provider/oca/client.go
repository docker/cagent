package oca

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/docker/cagent/pkg/auth/oca"
	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/model/provider/openai"
	"github.com/docker/cagent/pkg/model/provider/options"
)

// OCAAccessTokenEnv is the environment variable for a pre-obtained OCA token.
const OCAAccessTokenEnv = oca.EnvAccessToken

// NewClient creates a new OCA provider client that wraps the OpenAI client
// pointing at the OCA litellm endpoint.
func NewClient(ctx context.Context, cfg *latest.ModelConfig, env environment.Provider, opts ...options.Opt) (*openai.Client, error) {
	if cfg == nil {
		return nil, errors.New("model configuration is required")
	}

	// Resolve IDCS config from provider opts
	idcsCfg := oca.DefaultIDCSConfig()
	if cfg.ProviderOpts != nil {
		idcsCfg = oca.ConfigFromProviderOpts(cfg.ProviderOpts)
	}

	// Resolve token: env var > token store
	token, _ := env.Get(ctx, OCAAccessTokenEnv)
	if token == "" {
		store := oca.NewTokenStore()
		var err error
		token, err = oca.GetValidToken(ctx, idcsCfg, store)
		if err != nil {
			return nil, fmt.Errorf("OCA authentication failed: %w", err)
		}
	}

	// Set up the config to delegate to OpenAI client
	ocaCfg := *cfg

	// litellm model IDs use "oca/" prefix (e.g., "oca/gpt-4.1").
	// Since cagent splits "oca/gpt-4.1" into provider="oca" + model="gpt-4.1",
	// we need to restore the prefix for the litellm API request.
	if !strings.HasPrefix(ocaCfg.Model, "oca/") {
		ocaCfg.Model = "oca/" + ocaCfg.Model
	}

	// Set base URL to litellm endpoint
	if ocaCfg.BaseURL == "" {
		ocaCfg.BaseURL = idcsCfg.LiteLLMBaseURL
	}

	// Use the token as the API key (litellm accepts Bearer token as API key)
	ocaCfg.TokenKey = OCAAccessTokenEnv

	// Force Chat Completions API (litellm doesn't support Responses API)
	if ocaCfg.ProviderOpts == nil {
		ocaCfg.ProviderOpts = make(map[string]any)
	}
	ocaCfg.ProviderOpts["api_type"] = "openai_chatcompletions"

	slog.Debug("Creating OCA client via OpenAI wrapper",
		"model", ocaCfg.Model,
		"base_url", ocaCfg.BaseURL,
	)

	// Create an environment provider that injects the OCA token
	tokenEnv := &tokenEnvProvider{
		token:    token,
		delegate: env,
	}

	return openai.NewClient(ctx, &ocaCfg, tokenEnv, opts...)
}

// tokenEnvProvider wraps an environment provider to inject the OCA access token.
type tokenEnvProvider struct {
	token    string
	delegate environment.Provider
}

func (p *tokenEnvProvider) Get(ctx context.Context, name string) (string, bool) {
	if name == OCAAccessTokenEnv && p.token != "" {
		return p.token, true
	}
	return p.delegate.Get(ctx, name)
}
