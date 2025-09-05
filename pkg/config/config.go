package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	v0 "github.com/docker/cagent/pkg/config/v0"
	v1 "github.com/docker/cagent/pkg/config/v1"
	latest "github.com/docker/cagent/pkg/config/v2"
	"gopkg.in/yaml.v3"
)

// LoadConfigSecure loads the configuration from a file with path validation
func LoadConfigSecure(path, allowedDir string) (*latest.Config, error) {
	validatedPath, err := ValidatePathInDirectory(path, allowedDir)
	if err != nil {
		return nil, fmt.Errorf("path validation failed: %w", err)
	}

	return loadConfig(validatedPath)
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

func loadConfig(path string) (*latest.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	oldConfig, err := parseCurrentVersion(data, raw["version"])
	if err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	config, err := migrateToLatestConfig(oldConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to migrate config: %w", err)
	}

	if err := validateConfig(&config); err != nil {
		return nil, err
	}

	return &config, nil
}

func parseCurrentVersion(data []byte, version any) (any, error) {
	switch version {
	case nil, "0", 0:
		return v0.Load(data)
	case "1", 1:
		return v1.Load(data)
	default:
		return latest.Load(data)
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
			if _, exists := cfg.Models[modelName]; !exists {
				if provider, model, ok := strings.Cut(modelName, "/"); ok {
					autoRegisterModel(cfg, provider, model)
					continue
				}

				return fmt.Errorf("agent '%s' references non-existent model '%s'", agentName, modelName)
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

func autoRegisterModel(cfg *latest.Config, provider, model string) {
	if cfg.Models == nil {
		cfg.Models = make(map[string]latest.ModelConfig)
	}

	cfg.Models[provider+"/"+model] = latest.ModelConfig{
		Provider: provider,
		Model:    model,
	}
}

func boolPtr(b bool) *bool {
	return &b
}
