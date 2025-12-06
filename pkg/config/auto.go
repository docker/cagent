package config

import (
	"context"

	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/environment"
)

var DefaultModels = map[string]string{
	"openai":    "gpt-5-mini",
	"anthropic": "claude-sonnet-4-0",
	"google":    "gemini-2.5-flash",
	"dmr":       "ai/qwen3:latest",
	"mistral":   "mistral-small-latest",
	"bedrock":   "global.anthropic.claude-sonnet-4-5-20250929-v1:0",
}

func AvailableProviders(ctx context.Context, modelsGateway string, env environment.Provider) []string {
	var providers []string

	if modelsGateway == "" {
		switch {
		case env.Get(ctx, "ANTHROPIC_API_KEY") != "":
			providers = append(providers, "anthropic")
		case env.Get(ctx, "OPENAI_API_KEY") != "":
			providers = append(providers, "openai")
		case env.Get(ctx, "GOOGLE_API_KEY") != "":
			providers = append(providers, "google")
		case env.Get(ctx, "MISTRAL_API_KEY") != "":
			providers = append(providers, "mistral")
		case env.Get(ctx, "AWS_BEARER_TOKEN_BEDROCK") != "" ||
			env.Get(ctx, "AWS_ACCESS_KEY_ID") != "" ||
			env.Get(ctx, "AWS_PROFILE") != "" ||
			env.Get(ctx, "AWS_ROLE_ARN") != "":
			providers = append(providers, "bedrock")
		default:
			providers = append(providers, "dmr")
		}
	} else {
		// Default to anthropic when using a gateway
		providers = append(providers, "anthropic")
	}

	return providers
}

func AutoModelConfig(ctx context.Context, modelsGateway string, env environment.Provider) latest.ModelConfig {
	availableProviders := AvailableProviders(ctx, modelsGateway, env)
	firstAvailable := availableProviders[0]

	return latest.ModelConfig{
		Provider:  firstAvailable,
		Model:     DefaultModels[firstAvailable],
		MaxTokens: PreferredMaxTokens(firstAvailable),
	}
}

func PreferredMaxTokens(provider string) int {
	if provider == "dmr" {
		return 16000
	}
	return 64000
}

// AutoEmbeddingModelConfigs returns the ordered list of embedding-capable models
// to try when a RAG strategy uses `model: auto` for embeddings.
//
// The priority is:
//  1. OpenAI -> text-embedding-3-small model
//  2. DMR -> Google's embeddinggemma model (via Docker Model Runner)
func AutoEmbeddingModelConfigs() []latest.ModelConfig {
	return []latest.ModelConfig{
		{
			Provider: "openai",
			Model:    "text-embedding-3-small",
		},
		{
			Provider: "dmr",
			Model:    "ai/embeddinggemma",
		},
	}
}
