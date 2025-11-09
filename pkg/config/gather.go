package config

import (
	"context"
	"errors"
	"fmt"
	"sort"

	latest "github.com/docker/cagent/pkg/config/v2"
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/gateway"
	"github.com/docker/cagent/pkg/model/provider"
)

// gatherMissingEnvVars finds out which environment variables are required by the models and tools.
// It returns the missing variables and any non-fatal error encountered during tool discovery.
func gatherMissingEnvVars(ctx context.Context, cfg *latest.Config, env environment.Provider, runtimeConfig RuntimeConfig) (missing []string, toolErr error) {
	requiredEnv := map[string]bool{}

	// Models
	if runtimeConfig.ModelsGateway == "" {
		names := GatherEnvVarsForModels(cfg)
		for _, e := range names {
			requiredEnv[e] = true
		}
	}

	// Tools
	names, err := GatherEnvVarsForTools(ctx, cfg)
	if err != nil {
		// Store tool preflight error but continue checking models
		toolErr = err
	} else {
		for _, e := range names {
			requiredEnv[e] = true
		}
	}

	for _, e := range mcpToSortedList(requiredEnv) {
		if env.Get(ctx, e) == "" {
			missing = append(missing, e)
		}
	}

	return missing, toolErr
}

func GatherEnvVarsForModels(cfg *latest.Config) []string {
	requiredEnv := map[string]bool{}

	for name := range cfg.Models {
		model := cfg.Models[name]

		// Use the token environment variable from the alias if available
		if alias, exists := provider.ProviderAliases[model.Provider]; exists {
			if alias.TokenEnvVar != "" {
				requiredEnv[alias.TokenEnvVar] = true
			}
		} else {
			// Fallback to hardcoded mappings for unknown providers
			switch model.Provider {
			case "openai":
				requiredEnv["OPENAI_API_KEY"] = true
			case "anthropic":
				requiredEnv["ANTHROPIC_API_KEY"] = true
			case "google":
				requiredEnv["GOOGLE_API_KEY"] = true
			case "mistral":
				requiredEnv["MISTRAL_API_KEY"] = true
			}
		}
	}

	return mcpToSortedList(requiredEnv)
}

func GatherEnvVarsForTools(ctx context.Context, cfg *latest.Config) ([]string, error) {
	requiredEnv := map[string]bool{}
	var errs []error

	for _, ref := range gatherMCPServerReferences(cfg) {
		mcpServerName := gateway.ParseServerRef(ref)

		secrets, err := gateway.RequiredEnvVars(ctx, mcpServerName)
		if err != nil {
			errs = append(errs, fmt.Errorf("reading which secrets the MCP server needs for %s: %w", ref, err))
			continue
		}

		for _, secret := range secrets {
			requiredEnv[secret.Env] = true
		}
	}

	if len(errs) > 0 {
		return mcpToSortedList(requiredEnv), fmt.Errorf("tool env preflight: %w", errors.Join(errs...))
	}
	return mcpToSortedList(requiredEnv), nil
}

func gatherMCPServerReferences(cfg *latest.Config) []string {
	servers := map[string]bool{}

	for i := range cfg.Agents {
		agent := cfg.Agents[i]
		for j := range agent.Toolsets {
			toolSet := agent.Toolsets[j]

			if toolSet.Type == "mcp" && toolSet.Ref != "" {
				servers[toolSet.Ref] = true
			}
		}
	}

	var list []string
	for e := range servers {
		list = append(list, e)
	}
	sort.Strings(list)

	return list
}

func mcpToSortedList(requiredEnv map[string]bool) []string {
	var requiredEnvList []string

	for e := range requiredEnv {
		requiredEnvList = append(requiredEnvList, e)
	}
	sort.Strings(requiredEnvList)

	return requiredEnvList
}
