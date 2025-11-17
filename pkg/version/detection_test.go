package version

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetect_ExplicitVersion(t *testing.T) {
	t.Parallel()

	opts := DetectOptions{
		ExplicitVersion: "v1.2.3",
		AgentName:       "test-agent",
	}

	info, err := Detect(opts)
	require.NoError(t, err)

	assert.Equal(t, "v1.2.3", info.Version)
	assert.Equal(t, SourceExplicit, info.Source)
	assert.NotEmpty(t, info.CreatedAt)
}

func TestDetect_FirstVersion(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	opts := DetectOptions{
		AgentName:  "test-agent",
		WorkingDir: tempDir,
	}

	info, err := Detect(opts)
	require.NoError(t, err)

	assert.Equal(t, "v1.0.0", info.Version)
	assert.Equal(t, SourceCounter, info.Source)
	assert.NotEmpty(t, info.CreatedAt)
}

func TestDetect_IncrementedVersion(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	agentName := "test-agent"

	// Update version state to simulate existing version
	err := UpdateVersion(agentName, "v1.0.5", tempDir)
	require.NoError(t, err)

	opts := DetectOptions{
		AgentName:  agentName,
		WorkingDir: tempDir,
	}

	info, err := Detect(opts)
	require.NoError(t, err)

	assert.Equal(t, "v1.0.6", info.Version)
	assert.Equal(t, SourceCounter, info.Source)
	assert.NotEmpty(t, info.CreatedAt)
}

func TestDetect_MissingAgentName(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	opts := DetectOptions{
		// Missing AgentName
		WorkingDir: tempDir,
	}

	_, err := Detect(opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agent name is required")
}

func TestDetect_ExplicitOverridesCounter(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	agentName := "test-agent"

	// Update version state to simulate existing version
	err := UpdateVersion(agentName, "v1.0.5", tempDir)
	require.NoError(t, err)

	opts := DetectOptions{
		ExplicitVersion: "v9.9.9",
		AgentName:       agentName,
		WorkingDir:      tempDir,
	}

	info, err := Detect(opts)
	require.NoError(t, err)

	// Explicit should override counter
	assert.Equal(t, "v9.9.9", info.Version)
	assert.Equal(t, SourceExplicit, info.Source)
}

func TestIsValidVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		version string
		valid   bool
	}{
		{"v1.2.3", true},
		{"1.0.0", true},
		{"release-1.0", true},
		{"commit-abc123def", true},
		{"snapshot-20240101-120000", true},
		{"", false},
		{"version with spaces", false},
		{"version\nwith\nnewlines", false},
		{"version\twith\ttabs", false},
		{"version\rwith\rcarriage", false},
	}

	for _, test := range tests {
		t.Run(test.version, func(t *testing.T) {
			result := IsValidVersion(test.version)
			assert.Equal(t, test.valid, result, "version: %q", test.version)
		})
	}
}

func TestInfo_FormatForDisplay(t *testing.T) {
	t.Parallel()

	tests := []struct {
		info     Info
		expected string
	}{
		{
			info:     Info{Version: "v1.0.0", Source: SourceExplicit},
			expected: "v1.0.0 (explicit)",
		},
		{
			info:     Info{Version: "v1.0.5", Source: SourceCounter},
			expected: "v1.0.5 (auto-incremented)",
		},
	}

	for _, test := range tests {
		t.Run(string(test.info.Source), func(t *testing.T) {
			result := test.info.FormatForDisplay()
			assert.Equal(t, test.expected, result)
		})
	}
}

func TestDetect_CreatedAtFormat(t *testing.T) {
	t.Parallel()

	before := time.Now().Add(-1 * time.Second) // Allow 1 second buffer

	opts := DetectOptions{
		ExplicitVersion: "test",
		AgentName:       "test-agent",
	}

	info, err := Detect(opts)
	require.NoError(t, err)

	after := time.Now().Add(1 * time.Second) // Allow 1 second buffer

	// Parse the created_at time
	createdAt, err := time.Parse(time.RFC3339, info.CreatedAt)
	require.NoError(t, err)

	// Should be between before and after (with buffer)
	assert.True(t, createdAt.After(before) || createdAt.Equal(before),
		"createdAt %v should be after %v", createdAt, before)
	assert.True(t, createdAt.Before(after) || createdAt.Equal(after),
		"createdAt %v should be before %v", createdAt, after)

	// Should be UTC
	assert.Equal(t, time.UTC, createdAt.Location())
}

// Tests for new versioning functions

func TestIncrementVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		expected string
		hasError bool
	}{
		{"v1.0.0", "v1.0.1", false},
		{"v2.5.10", "v2.5.11", false},
		{"1.0.0", "1.0.1", false},
		{"v1.0", "", true}, // Invalid format
		{"invalid", "", true},
	}

	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			result, err := incrementVersion(test.input)
			if test.hasError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, test.expected, result)
			}
		})
	}
}

func TestUpdateVersion(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	agentName := "test-agent"
	version := "v2.0.0"

	// Update version
	err := UpdateVersion(agentName, version, tempDir)
	require.NoError(t, err)

	// Verify it was stored
	state, err := loadVersionState(tempDir)
	require.NoError(t, err)
	assert.Equal(t, version, state.Agents[agentName])

	// Update again with new version
	newVersion := "v3.0.0"
	err = UpdateVersion(agentName, newVersion, tempDir)
	require.NoError(t, err)

	// Verify update
	state, err = loadVersionState(tempDir)
	require.NoError(t, err)
	assert.Equal(t, newVersion, state.Agents[agentName])
}

func TestVersionState_MultipleAgents(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	// Add multiple agents
	agents := map[string]string{
		"agent1": "v1.0.0",
		"agent2": "v2.5.3",
		"agent3": "v0.1.0",
	}

	for name, version := range agents {
		err := UpdateVersion(name, version, tempDir)
		require.NoError(t, err)
	}

	// Verify all are stored
	state, err := loadVersionState(tempDir)
	require.NoError(t, err)

	for name, expectedVersion := range agents {
		assert.Equal(t, expectedVersion, state.Agents[name])
	}
}

// Test edge cases

func TestGetNextVersion_EmptyAgentName(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	_, err := getNextVersion("", tempDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agent name is required")
}

func TestUpdateVersion_EmptyAgentName(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	err := UpdateVersion("", "v1.0.0", tempDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agent name is required")
}
