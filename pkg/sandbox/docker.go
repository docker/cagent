package sandbox

import (
	"bytes"
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/tools"
)

const (
	// sandboxLabelKey is the label used to identify cagent sandbox containers.
	sandboxLabelKey = "com.docker.cagent.sandbox"
	// sandboxLabelPID stores the PID of the cagent process that created the container.
	sandboxLabelPID = "com.docker.cagent.sandbox.pid"
)

// DockerRunner handles command execution in a Docker container sandbox.
type DockerRunner struct {
	config      *latest.SandboxConfig
	workingDir  string
	env         []string
	containerID string
	mu          sync.Mutex
}

// Verify interface compliance.
var _ Runner = (*DockerRunner)(nil)

// NewDockerRunner creates a new Docker sandbox runner.
// It cleans up any orphaned containers from previous cagent runs.
func NewDockerRunner(config *latest.SandboxConfig, workingDir string, env []string) *DockerRunner {
	cleanupOrphanedSandboxContainers()

	return &DockerRunner{
		config:     config,
		workingDir: workingDir,
		env:        env,
	}
}

// cleanupOrphanedSandboxContainers removes sandbox containers from previous cagent processes
// that are no longer running. This handles cases where cagent was killed or crashed.
func cleanupOrphanedSandboxContainers() {
	cmd := exec.Command("docker", "ps", "-q", "--filter", "label="+sandboxLabelKey)
	output, err := cmd.Output()
	if err != nil {
		return // Docker not available or no containers
	}

	containerIDs := strings.Fields(string(output))
	currentPID := os.Getpid()

	for _, containerID := range containerIDs {
		pid := getContainerOwnerPID(containerID)
		if pid == 0 || pid == currentPID || isProcessRunning(pid) {
			continue
		}

		slog.Debug("Cleaning up orphaned sandbox container", "container", containerID, "pid", pid)
		stopCmd := exec.Command("docker", "stop", "-t", "1", containerID)
		_ = stopCmd.Run()
	}
}

// getContainerOwnerPID returns the PID that created the container, or 0 if unknown.
func getContainerOwnerPID(containerID string) int {
	cmd := exec.Command("docker", "inspect", "-f",
		"{{index .Config.Labels \""+sandboxLabelPID+"\"}}", containerID)
	output, err := cmd.Output()
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(output)))
	return pid
}

// isProcessRunning checks if a process with the given PID is still running.
func isProcessRunning(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, FindProcess always succeeds, so we need to send signal 0
	// to check if the process actually exists
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

// RunCommand executes a command inside the Docker sandbox container.
func (d *DockerRunner) RunCommand(timeoutCtx, ctx context.Context, command, cwd string, timeout time.Duration) *tools.ToolCallResult {
	containerID, err := d.ensureContainer(ctx)
	if err != nil {
		return tools.ResultError(fmt.Sprintf("Failed to start sandbox container: %s", err))
	}

	args := []string{"exec", "-w", cwd}
	args = append(args, d.buildEnvVars()...)
	args = append(args, containerID, "/bin/sh", "-c", command)

	cmd := exec.CommandContext(timeoutCtx, "docker", args...)
	var outBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf

	err = cmd.Run()

	output := FormatCommandOutput(timeoutCtx, ctx, err, outBuf.String(), timeout)
	return tools.ResultSuccess(LimitOutput(output))
}

// Start is a no-op for Docker runner; containers are lazily started.
func (d *DockerRunner) Start(context.Context) error {
	return nil
}

// Stop stops and removes the sandbox container.
func (d *DockerRunner) Stop(context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.containerID == "" {
		return nil
	}

	stopCmd := exec.Command("docker", "stop", "-t", "1", d.containerID)
	_ = stopCmd.Run()

	d.containerID = ""
	return nil
}

// ensureContainer ensures the sandbox container is running, starting it if necessary.
func (d *DockerRunner) ensureContainer(ctx context.Context) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.containerID != "" && d.isContainerRunning(ctx) {
		return d.containerID, nil
	}
	d.containerID = ""

	return d.startContainer(ctx)
}

func (d *DockerRunner) isContainerRunning(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, "docker", "container", "inspect", "-f", "{{.State.Running}}", d.containerID)
	output, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(output)) == "true"
}

func (d *DockerRunner) startContainer(ctx context.Context) (string, error) {
	containerName := d.generateContainerName()
	image := cmp.Or(d.config.Image, "alpine:latest")

	args := []string{
		"run", "-d",
		"--name", containerName,
		"--rm", "--init", "--network", "host",
		"--label", sandboxLabelKey + "=true",
		"--label", fmt.Sprintf("%s=%d", sandboxLabelPID, os.Getpid()),
	}
	args = append(args, d.buildVolumeMounts()...)
	args = append(args, d.buildEnvVars()...)
	args = append(args, "-w", d.workingDir, image, "tail", "-f", "/dev/null")

	cmd := exec.CommandContext(ctx, "docker", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to start sandbox container: %w\nstderr: %s", err, stderr.String())
	}

	d.containerID = strings.TrimSpace(string(output))
	return d.containerID, nil
}

func (d *DockerRunner) generateContainerName() string {
	randomBytes := make([]byte, 4)
	_, _ = rand.Read(randomBytes)
	return fmt.Sprintf("cagent-sandbox-%s", hex.EncodeToString(randomBytes))
}

func (d *DockerRunner) buildVolumeMounts() []string {
	var args []string
	for _, pathSpec := range d.config.Paths {
		hostPath, mode := ParseSandboxPath(pathSpec)

		// Resolve to absolute path
		if !filepath.IsAbs(hostPath) {
			if d.workingDir != "" {
				hostPath = filepath.Join(d.workingDir, hostPath)
			} else {
				// If workingDir is empty, resolve relative to current directory
				var err error
				hostPath, err = filepath.Abs(hostPath)
				if err != nil {
					// Skip invalid paths
					continue
				}
			}
		}
		hostPath = filepath.Clean(hostPath)

		// Container path mirrors host path for simplicity
		mountSpec := fmt.Sprintf("%s:%s:%s", hostPath, hostPath, mode)
		args = append(args, "-v", mountSpec)
	}
	return args
}

// buildEnvVars creates Docker -e flags for environment variables.
// Only variables with valid POSIX names are forwarded.
func (d *DockerRunner) buildEnvVars() []string {
	var args []string
	for _, envVar := range d.env {
		if idx := strings.Index(envVar, "="); idx > 0 {
			key := envVar[:idx]
			if IsValidEnvVarName(key) {
				args = append(args, "-e", envVar)
			}
		}
	}
	return args
}

// IsValidEnvVarName checks if an environment variable name is valid for POSIX.
// Valid names start with a letter or underscore and contain only alphanumerics and underscores.
func IsValidEnvVarName(name string) bool {
	if name == "" {
		return false
	}
	for i, c := range name {
		isValid := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_' || (i > 0 && c >= '0' && c <= '9')
		if !isValid {
			return false
		}
	}
	return true
}

// ParseSandboxPath parses a path specification like "./path" or "/path:ro" into path and mode.
func ParseSandboxPath(pathSpec string) (path, mode string) {
	mode = "rw" // Default to read-write

	switch {
	case strings.HasSuffix(pathSpec, ":ro"):
		path = strings.TrimSuffix(pathSpec, ":ro")
		mode = "ro"
	case strings.HasSuffix(pathSpec, ":rw"):
		path = strings.TrimSuffix(pathSpec, ":rw")
		mode = "rw"
	default:
		path = pathSpec
	}

	return path, mode
}

// FormatCommandOutput formats command output handling timeout, cancellation, and errors.
func FormatCommandOutput(timeoutCtx, ctx context.Context, err error, rawOutput string, timeout time.Duration) string {
	var output string
	if timeoutCtx.Err() != nil {
		if ctx.Err() != nil {
			output = "Command cancelled"
		} else {
			output = fmt.Sprintf("Command timed out after %v\nOutput: %s", timeout, rawOutput)
		}
	} else {
		output = rawOutput
		if err != nil {
			output = fmt.Sprintf("Error executing command: %s\nOutput: %s", err, output)
		}
	}
	return cmp.Or(strings.TrimSpace(output), "<no output>")
}

// LimitOutput truncates output to a maximum size.
func LimitOutput(output string) string {
	const maxOutputSize = 30000
	if len(output) > maxOutputSize {
		return output[:maxOutputSize] + "\n\n[Output truncated: exceeded 30,000 character limit]"
	}
	return output
}
