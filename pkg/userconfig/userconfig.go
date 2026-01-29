// Package userconfig provides user-level configuration for cagent.
// This configuration is stored in ~/.config/cagent/config.yaml and contains
// user preferences like aliases.
package userconfig

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	"github.com/goccy/go-yaml"
	"github.com/natefinch/atomic"

	"github.com/docker/cagent/pkg/paths"
)

// Alias represents an alias configuration with optional runtime settings
type Alias struct {
	// Path is the agent file path or OCI reference
	Path string `yaml:"path"`
	// Yolo enables auto-approve mode for all tool calls
	Yolo bool `yaml:"yolo,omitempty"`
	// Model overrides the agent's model (format: [agent=]provider/model)
	Model string `yaml:"model,omitempty"`
	// HideToolResults hides tool call results in the TUI
	HideToolResults bool `yaml:"hide_tool_results,omitempty"`
}

// HasOptions returns true if the alias has any runtime options set
func (a *Alias) HasOptions() bool {
	return a != nil && (a.Yolo || a.Model != "" || a.HideToolResults)
}

// Settings represents global user settings
type Settings struct {
	// HideToolResults hides tool call results in the TUI by default
	HideToolResults bool `yaml:"hide_tool_results,omitempty"`
	// Theme is the default theme reference (e.g., "dark", "light")
	// Theme files are loaded from ~/.cagent/themes/<theme>.yaml
	Theme string `yaml:"theme,omitempty"`
}

// CredentialHelper contains configuration for a credential helper command
// that retrieves Docker credentials (DOCKER_TOKEN) from an external source.
type CredentialHelper struct {
	// Command is the CLI command to execute to retrieve the Docker token.
	// The command should output the token on stdout.
	Command string   `yaml:"command,omitempty"`
	Args    []string `yaml:"args,omitempty"`
}

// CurrentVersion is the current version of the user config format
const CurrentVersion = "v1"

// Config represents the user-level cagent configuration
type Config struct {
	// mu protects concurrent access to the Aliases map.
	// Config methods may be called from parallel tests or goroutines.
	mu sync.Mutex

	// Version is the config format version
	Version string `yaml:"version,omitempty"`
	// ModelsGateway is the default models gateway URL
	ModelsGateway string `yaml:"models_gateway,omitempty"`
	// Aliases maps alias names to alias configurations
	Aliases map[string]*Alias `yaml:"aliases,omitempty"`
	// Settings contains global user settings
	Settings *Settings `yaml:"settings,omitempty"`
	// CredentialHelper configures an external command to retrieve Docker credentials
	CredentialHelper *CredentialHelper `yaml:"credential_helper,omitempty"`
}

// Path returns the path to the config file
func Path() string {
	return filepath.Join(paths.GetConfigDir(), "config.yaml")
}

// legacyAliasesPath returns the path to the legacy aliases.yaml file
func legacyAliasesPath() string {
	return filepath.Join(paths.GetConfigDir(), "aliases.yaml")
}

// Load loads the user configuration from the config file.
// If the config file doesn't exist but a legacy aliases.yaml does,
// the aliases are migrated to the new config file.
func Load() (*Config, error) {
	return loadFrom(Path(), legacyAliasesPath())
}

func loadFrom(configPath, legacyPath string) (*Config, error) {
	config, err := readConfig(configPath)
	if err != nil {
		return nil, err
	}

	// Try migrating from legacy file if no aliases exist yet
	if len(config.Aliases) == 0 && config.migrateFromLegacy(legacyPath) {
		if err := config.saveTo(configPath); err != nil {
			return nil, fmt.Errorf("failed to save migrated config: %w", err)
		}
	}

	return config, nil
}

// readConfig reads and parses the config file, returning an empty config if file doesn't exist.
func readConfig(configPath string) (*Config, error) {
	config := &Config{Aliases: make(map[string]*Alias)}

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return config, nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	if err := yaml.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	if config.Aliases == nil {
		config.Aliases = make(map[string]*Alias)
	}

	return config, nil
}

// migrateFromLegacy migrates aliases from the legacy aliases.yaml file.
// Returns true if any aliases were migrated.
// After successful migration, the legacy file is deleted.
func (c *Config) migrateFromLegacy(legacyPath string) bool {
	if legacyPath == "" {
		return false
	}

	data, err := os.ReadFile(legacyPath)
	if err != nil {
		return false
	}

	var legacy map[string]string
	if err := yaml.Unmarshal(data, &legacy); err != nil {
		slog.Warn("Failed to parse legacy aliases file", "path", legacyPath, "error", err)
		return false
	}

	if len(legacy) == 0 {
		return false
	}

	// Protect concurrent writes to the Aliases map while migrating
	// legacy aliases. This avoids concurrent map write panics if
	// the config is accessed by multiple goroutines.
	c.mu.Lock()
	defer c.mu.Unlock()

	for name, path := range legacy {
		c.Aliases[name] = &Alias{Path: path}
	}

	slog.Info("Migrated aliases from legacy file", "path", legacyPath, "count", len(legacy))

	if err := os.Remove(legacyPath); err != nil {
		slog.Warn("Failed to remove legacy aliases file", "path", legacyPath, "error", err)
	}

	return true
}

// Save saves the configuration to the config file
func (c *Config) Save() error {
	return c.saveTo(Path())
}

func (c *Config) saveTo(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Ensure version is always set to current version when saving
	c.Version = CurrentVersion

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	return atomic.WriteFile(path, bytes.NewReader(data))
}

// GetAlias retrieves the alias configuration for a given name.
//
// This method is safe for concurrent use. Reads from the Aliases map
// are protected by a mutex to avoid concurrent read/write panics when
// aliases are accessed while being modified.
func (c *Config) GetAlias(name string) (*Alias, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	alias, ok := c.Aliases[name]
	return alias, ok
}

// validAliasNameRegex matches valid alias names: alphanumeric characters, hyphens, and underscores.
// Must start with an alphanumeric character.
var validAliasNameRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

// ValidateAliasName checks if an alias name is valid.
// Valid names must:
// - Not be empty
// - Start with an alphanumeric character
// - Contain only alphanumeric characters, hyphens, and underscores
// - Not contain path separators or special characters
func ValidateAliasName(name string) error {
	if name == "" {
		return errors.New("alias name cannot be empty")
	}
	if !validAliasNameRegex.MatchString(name) {
		return fmt.Errorf("invalid alias name %q: must start with a letter or digit and contain only letters, digits, hyphens, and underscores", name)
	}
	return nil
}

// SetAlias creates or updates an alias.
// Returns an error if the alias name or alias configuration is invalid.
//
// This method is safe for concurrent use. Writes to the Aliases map
// are protected by a mutex to avoid concurrent map write panics when
// aliases are modified from multiple goroutines.
func (c *Config) SetAlias(name string, alias *Alias) error {
	if err := ValidateAliasName(name); err != nil {
		return err
	}
	if alias == nil || alias.Path == "" {
		return errors.New("agent path cannot be empty")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.Aliases[name] = alias
	return nil
}

// DeleteAlias removes an alias by name.
// It returns true if the alias existed.
//
// This method is safe for concurrent use. Access to the Aliases map
// is protected by a mutex to prevent concurrent map read/write panics
// when called from parallel tests or goroutines.
func (c *Config) DeleteAlias(name string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.Aliases[name]; exists {
		delete(c.Aliases, name)
		return true
	}
	return false
}

// GetSettings returns the global settings, or an empty Settings if not set
func (c *Config) GetSettings() *Settings {
	if c.Settings == nil {
		return &Settings{}
	}
	return c.Settings
}
