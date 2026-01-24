package config

import (
	"cmp"
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"

	"github.com/docker/cagent/pkg/reference"
	"github.com/docker/cagent/pkg/userconfig"
)

//go:embed default-agent.yaml
var defaultAgent []byte

// ResolveAlias resolves an agent reference and returns the alias if it exists and has options.
// Returns nil if the reference is not an alias or doesn't have options.
func ResolveAlias(agentFilename string) *userconfig.Alias {
	agentFilename = cmp.Or(agentFilename, "default")

	cfg, err := userconfig.Load()
	if err != nil {
		return nil
	}

	alias, ok := cfg.GetAlias(agentFilename)
	if !ok || !alias.HasOptions() {
		return nil
	}

	return alias
}

// GetUserSettings returns the global user settings from the config file.
// Returns an empty Settings if the config file doesn't exist or has no settings.
func GetUserSettings() *userconfig.Settings {
	cfg, err := userconfig.Load()
	if err != nil {
		return &userconfig.Settings{}
	}
	return cfg.GetSettings()
}

// ResolveSources resolves an agent file reference (local file, URL, or OCI image) to sources
// For OCI references, always checks remote for updates but falls back to local cache if offline
func ResolveSources(agentsPath string) (Sources, error) {
	// Handle URL references first (before resolve() which converts to absolute path)
	if IsURLReference(agentsPath) {
		return map[string]Source{
			agentsPath: NewURLSource(agentsPath),
		}, nil
	}

	resolvedPath, err := resolve(agentsPath)
	if err != nil {
		if IsOCIReference(agentsPath) {
			return map[string]Source{
				reference.OciRefToFilename(agentsPath): NewOCISource(agentsPath),
			}, nil
		}
		return nil, err
	}

	if resolvedPath == "default" {
		return map[string]Source{
			"default": NewBytesSource("default", defaultAgent),
		}, nil
	}

	if isLocalFile(resolvedPath) {
		return map[string]Source{
			fileNameWithoutExt(resolvedPath): NewFileSource(resolvedPath),
		}, nil
	}

	if dirExists(resolvedPath) {
		sources := make(Sources)
		entries, err := os.ReadDir(resolvedPath)
		if err != nil {
			return nil, fmt.Errorf("reading agents directory %s: %w", resolvedPath, err)
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(entry.Name()))
			if ext != ".yaml" && ext != ".yml" {
				continue
			}
			a := filepath.Join(resolvedPath, entry.Name())
			sources[fileNameWithoutExt(a)], err = Resolve(a)
			if err != nil {
				return nil, err
			}
		}
		return sources, nil
	}

	return map[string]Source{
		reference.OciRefToFilename(resolvedPath): NewOCISource(resolvedPath),
	}, nil
}

// Resolve resolves an agent file reference (local file, URL, or OCI image) to a source
// For OCI references, always checks remote for updates but falls back to local cache if offline
func Resolve(agentFilename string) (Source, error) {
	// Handle URL references first (before resolve() which converts to absolute path)
	if IsURLReference(agentFilename) {
		return NewURLSource(agentFilename), nil
	}

	resolvedPath, err := resolve(agentFilename)
	if err != nil {
		if IsOCIReference(agentFilename) {
			return NewOCISource(agentFilename), nil
		}
		return nil, err
	}

	if resolvedPath == "default" {
		return NewBytesSource(resolvedPath, defaultAgent), nil
	}
	if isLocalFile(resolvedPath) {
		return NewFileSource(resolvedPath), nil
	}
	return NewOCISource(resolvedPath), nil
}

// resolve resolves an agent reference, handling aliases and defaults
func resolve(agentFilename string) (string, error) {
	agentFilename = cmp.Or(agentFilename, "default")

	// Try to resolve as an alias first
	if cfg, err := userconfig.Load(); err == nil {
		if alias, ok := cfg.GetAlias(agentFilename); ok {
			slog.Debug("Resolved alias", "alias", agentFilename, "path", alias.Path)
			agentFilename = alias.Path
		}
	}

	// "default" is either a user defined alias or the default (embedded) agent
	if agentFilename == "default" {
		return "default", nil
	}

	// Don't convert OCI references or URLs to absolute paths
	if IsOCIReference(agentFilename) || IsURLReference(agentFilename) {
		return agentFilename, nil
	}

	abs, err := filepath.Abs(agentFilename)
	if err != nil {
		return "", err
	}

	return abs, nil
}

// fileExists checks if a file exists at the given path
func fileExists(path string) bool {
	s, err := os.Stat(path)
	exists := err == nil && !s.IsDir()
	return exists
}

// dirExists checks if a directory exists at the given path
func dirExists(path string) bool {
	s, err := os.Stat(path)
	exists := err == nil && s.IsDir()
	return exists
}

// IsOCIReference checks if the input is a valid OCI reference
func IsOCIReference(input string) bool {
	if isLocalFile(input) {
		return false
	}
	_, err := name.ParseReference(input)
	return err == nil
}

// isLocalFile checks if the input is a local file
func isLocalFile(input string) bool {
	ext := strings.ToLower(filepath.Ext(input))
	// Check for YAML file extensions or file descriptors
	if ext == ".yaml" || ext == ".yml" || strings.HasPrefix(input, "/dev/fd/") {
		return true
	}
	// Check if it exists as a file on disk
	return fileExists(input)
}

func fileNameWithoutExt(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	return strings.TrimSuffix(base, ext)
}
