package secrets

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/docker/cagent/pkg/config"
	latest "github.com/docker/cagent/pkg/config/v2"
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/gateway"
	"github.com/docker/cagent/pkg/model/provider"
	"github.com/docker/cagent/pkg/tools/mcp"
)

// GatherMissingEnvVars finds out which environment variables are required by the models and tools.
// This allows exiting early with a proper error message instead of failing later when trying to use a model or tool.
// TODO(dga): This code contains lots of duplication and ought to be refactored.
func GatherMissingEnvVars(ctx context.Context, cfg *latest.Config, env environment.Provider, runtimeConfig config.RuntimeConfig) ([]string, error) {
	requiredEnv := map[string]bool{}

	// Models
	if runtimeConfig.ModelsGateway == "" {
		names := GatherEnvVarsForModels(cfg)
		for _, e := range names {
			requiredEnv[e] = true
		}
	}

	// Tools
	if mcpGatewayURL := os.Getenv(mcp.DOCKER_MCP_GATEWAY_URL_ENV); mcpGatewayURL != "" {
		names, err := GatherEnvVarsForTools(ctx, cfg)
		if err != nil {
			return nil, err
		}
		for _, e := range names {
			requiredEnv[e] = true
		}
	}

	// Check for missing
	var missing []string
	for _, e := range mcpToSortedList(requiredEnv) {
		if env.Get(ctx, e) == "" {
			missing = append(missing, e)
		}
	}

	return missing, nil
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
			}
		}
	}

	return mcpToSortedList(requiredEnv)
}

func GatherEnvVarsForTools(ctx context.Context, cfg *latest.Config) ([]string, error) {
	requiredEnv := map[string]bool{}

	for _, ref := range config.GatherMCPServerReferences(cfg) {
		mcpServerName := gateway.ParseServerRef(ref)

		secrets, err := gateway.RequiredEnvVars(ctx, mcpServerName, gateway.DockerCatalogURL)
		if err != nil {
			return nil, fmt.Errorf("reading which secrets the MCP server needs: %w", err)
		}

		for _, secret := range secrets {
			requiredEnv[secret.Env] = true
		}
	}

	return mcpToSortedList(requiredEnv), nil
}

func mcpToSortedList(requiredEnv map[string]bool) []string {
	var requiredEnvList []string

	for e := range requiredEnv {
		requiredEnvList = append(requiredEnvList, e)
	}
	sort.Strings(requiredEnvList)

	return requiredEnvList
}
