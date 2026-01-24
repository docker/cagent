package environment

import (
	"bytes"
	"context"
	"log/slog"
	"os/exec"
	"strings"
)

// CredentialHelperProvider retrieves Docker credentials using an external CLI command
// configured in the user's global config file.
type CredentialHelperProvider struct {
	command string
	args    []string
}

// NewCredentialHelperProvider creates a new CredentialHelperProvider instance.
// The command parameter is the shell command to execute to retrieve the Docker token.
func NewCredentialHelperProvider(command string, args ...string) *CredentialHelperProvider {
	return &CredentialHelperProvider{command: command, args: args}
}

func (p *CredentialHelperProvider) Get(ctx context.Context, name string) (string, bool) {
	if name != DockerDesktopTokenEnv {
		return "", false
	}

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, p.command, p.args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		slog.Debug("Failed to get Docker token from credential helper",
			"command", p.command, "args", p.args, "error", err, "stderr", stderr.String())
		return "", false
	}

	if token := strings.TrimSpace(stdout.String()); token != "" {
		return token, true
	}

	slog.Debug("Credential helper returned empty token", "command", p.command, "args", p.args)
	return "", false
}
