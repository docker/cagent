package aliases

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/goccy/go-yaml"

	"github.com/docker/cagent/pkg/paths"
)

// Aliases represents the aliases configuration
type Aliases map[string]string

func aliasesFilePath() string {
	return filepath.Join(paths.GetConfigDir(), "aliases.yaml")
}

// Load loads aliases from the configuration file
func Load() (*Aliases, error) {
	return loadFrom(aliasesFilePath())
}

// loadFrom loads aliases from a specific file path
func loadFrom(path string) (*Aliases, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Aliases{}, nil
		}
		return nil, fmt.Errorf("failed to read aliases file: %w", err)
	}

	var aliases Aliases
	if err := yaml.Unmarshal(data, &aliases); err != nil {
		return nil, fmt.Errorf("failed to parse aliases file: %w", err)
	}

	return &aliases, nil
}

// Save saves aliases to the configuration file
func (s *Aliases) Save() error {
	return s.saveTo(aliasesFilePath())
}

// saveTo saves aliases to a specific file path
func (s *Aliases) saveTo(path string) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	data, err := yaml.Marshal(s)
	if err != nil {
		return fmt.Errorf("failed to marshal aliases: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("failed to write aliases file: %w", err)
	}

	return nil
}

// Get retrieves the agent path for an alias
func (s *Aliases) Get(name string) (string, bool) {
	path, ok := (*s)[name]
	return path, ok
}

// Set creates or updates an alias
func (s *Aliases) Set(name, agentPath string) {
	(*s)[name] = agentPath
}

// Delete removes an alias
func (s *Aliases) Delete(name string) bool {
	if _, exists := (*s)[name]; exists {
		delete(*s, name)
		return true
	}
	return false
}

// List returns all aliases
func (s *Aliases) List() map[string]string {
	return *s
}
