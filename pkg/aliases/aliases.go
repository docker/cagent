package aliases

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/goccy/go-yaml"

	"github.com/docker/cagent/pkg/paths"
)

// Aliases represents the aliases configuration
type Aliases struct {
	Aliases map[string]string `yaml:"aliases"`
}

// GetAliasesFilePath returns the path to the aliases file
func GetAliasesFilePath() string {
	return filepath.Join(paths.GetConfigDir(), "aliases.yaml")
}

// Load loads aliases from the configuration file
func Load() (*Aliases, error) {
	return LoadFrom(GetAliasesFilePath())
}

// LoadFrom loads aliases from a specific file path
func LoadFrom(path string) (*Aliases, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Aliases{Aliases: make(map[string]string)}, nil
		}
		return nil, fmt.Errorf("failed to read aliases file: %w", err)
	}

	var s Aliases
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("failed to parse aliases file: %w", err)
	}

	if s.Aliases == nil {
		s.Aliases = make(map[string]string)
	}

	return &s, nil
}

// Save saves aliases to the configuration file
func (s *Aliases) Save() error {
	return s.SaveTo(GetAliasesFilePath())
}

// SaveTo saves aliases to a specific file path
func (s *Aliases) SaveTo(path string) error {
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
	path, ok := s.Aliases[name]
	return path, ok
}

// Set creates or updates an alias
func (s *Aliases) Set(name, agentPath string) {
	s.Aliases[name] = agentPath
}

// Delete removes an alias
func (s *Aliases) Delete(name string) bool {
	if _, exists := s.Aliases[name]; exists {
		delete(s.Aliases, name)
		return true
	}
	return false
}

// List returns all aliases
func (s *Aliases) List() map[string]string {
	return s.Aliases
}
