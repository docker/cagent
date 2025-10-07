package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/goccy/go-yaml"

	v0 "github.com/docker/cagent/pkg/config/v0"
	v1 "github.com/docker/cagent/pkg/config/v1"
	latest "github.com/docker/cagent/pkg/config/v2"
	v2 "github.com/docker/cagent/pkg/config/v2"
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
	dir := filepath.Dir(path)

	data, err := fs.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	var raw struct {
		Version any `yaml:"version"`
	}
	if err := yaml.UnmarshalWithOptions(data, &raw, yaml.ReferenceDirs(dir)); err != nil {
		return nil, fmt.Errorf("looking for version in config file %s\n%s", path, yaml.FormatError(err, true, true))
	}

	oldConfig, err := parseCurrentVersion(dir, data, raw.Version)
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

func parseCurrentVersion(dir string, data []byte, version any) (any, error) {
	options := []yaml.DecodeOption{yaml.Strict(), yaml.ReferenceDirs(dir)}

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
		var cfg v2.Config
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

func ValidatePathInDirectory(path, allowedDir string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("empty path")
	}

	cleanPath := filepath.Clean(path)

	if cleanPath == "" || cleanPath == "." {
		return "", fmt.Errorf("empty or invalid path")
	}

	if filepath.IsAbs(cleanPath) && allowedDir == "" {
		if strings.Contains(path, "..") {
			return "", fmt.Errorf("path contains directory traversal sequences")
		}
		return cleanPath, nil
	}

	if allowedDir == "" {
		if strings.HasPrefix(cleanPath, "..") {
			return "", fmt.Errorf("path contains directory traversal sequences")
		}
		return cleanPath, nil
	}

	cleanAllowedDir := filepath.Clean(allowedDir)
	absAllowedDir, err := filepath.Abs(cleanAllowedDir)
	if err != nil {
		return "", fmt.Errorf("invalid allowed directory: %w", err)
	}

	var targetPath string
	if filepath.IsAbs(cleanPath) {
		targetPath = cleanPath
	} else {
		targetPath = filepath.Join(absAllowedDir, cleanPath)
	}

	absTargetPath, err := filepath.Abs(targetPath)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}

	relPath, err := filepath.Rel(absAllowedDir, absTargetPath)
	if err != nil {
		return "", fmt.Errorf("cannot determine relative path: %w", err)
	}

	if strings.HasPrefix(relPath, "..") {
		return "", fmt.Errorf("path outside allowed directory: %s", path)
	}

	return absTargetPath, nil
}
