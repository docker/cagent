package sandbox

import (
	"context"
	"time"

	"github.com/docker/cagent/pkg/tools"
)

// Runner is a pluggable interface for sandbox execution backends.
// Implementations handle command execution in isolated environments
// (Docker containers, Kubernetes pods, etc.).
type Runner interface {
	// RunCommand executes a command synchronously and returns the result.
	// The timeoutCtx carries the command timeout; ctx is the parent context.
	// cwd is the working directory inside the sandbox.
	// timeout is the original duration for formatting timeout messages.
	RunCommand(timeoutCtx, ctx context.Context, command, cwd string, timeout time.Duration) *tools.ToolCallResult

	// Start initializes the runner (e.g., discover pod, start container).
	Start(ctx context.Context) error
	// Stop cleans up the runner (e.g., stop container).
	Stop(ctx context.Context) error
}
