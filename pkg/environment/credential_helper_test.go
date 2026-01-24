package environment

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewCredentialHelperProvider(t *testing.T) {
	t.Parallel()

	p := NewCredentialHelperProvider("echo", "test-token")
	assert.Equal(t, "echo", p.command)
	assert.Equal(t, []string{"test-token"}, p.args)
}

func TestCredentialHelperProvider_Get(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		command   string
		args      []string
		envName   string
		wantValue string
		wantFound bool
	}{
		{"ignores non-DOCKER_TOKEN vars", "echo", []string{"test-token"}, "OTHER_VAR", "", false},
		{"success", "echo", []string{"my-secret-token"}, DockerDesktopTokenEnv, "my-secret-token", true},
		{"trims whitespace", "echo", []string{"  token-with-spaces  "}, DockerDesktopTokenEnv, "token-with-spaces", true},
		{"empty output", "echo", []string{""}, DockerDesktopTokenEnv, "", false},
		{"command fails", "false", nil, DockerDesktopTokenEnv, "", false},
		{"command not found", "nonexistent-command-12345", nil, DockerDesktopTokenEnv, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := NewCredentialHelperProvider(tt.command, tt.args...)
			value, found := p.Get(t.Context(), tt.envName)

			assert.Equal(t, tt.wantFound, found)
			assert.Equal(t, tt.wantValue, value)
		})
	}
}
