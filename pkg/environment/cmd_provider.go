package environment

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os/exec"
	"strings"
)

// runCommand executes a command and returns its trimmed stdout.
// Returns ("", false) if the command fails or is not found.
func runCommand(ctx context.Context, logLabel, name string, args ...string) (string, bool) {
	var stdout, stderr bytes.Buffer

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		slog.Debug("Failed to find secret in "+logLabel, "error", err)
		return "", false
	}

	return strings.TrimSpace(stdout.String()), true
}

// lookupBinary checks if a binary is available on the system PATH.
// Returns a non-nil error if the binary is not found.
func lookupBinary(name string, notFoundErr error) error {
	path, err := exec.LookPath(name)
	if err != nil && !errors.Is(err, exec.ErrNotFound) {
		slog.Warn("failed to lookup `"+name+"` binary", "error", err)
	}
	if path == "" {
		return notFoundErr
	}
	return nil
}
