package version

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DetectionSource represents the source of version information
type DetectionSource string

const (
	SourceExplicit DetectionSource = "explicit"
	SourceCounter  DetectionSource = "counter"
)

// Info contains version information and its source
type Info struct {
	Version   string          `json:"version"`
	CreatedAt string          `json:"created_at"`
	Source    DetectionSource `json:"source"`
}

// DetectOptions contains options for version detection
type DetectOptions struct {
	// ExplicitVersion if provided, will be used as the version
	ExplicitVersion string
	// AgentName is the name of the agent for counter tracking
	AgentName string
	// WorkingDir is the directory for storing version state
	WorkingDir string
}

// State represents the persisted version counter state
type State struct {
	Agents map[string]string `json:"agents"`
}

// Detect attempts to determine version information using the following hierarchy:
// 1. Explicit version (if provided)
// 2. Counter-based version (auto-incremented)
func Detect(opts DetectOptions) (Info, error) {
	createdAt := time.Now().UTC().Format(time.RFC3339)

	// Use explicit version if provided
	if opts.ExplicitVersion != "" {
		return Info{
			Version:   opts.ExplicitVersion,
			CreatedAt: createdAt,
			Source:    SourceExplicit,
		}, nil
	}

	// Generate next counter-based version
	nextVersion, err := getNextVersion(opts.AgentName, opts.WorkingDir)
	if err != nil {
		return Info{}, fmt.Errorf("failed to generate next version: %w", err)
	}

	return Info{
		Version:   nextVersion,
		CreatedAt: createdAt,
		Source:    SourceCounter,
	}, nil
}

// getNextVersion returns the next version for the given agent
func getNextVersion(agentName, workingDir string) (string, error) {
	if agentName == "" {
		return "", fmt.Errorf("agent name is required for version generation")
	}

	state, err := loadVersionState(workingDir)
	if err != nil {
		return "", fmt.Errorf("failed to load version state: %w", err)
	}

	currentVersion, exists := state.Agents[agentName]
	if !exists {
		// First version for this agent
		return "v1.0.0", nil
	}

	nextVersion, err := incrementVersion(currentVersion)
	if err != nil {
		return "", fmt.Errorf("failed to increment version %s: %w", currentVersion, err)
	}

	return nextVersion, nil
}

// UpdateVersion updates the stored version for an agent
func UpdateVersion(agentName, version, workingDir string) error {
	if agentName == "" {
		return fmt.Errorf("agent name is required")
	}

	state, err := loadVersionState(workingDir)
	if err != nil {
		return fmt.Errorf("failed to load version state: %w", err)
	}

	state.Agents[agentName] = version
	return saveVersionState(state, workingDir)
}

// loadVersionState loads the version state from disk
func loadVersionState(workingDir string) (*State, error) {
	stateFile := getStateFilePath(workingDir)

	if err := os.MkdirAll(filepath.Dir(stateFile), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create state directory: %w", err)
	}

	if _, err := os.Stat(stateFile); os.IsNotExist(err) {
		return &State{Agents: make(map[string]string)}, nil
	}

	data, err := os.ReadFile(stateFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read state file: %w", err)
	}

	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to parse state file: %w", err)
	}

	if state.Agents == nil {
		state.Agents = make(map[string]string)
	}

	return &state, nil
}

// saveVersionState saves the version state to disk
func saveVersionState(state *State, workingDir string) error {
	stateFile := getStateFilePath(workingDir)

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	return os.WriteFile(stateFile, data, 0o644)
}

// getStateFilePath returns the path to the version state file
func getStateFilePath(workingDir string) string {
	if workingDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, ".cagent", "version-state.json")
		}
		return ".cagent/version-state.json"
	}
	return filepath.Join(workingDir, ".cagent", "version-state.json")
}

// incrementVersion increments a semantic version string
func incrementVersion(version string) (string, error) {
	// Check if version has 'v' prefix
	hasPrefix := strings.HasPrefix(version, "v")
	v := strings.TrimPrefix(version, "v")

	// Split version into parts
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("invalid version format: %s", version)
	}

	// Parse patch version and increment
	var major, minor, patch int
	if n, err := fmt.Sscanf(v, "%d.%d.%d", &major, &minor, &patch); err != nil || n != 3 {
		return "", fmt.Errorf("failed to parse version: %s", version)
	}

	patch++

	// Preserve original prefix style
	if hasPrefix {
		return fmt.Sprintf("v%d.%d.%d", major, minor, patch), nil
	}
	return fmt.Sprintf("%d.%d.%d", major, minor, patch), nil
}

// IsValidVersion checks if a version string follows acceptable patterns
func IsValidVersion(version string) bool {
	if version == "" {
		return false
	}

	// Allow various common version patterns:
	// - Semantic versions: v1.2.3, 1.2.3
	// - Git tags: release-1.0, v2.0.0-beta.1
	// - Commit-based: commit-abc123def
	// - Snapshot: snapshot-20240101-120000

	// For now, just check it's not empty and doesn't contain invalid characters
	invalidChars := []string{"\n", "\r", "\t", " "}
	for _, char := range invalidChars {
		if strings.Contains(version, char) {
			return false
		}
	}

	return true
}

// FormatForDisplay formats version info for user display
func (info Info) FormatForDisplay() string {
	switch info.Source {
	case SourceExplicit:
		return fmt.Sprintf("%s (explicit)", info.Version)
	case SourceCounter:
		return fmt.Sprintf("%s (auto-incremented)", info.Version)
	default:
		return info.Version
	}
}
