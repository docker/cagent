package config

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/goccy/go-yaml"

	v0 "github.com/docker/cagent/pkg/config/v0"
	v1 "github.com/docker/cagent/pkg/config/v1"
	latest "github.com/docker/cagent/pkg/config/v2" //nolint:staticcheck // This is used everywhere we reference the latest version
	v2 "github.com/docker/cagent/pkg/config/v2"     //nolint:staticcheck // This is used for migrations to v2
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/filesystem"
)

func LoadConfigSecureDeprecated(path, allowedDir string) (*latest.Config, error) {
	fs, err := os.OpenRoot(allowedDir)
	if err != nil {
		return nil, fmt.Errorf("opening filesystem %s: %w", allowedDir, err)
	}

	return LoadConfig(path, fs)
}

func LoadConfig(path string, fs filesystem.FS) (*latest.Config, error) {
	data, err := fs.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	var raw struct {
		Version string `yaml:"version,omitempty"`
	}
	if err := yaml.UnmarshalWithOptions(data, &raw); err != nil {
		return nil, fmt.Errorf("looking for version in config file %s\n%s", path, yaml.FormatError(err, true, true))
	}
	if raw.Version == "" {
		raw.Version = latest.Version
	}

	oldConfig, err := parseCurrentVersion(data, raw.Version)
	if err != nil {
		return nil, fmt.Errorf("parsing config file %s\n%s", path, yaml.FormatError(err, true, true))
	}

	config, err := migrateToLatestConfig(oldConfig)
	if err != nil {
		return nil, fmt.Errorf("migrating config: %w", err)
	}

	config.Version = raw.Version

	if err := validateConfig(&config); err != nil {
		return nil, err
	}

	return &config, nil
}

// CheckRequiredEnvVars checks which environment variables are required by the models and tools.
//
// This allows exiting early with a proper error message instead of failing later when trying to use a model or tool.
func CheckRequiredEnvVars(ctx context.Context, cfg *latest.Config, env environment.Provider, runtimeConfig RuntimeConfig) error {
	missing, err := gatherMissingEnvVars(ctx, cfg, env, runtimeConfig)
	if err != nil {
		// If there's a tool preflight error, log it but continue
		slog.Warn("Failed to preflight toolset environment variables; continuing", "error", err)
	}

	// Return error if there are missing environment variables
	if len(missing) > 0 {
		return &environment.RequiredEnvError{
			Missing: missing,
		}
	}

	return nil
}

func parseCurrentVersion(data []byte, version string) (any, error) {
	options := []yaml.DecodeOption{yaml.Strict()}

	switch version {
	case v0.Version:
		var cfg v0.Config
		err := yaml.UnmarshalWithOptions(data, &cfg, options...)
		return cfg, err
	case v1.Version:
		var cfg v1.Config
		err := yaml.UnmarshalWithOptions(data, &cfg, options...)
		return cfg, err
	case v2.Version:
		var cfg v2.Config
		err := yaml.UnmarshalWithOptions(data, &cfg, options...)
		return cfg, err
	default:
		return nil, fmt.Errorf("unsupported config version: %v", version)
	}
}

func migrateToLatestConfig(c any) (latest.Config, error) {
	var err error
	for {
		if old, ok := c.(v0.Config); ok {
			c, err = v1.UpgradeFrom(old)
			if err != nil {
				return latest.Config{}, err
			}
			continue
		}
		if old, ok := c.(v1.Config); ok {
			c, err = latest.UpgradeFrom(old)
			if err != nil {
				return latest.Config{}, err
			}
			continue
		}

		return c.(latest.Config), nil
	}
}

func validateConfig(cfg *latest.Config) error {
	if cfg.Models == nil {
		cfg.Models = map[string]latest.ModelConfig{}
	}

	for name := range cfg.Models {
		if cfg.Models[name].ParallelToolCalls == nil {
			m := cfg.Models[name]
			m.ParallelToolCalls = boolPtr(true)
			cfg.Models[name] = m
		}
	}

	for agentName := range cfg.Agents {
		agent := cfg.Agents[agentName]

		modelNames := strings.SplitSeq(agent.Model, ",")
		for modelName := range modelNames {
			if _, exists := cfg.Models[modelName]; exists {
				continue
			}

			provider, model, ok := strings.Cut(modelName, "/")
			if !ok {
				return fmt.Errorf("agent '%s' references non-existent model '%s'", agentName, modelName)
			}

			cfg.Models[modelName] = latest.ModelConfig{
				Provider: provider,
				Model:    model,
			}
		}

		for _, subAgentName := range agent.SubAgents {
			if _, exists := cfg.Agents[subAgentName]; !exists {
				return fmt.Errorf("agent '%s' references non-existent sub-agent '%s'", agentName, subAgentName)
			}
		}
	}

	return nil
}

func boolPtr(b bool) *bool {
	return &b
}
