package provider

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/docker/cagent/pkg/chat"
	latest "github.com/docker/cagent/pkg/config/v2"
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/model/provider/anthropic"
	"github.com/docker/cagent/pkg/model/provider/dmr"
	"github.com/docker/cagent/pkg/model/provider/gemini"
	"github.com/docker/cagent/pkg/model/provider/openai"
	"github.com/docker/cagent/pkg/model/provider/options"
	"github.com/docker/cagent/pkg/tools"
)

// Alias defines the configuration for a provider alias
type Alias struct {
	APIType     string // The actual API type to use (openai, anthropic, etc.)
	BaseURL     string // Default base URL for the provider
	TokenEnvVar string // Environment variable name for the API token
}

// ProviderAliases maps provider names to their corresponding configurations
var ProviderAliases = map[string]Alias{
	"requesty": {
		APIType:     "openai",
		BaseURL:     "https://router.requesty.ai/v1",
		TokenEnvVar: "REQUESTY_API_KEY",
	},
	"azure": {
		APIType:     "openai",
		TokenEnvVar: "AZURE_API_KEY",
	},
}

// Provider defines the interface for model providers
type Provider interface {
	// ID returns the model provider ID
	ID() string
	// CreateChatCompletionStream creates a streaming chat completion request
	// It returns a stream that can be iterated over to get completion chunks
	CreateChatCompletionStream(
		ctx context.Context,
		messages []chat.Message,
		tools []tools.Tool,
	) (chat.MessageStream, error)
	// Options returns the effective model options used by this provider
	Options() options.ModelOptions
}

func New(ctx context.Context, cfg *latest.ModelConfig, env environment.Provider, opts ...options.Opt) (Provider, error) {
	slog.Debug("Creating model provider", "type", cfg.Provider, "model", cfg.Model)

	// Apply provider alias defaults to the config
	enhancedCfg := applyProviderDefaults(cfg)
	apiType := ""
	if alias, exists := ProviderAliases[cfg.Provider]; exists {
		apiType = alias.APIType
	}

	// Resolve the actual API type from aliases or direct specification
	providerType := resolveProviderType(cfg.Provider, apiType)

	switch providerType {
	case "openai":
		return openai.NewClient(ctx, enhancedCfg, env, opts...)

	case "anthropic":
		return anthropic.NewClient(ctx, enhancedCfg, env, opts...)

	case "google":
		return gemini.NewClient(ctx, enhancedCfg, env, opts...)

	case "dmr":
		return dmr.NewClient(ctx, enhancedCfg, opts...)

	default:
		slog.Error("Unknown provider type", "type", providerType)
		return nil, fmt.Errorf("unknown provider type: %s", providerType)
	}
}

// applyProviderDefaults applies default configuration from provider aliases to the model config
// This sets default base URLs and token keys if not already specified
func applyProviderDefaults(cfg *latest.ModelConfig) *latest.ModelConfig {
	// Create a copy to avoid modifying the original
	enhancedCfg := *cfg

	// Check if provider has alias configuration
	if alias, exists := ProviderAliases[cfg.Provider]; exists {
		// Set default base URL if not already specified
		if enhancedCfg.BaseURL == "" && alias.BaseURL != "" {
			enhancedCfg.BaseURL = alias.BaseURL
		}

		// Set default token key if not already specified
		if enhancedCfg.TokenKey == "" && alias.TokenEnvVar != "" {
			enhancedCfg.TokenKey = alias.TokenEnvVar
		}
	}

	return &enhancedCfg
}

// resolveProviderType resolves the actual API type from the provider name and optional apiType
func resolveProviderType(provider, apiType string) string {
	// If apiType is explicitly provided, use it
	if apiType != "" {
		return apiType
	}

	// Check if provider has an alias mapping
	if resolved, exists := ProviderAliases[provider]; exists {
		return resolved.APIType
	}

	// Fall back to the provider name itself
	return provider
}
