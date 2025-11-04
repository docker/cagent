package config

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/goccy/go-yaml"

	v0 "github.com/docker/cagent/pkg/config/v0"
	v1 "github.com/docker/cagent/pkg/config/v1"
	latest "github.com/docker/cagent/pkg/config/v2"
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/filesystem"
)

func LoadConfigSecureDeprecated(path, allowedDir string) (*latest.Config, error) {
	root, err := os.OpenRoot(allowedDir)
	if err != nil {
		return nil, fmt.Errorf("opening filesystem %s: %w", allowedDir, err)
	}
	defer root.Close()

	// Wrap os.Root to implement filesystem.FS
	fs := &rootFS{root: root}
	return LoadConfig(path, fs)
}

// rootFS wraps os.Root to implement filesystem.FS
type rootFS struct {
	root *os.Root
}

func (r *rootFS) ReadFile(name string) ([]byte, error) {
	file, err := r.root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(file)
}

func LoadConfig(path string, fs filesystem.FS) (*latest.Config, error) {
	data, err := fs.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	var raw struct {
		Version any `yaml:"version"`
	}
	if err := yaml.UnmarshalWithOptions(data, &raw); err != nil {
		return nil, fmt.Errorf("looking for version in config file %s\n%s", path, yaml.FormatError(err, true, true))
	}

	oldConfig, err := parseCurrentVersion(data, raw.Version)
	if err != nil {
		return nil, fmt.Errorf("parsing config file %s\n%s", path, yaml.FormatError(err, true, true))
	}

	config, err := migrateToLatestConfig(oldConfig)
	if err != nil {
		return nil, fmt.Errorf("migrating config: %w", err)
	}

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

func parseCurrentVersion(data []byte, version any) (any, error) {
	options := []yaml.DecodeOption{yaml.Strict()}

	switch version {
	case nil, "0", 0:
		var cfg v0.Config
		err := yaml.UnmarshalWithOptions(data, &cfg, options...)
		return cfg, err
	case "1", 1:
		var cfg v1.Config
		err := yaml.UnmarshalWithOptions(data, &cfg, options...)
		return cfg, err
	default:
		var cfg latest.Config
		err := yaml.UnmarshalWithOptions(data, &cfg, options...)
		return cfg, err
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
