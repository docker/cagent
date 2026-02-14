package config

import (
	"log/slog"
	"strings"

	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/modelsdev"
)

// ResolveModelAliases resolves model aliases to their pinned versions in the config.
// For example, "claude-sonnet-4-5" might resolve to "claude-sonnet-4-5-20250929".
// This modifies the config in place.
//
// NOTE: Alias resolution is skipped for models with custom base_url configurations,
// either set directly on the model or inherited from a custom provider definition.
// This is necessary because external providers (like Azure Foundry) may use the alias
// names directly as deployment names rather than the pinned version names.
func ResolveModelAliases(cfg *latest.Config, store *modelsdev.Store) {
	// Resolve model aliases in the models section
	for name, modelCfg := range cfg.Models {
		// Skip alias resolution for models with custom base_url (direct or via provider)
		// Custom endpoints like Azure Foundry use alias names as deployment names
		if hasCustomBaseURL(&modelCfg, cfg.Providers) {
			slog.Debug("Skipping model alias resolution for model with custom base_url",
				"model_name", name, "provider", modelCfg.Provider, "model", modelCfg.Model)
			continue
		}

		if resolved := store.ResolveModelAlias(modelCfg.Provider, modelCfg.Model); resolved != modelCfg.Model {
			modelCfg.Model = resolved
			cfg.Models[name] = modelCfg
		}

		// Resolve model aliases in routing rules
		for i, rule := range modelCfg.Routing {
			if provider, model, ok := strings.Cut(rule.Model, "/"); ok {
				if resolved := store.ResolveModelAlias(provider, model); resolved != model {
					modelCfg.Routing[i].Model = provider + "/" + resolved
				}
			}
		}
		cfg.Models[name] = modelCfg
	}

	// Resolve inline model references in agents (e.g., "anthropic/claude-sonnet-4-5")
	for _, agent := range cfg.Agents {
		if agent.Model == "" || agent.Model == "auto" {
			continue
		}

		var resolvedModels []string
		for modelRef := range strings.SplitSeq(agent.Model, ",") {
			if provider, model, ok := strings.Cut(modelRef, "/"); ok {
				if resolved := store.ResolveModelAlias(provider, model); resolved != model {
					resolvedModels = append(resolvedModels, provider+"/"+resolved)
					continue
				}
			}
			resolvedModels = append(resolvedModels, modelRef)
		}

		cfg.Agents.Update(agent.Name, func(a *latest.AgentConfig) {
			a.Model = strings.Join(resolvedModels, ",")
		})
	}
}

// hasCustomBaseURL checks if a model config has a custom base_url, either directly
// or through a referenced provider definition.
func hasCustomBaseURL(modelCfg *latest.ModelConfig, providers map[string]latest.ProviderConfig) bool {
	// Check if the model has a direct base_url
	if modelCfg.BaseURL != "" {
		return true
	}

	// Check if the model references a provider with a base_url
	if providers != nil && modelCfg.Provider != "" {
		if providerCfg, exists := providers[modelCfg.Provider]; exists {
			return providerCfg.BaseURL != ""
		}
	}

	return false
}
