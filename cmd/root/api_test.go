package root

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsOCIReference(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		// Valid OCI references
		{
			name:     "simple repository with tag",
			input:    "myregistry/myrepo:latest",
			expected: true,
		},
		{
			name:     "repository with digest",
			input:    "myregistry/myrepo@sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			expected: true,
		},
		{
			name:     "docker hub image",
			input:    "nginx:latest",
			expected: true,
		},
		{
			name:     "fully qualified registry",
			input:    "ghcr.io/docker/cagent:v1.0.0",
			expected: true,
		},
		{
			name:     "registry with port",
			input:    "localhost:5000/myimage:tag",
			expected: true,
		},

		// Local files - NOT OCI references
		{
			name:     "yaml file",
			input:    "agent.yaml",
			expected: false,
		},
		{
			name:     "yml file",
			input:    "config.yml",
			expected: false,
		},
		{
			name:     "yaml file with path",
			input:    "/path/to/agent.yaml",
			expected: false,
		},
		{
			name:     "file descriptor",
			input:    "/dev/fd/3",
			expected: false,
		},

		// Invalid inputs - NOT valid OCI references
		{
			name:     "empty string",
			input:    "",
			expected: false,
		},
		{
			name:     "typo in yaml filename",
			input:    "my-agnt.yaml",
			expected: false,
		},
		{
			name:     "invalid OCI reference with too many colons",
			input:    "invalid:reference:with:too:many:colons",
			expected: false,
		},
		{
			name:     "random string",
			input:    "not-a-valid-reference!!!",
			expected: false,
		},
		{
			name:     "non-existent directory path that looks like OCI ref",
			input:    "/path/to/agents",
			expected: true, // Parses as valid OCI ref if path doesn't exist
		},
		{
			name:     "existing directory",
			input:    tmpDir,
			expected: false, // Existing paths are NOT OCI references
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isOCIReference(tt.input)
			assert.Equal(t, tt.expected, result, "isOCIReference(%q) = %v, want %v", tt.input, result, tt.expected)
		})
	}
}
