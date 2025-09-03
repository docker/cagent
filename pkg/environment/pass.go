package environment

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
)

// PassProvider is a provider that retrieves secrets using the `pass` password
// manager.
type PassProvider struct{}

type ErrPassNotAvailable struct{}

func (ErrPassNotAvailable) Error() string {
	return "pass is not installed"
}

// NewPassProvider creates a new PassProvider instance.
func NewPassProvider() (*PassProvider, error) {
	path, err := exec.LookPath("pass")
	if err != nil && !errors.Is(err, exec.ErrNotFound) {
		slog.Warn("failed to lookup `pass` binary", "error", err)
	}
	if path == "" {
		return nil, ErrPassNotAvailable{}
	}
	return &PassProvider{}, nil
}

// Get retrieves the value of a secret by its name using the `pass` CLI.
// The name corresponds to the path in the `pass` store.
func (p *PassProvider) Get(ctx context.Context, name string) (string, error) {
	cmd := exec.CommandContext(ctx, "pass", "show", name)

	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("failed to retrieve secret with `pass`: %w, stderr: %v", err, stderr.String())
	}

	return strings.TrimSpace(out.String()), nil
}
