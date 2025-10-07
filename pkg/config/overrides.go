package config

import (
	"fmt"
	"strings"

	v2 "github.com/docker/cagent/pkg/config/v2"
)

// ApplyModelOverrides applies CLI model overrides to the configuration
func ApplyModelOverrides(cfg *v2.Config, overrides []string) error {
	for _, override := range overrides {
		if err := applySingleOverride(cfg, override); err != nil {
			return err
		}
	}

	// After applying overrides, ensure new models are added to cfg.Models
	return ensureModelsExist(cfg)
}

// applySingleOverride processes a single model override string
func applySingleOverride(cfg *v2.Config, override string) error {
	override = strings.TrimSpace(override)
	if override == "" {
		return nil // Skip empty overrides
	}

	// Handle comma-separated format: "agent1=model1,agent2=model2"
	if strings.Contains(override, ",") {
		for part := range strings.SplitSeq(override, ",") {
			if err := applySingleOverride(cfg, part); err != nil {
				return err
			}
		}
		return nil
	}

	// Check if this is an agent-specific override (contains '=')
	agentName, modelSpec, ok := strings.Cut(override, "=")
	if ok {
		agentName = strings.TrimSpace(agentName)
		if agentName == "" {
			return fmt.Errorf("empty agent name in override: %s", override)
		}

		modelSpec = strings.TrimSpace(modelSpec)
		if modelSpec == "" {
			return fmt.Errorf("empty model specification in override: %s", override)
		}

		// Apply to specific agent
		agentConfig, exists := cfg.Agents[agentName]
		if !exists {
			return fmt.Errorf("unknown agent '%s'", agentName)
		}

		agentConfig.Model = modelSpec
		cfg.Agents[agentName] = agentConfig
	} else {
		// Global override: apply to all agents
		modelSpec := strings.TrimSpace(override)
		if modelSpec == "" {
			return fmt.Errorf("empty model specification")
		}

		for name := range cfg.Agents {
			agentConfig := cfg.Agents[name]
			agentConfig.Model = modelSpec
			cfg.Agents[name] = agentConfig
		}
	}

	return nil
}

// ensureModelsExist ensures that all models referenced by agents exist in cfg.Models
// This handles inline model specs that may have been added via CLI overrides
func ensureModelsExist(cfg *v2.Config) error {
	if cfg.Models == nil {
		cfg.Models = map[string]v2.ModelConfig{}
	}

	for agentName := range cfg.Agents {
		agentConfig := cfg.Agents[agentName]

		modelNames := strings.SplitSeq(agentConfig.Model, ",")
		for modelName := range modelNames {
			if _, exists := cfg.Models[modelName]; exists {
				continue
			}

			providerName, model, ok := strings.Cut(modelName, "/")
			if !ok {
				return fmt.Errorf("agent '%s' references non-existent model '%s'", agentName, modelName)
			}

			cfg.Models[modelName] = v2.ModelConfig{
				Provider: providerName,
				Model:    model,
			}
		}
	}

	return nil
}
