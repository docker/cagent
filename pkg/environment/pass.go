package environment

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os/exec"
	"strings"
)

// PassProvider is a provider that retrieves secrets using the `pass` password
// manager.
type PassProvider struct{}

type PassNotAvailableError struct{}

func (PassNotAvailableError) Error() string {
	return "pass is not installed"
}

// NewPassProvider creates a new PassProvider instance.
func NewPassProvider() (*PassProvider, error) {
	path, err := exec.LookPath("pass")
	if err != nil && !errors.Is(err, exec.ErrNotFound) {
		slog.Warn("failed to lookup `pass` binary", "error", err)
	}
	if path == "" {
		return nil, PassNotAvailableError{}
	}
	return &PassProvider{}, nil
}

// Get retrieves the value of a secret by its name using the `pass` CLI.
// The name corresponds to the path in the `pass` store.
func (p *PassProvider) Get(ctx context.Context, name string) (string, bool) {
	cmd := exec.CommandContext(ctx, "pass", "show", name)

	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		// Ignore error
		slog.Debug("Failed to find secret in pass", "error", err)
		return "", false
	}

	return strings.TrimSpace(out.String()), true
}
