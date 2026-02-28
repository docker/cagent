package sandbox

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseSandboxPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		wantPath string
		wantMode string
	}{
		{input: ".", wantPath: ".", wantMode: "rw"},
		{input: "/tmp", wantPath: "/tmp", wantMode: "rw"},
		{input: "./src", wantPath: "./src", wantMode: "rw"},
		{input: "/tmp:ro", wantPath: "/tmp", wantMode: "ro"},
		{input: "./config:ro", wantPath: "./config", wantMode: "ro"},
		{input: "/data:rw", wantPath: "/data", wantMode: "rw"},
		{input: "./secrets:ro", wantPath: "./secrets", wantMode: "ro"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			path, mode := ParseSandboxPath(tt.input)
			assert.Equal(t, tt.wantPath, path)
			assert.Equal(t, tt.wantMode, mode)
		})
	}
}

func TestIsValidEnvVarName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		valid bool
	}{
		{"HOME", true},
		{"USER", true},
		{"PATH", true},
		{"_private", true},
		{"MY_VAR_123", true},
		{"a", true},
		{"A", true},
		{"_", true},
		{"", false},
		{"123", false},
		{"1VAR", false},
		{"VAR-NAME", false},
		{"VAR.NAME", false},
		{"VAR NAME", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := IsValidEnvVarName(tt.name)
			assert.Equal(t, tt.valid, result, "IsValidEnvVarName(%q)", tt.name)
		})
	}
}

func TestIsProcessRunning(t *testing.T) {
	t.Parallel()

	// Current process should be running
	assert.True(t, isProcessRunning(os.Getpid()), "Current process should be running")

	// Non-existent PID should not be running (using a very high PID unlikely to exist)
	assert.False(t, isProcessRunning(999999999), "Very high PID should not be running")
}
