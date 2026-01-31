package builtin

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/docker/cagent/pkg/concurrent"
	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/tools"
)

const (
	ToolNameShell              = "shell"
	ToolNameRunShellBackground = "run_background_job"
	ToolNameListBackgroundJobs = "list_background_jobs"
	ToolNameViewBackgroundJob  = "view_background_job"
	ToolNameStopBackgroundJob  = "stop_background_job"
)

// ShellTool provides shell command execution capabilities.
type ShellTool struct {
	handler *shellHandler
}

// Verify interface compliance
var (
	_ tools.ToolSet      = (*ShellTool)(nil)
	_ tools.Startable    = (*ShellTool)(nil)
	_ tools.Instructable = (*ShellTool)(nil)
)

type shellHandler struct {
	shell           string
	shellArgsPrefix []string
	env             []string
	timeout         time.Duration
	workingDir      string
	jobs            *concurrent.Map[string, *backgroundJob]
	jobCounter      atomic.Int64
	sandbox         *sandboxRunner
}

// Job status constants
const (
	statusRunning int32 = iota
	statusCompleted
	statusStopped
	statusFailed
)

// backgroundJob tracks a background shell command
type backgroundJob struct {
	id           string
	cmd          string
	cwd          string
	process      *os.Process
	processGroup *processGroup
	outputMu     sync.RWMutex
	output       *bytes.Buffer
	startTime    time.Time
	status       atomic.Int32
	exitCode     int
	err          error
}

// limitedWriter wraps a buffer and stops writing after maxSize bytes
type limitedWriter struct {
	mu      sync.Mutex
	buf     *bytes.Buffer
	written int64
	maxSize int64
}

func (lw *limitedWriter) Write(p []byte) (n int, err error) {
	lw.mu.Lock()
	defer lw.mu.Unlock()

	if lw.written >= lw.maxSize {
		return len(p), nil // Discard but report success
	}

	remaining := lw.maxSize - lw.written
	toWrite := min(int64(len(p)), remaining)

	n, err = lw.buf.Write(p[:toWrite])
	lw.written += int64(n)

	if err == nil && int64(n) < int64(len(p)) {
		return len(p), nil // Report full write even if truncated
	}
	return n, err
}

type RunShellArgs struct {
	Cmd     string `json:"cmd" jsonschema:"The shell command to execute"`
	Cwd     string `json:"cwd,omitempty" jsonschema:"The working directory to execute the command in (default: \".\")"`
	Timeout int    `json:"timeout,omitempty" jsonschema:"Command execution timeout in seconds (default: 30)"`
}

type RunShellBackgroundArgs struct {
	Cmd string `json:"cmd" jsonschema:"The shell command to execute in the background"`
	Cwd string `json:"cwd,omitempty" jsonschema:"The working directory to execute the command in (default: \".\")"`
}

type ViewBackgroundJobArgs struct {
	JobID string `json:"job_id" jsonschema:"The ID of the background job to view"`
}

type StopBackgroundJobArgs struct {
	JobID string `json:"job_id" jsonschema:"The ID of the background job to stop"`
}

// statusStrings maps job status constants to their string representations
var statusStrings = map[int32]string{
	statusRunning:   "running",
	statusCompleted: "completed",
	statusStopped:   "stopped",
	statusFailed:    "failed",
}

func statusToString(status int32) string {
	if s, ok := statusStrings[status]; ok {
		return s
	}
	return "unknown"
}

func (h *shellHandler) RunShell(ctx context.Context, params RunShellArgs) (*tools.ToolCallResult, error) {
	timeout := h.timeout
	if params.Timeout > 0 {
		timeout = time.Duration(params.Timeout) * time.Second
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cwd := h.resolveWorkDir(params.Cwd)

	// Delegate to sandbox runner if configured
	if h.sandbox != nil {
		return h.sandbox.runCommand(timeoutCtx, ctx, params.Cmd, cwd, timeout), nil
	}

	return h.runNativeCommand(timeoutCtx, ctx, params.Cmd, cwd, timeout), nil
}

func (h *shellHandler) runNativeCommand(timeoutCtx, ctx context.Context, command, cwd string, timeout time.Duration) *tools.ToolCallResult {
	cmd := exec.Command(h.shell, append(h.shellArgsPrefix, command)...)
	cmd.Env = h.env
	cmd.Dir = cwd
	cmd.SysProcAttr = platformSpecificSysProcAttr()

	var outBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf

	if err := cmd.Start(); err != nil {
		return tools.ResultError(fmt.Sprintf("Error starting command: %s", err))
	}

	pg, err := createProcessGroup(cmd.Process)
	if err != nil {
		return tools.ResultError(fmt.Sprintf("Error creating process group: %s", err))
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	var cmdErr error
	select {
	case <-timeoutCtx.Done():
		_ = kill(cmd.Process, pg)
	case cmdErr = <-done:
	}

	output := formatCommandOutput(timeoutCtx, ctx, cmdErr, outBuf.String(), timeout)
	return tools.ResultSuccess(limitOutput(output))
}

func (h *shellHandler) RunShellBackground(_ context.Context, params RunShellBackgroundArgs) (*tools.ToolCallResult, error) {
	counter := h.jobCounter.Add(1)
	jobID := fmt.Sprintf("job_%d_%d", time.Now().Unix(), counter)

	cmd := exec.Command(h.shell, append(h.shellArgsPrefix, params.Cmd)...)
	cmd.Env = h.env
	cmd.Dir = h.resolveWorkDir(params.Cwd)
	cmd.SysProcAttr = platformSpecificSysProcAttr()

	outputBuf := &bytes.Buffer{}
	limitedWriter := &limitedWriter{buf: outputBuf, maxSize: 10 * 1024 * 1024}
	cmd.Stdout = limitedWriter
	cmd.Stderr = limitedWriter

	if err := cmd.Start(); err != nil {
		return tools.ResultError(fmt.Sprintf("Error starting background command: %s", err)), nil
	}

	pg, err := createProcessGroup(cmd.Process)
	if err != nil {
		_ = kill(cmd.Process, pg)
		return tools.ResultError(fmt.Sprintf("Error creating process group: %s", err)), nil
	}

	job := &backgroundJob{
		id:           jobID,
		cmd:          params.Cmd,
		cwd:          params.Cwd,
		process:      cmd.Process,
		processGroup: pg,
		output:       outputBuf,
		startTime:    time.Now(),
	}
	job.status.Store(statusRunning)
	h.jobs.Store(jobID, job)

	go h.monitorJob(job, cmd)

	return tools.ResultSuccess(fmt.Sprintf("Background job started with ID: %s\nCommand: %s\nWorking directory: %s",
		jobID, params.Cmd, params.Cwd)), nil
}

func (h *shellHandler) monitorJob(job *backgroundJob, cmd *exec.Cmd) {
	err := cmd.Wait()

	job.outputMu.Lock()
	defer job.outputMu.Unlock()

	if job.status.Load() == statusStopped {
		return
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			job.exitCode = exitErr.ExitCode()
		} else {
			job.exitCode = -1
		}
		job.status.Store(statusFailed)
		job.err = err
	} else {
		job.exitCode = 0
		job.status.Store(statusCompleted)
	}
}

func (h *shellHandler) ListBackgroundJobs(_ context.Context, _ map[string]any) (*tools.ToolCallResult, error) {
	var output strings.Builder
	output.WriteString("Background Jobs:\n\n")

	jobCount := 0
	h.jobs.Range(func(jobID string, job *backgroundJob) bool {
		jobCount++
		status := job.status.Load()
		elapsed := time.Since(job.startTime).Round(time.Second)

		fmt.Fprintf(&output, "ID: %s\n", jobID)
		fmt.Fprintf(&output, "  Command: %s\n", job.cmd)
		fmt.Fprintf(&output, "  Status: %s\n", statusToString(status))
		fmt.Fprintf(&output, "  Runtime: %s\n", elapsed)
		if status != statusRunning {
			job.outputMu.RLock()
			fmt.Fprintf(&output, "  Exit Code: %d\n", job.exitCode)
			job.outputMu.RUnlock()
		}
		output.WriteString("\n")
		return true
	})

	if jobCount == 0 {
		output.WriteString("No background jobs found.\n")
	}

	return tools.ResultSuccess(output.String()), nil
}

func (h *shellHandler) ViewBackgroundJob(_ context.Context, params ViewBackgroundJobArgs) (*tools.ToolCallResult, error) {
	job, exists := h.jobs.Load(params.JobID)
	if !exists {
		return tools.ResultError(fmt.Sprintf("Job not found: %s", params.JobID)), nil
	}

	status := job.status.Load()

	job.outputMu.RLock()
	output := job.output.String()
	exitCode := job.exitCode
	job.outputMu.RUnlock()

	var result strings.Builder
	fmt.Fprintf(&result, "Job ID: %s\n", job.id)
	fmt.Fprintf(&result, "Command: %s\n", job.cmd)
	fmt.Fprintf(&result, "Status: %s\n", statusToString(status))
	fmt.Fprintf(&result, "Runtime: %s\n", time.Since(job.startTime).Round(time.Second))
	if status != statusRunning {
		fmt.Fprintf(&result, "Exit Code: %d\n", exitCode)
	}
	result.WriteString("\n--- Output ---\n")
	if output == "" {
		result.WriteString("<no output>\n")
	} else {
		result.WriteString(output)
		if len(output) >= 10*1024*1024 {
			result.WriteString("\n\n[Output truncated at 10MB limit]")
		}
	}

	return tools.ResultSuccess(result.String()), nil
}

func (h *shellHandler) StopBackgroundJob(_ context.Context, params StopBackgroundJobArgs) (*tools.ToolCallResult, error) {
	job, exists := h.jobs.Load(params.JobID)
	if !exists {
		return tools.ResultError(fmt.Sprintf("Job not found: %s", params.JobID)), nil
	}

	if !job.status.CompareAndSwap(statusRunning, statusStopped) {
		currentStatus := job.status.Load()
		return tools.ResultError(fmt.Sprintf("Job %s is not running (current status: %s)", params.JobID, statusToString(currentStatus))), nil
	}

	if err := kill(job.process, job.processGroup); err != nil {
		return tools.ResultError(fmt.Sprintf("Job %s marked as stopped, but error killing process: %s", params.JobID, err)), nil
	}

	return tools.ResultSuccess(fmt.Sprintf("Job %s stopped successfully", params.JobID)), nil
}

// NewShellTool creates a new shell tool with optional sandbox configuration.
func NewShellTool(env []string, runConfig *config.RuntimeConfig, sandboxConfig *latest.SandboxConfig) *ShellTool {
	shell, argsPrefix := detectShell(sandboxConfig != nil)

	handler := &shellHandler{
		shell:           shell,
		shellArgsPrefix: argsPrefix,
		env:             env,
		timeout:         30 * time.Second,
		jobs:            concurrent.NewMap[string, *backgroundJob](),
		workingDir:      runConfig.WorkingDir,
	}

	if sandboxConfig != nil {
		handler.sandbox = newSandboxRunner(sandboxConfig, runConfig.WorkingDir, env)
	}

	return &ShellTool{handler: handler}
}

// detectShell returns the appropriate shell and arguments based on the platform.
func detectShell(sandboxMode bool) (shell string, argsPrefix []string) {
	if sandboxMode {
		return "/bin/sh", []string{"-c"}
	}

	if runtime.GOOS == "windows" {
		return detectWindowsShell()
	}

	return cmp.Or(os.Getenv("SHELL"), "/bin/sh"), []string{"-c"}
}

func detectWindowsShell() (shell string, argsPrefix []string) {
	powershellArgs := []string{"-NoProfile", "-NonInteractive", "-Command"}
	for _, ps := range []string{"pwsh.exe", "powershell.exe"} {
		if path, err := exec.LookPath(ps); err == nil {
			return path, powershellArgs
		}
	}
	return cmp.Or(os.Getenv("ComSpec"), "cmd.exe"), []string{"/C"}
}

// resolveWorkDir returns the effective working directory.
func (h *shellHandler) resolveWorkDir(cwd string) string {
	if cwd == "" || cwd == "." {
		return h.workingDir
	}
	return cwd
}

// formatCommandOutput formats command output handling timeout, cancellation, and errors.
func formatCommandOutput(timeoutCtx, ctx context.Context, err error, rawOutput string, timeout time.Duration) string {
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

func (t *ShellTool) Instructions() string {
	if t.handler.sandbox != nil {
		return t.buildSandboxInstructions()
	}
	return nativeInstructions
}

// buildSandboxInstructions returns the native instructions with a note about Linux sandboxing.
func (t *ShellTool) buildSandboxInstructions() string {
	return "**Note:** For sandboxing reasons, all shell commands run inside a Linux container.\n\n" + nativeInstructions
}

func (t *ShellTool) Tools(context.Context) ([]tools.Tool, error) {
	shellDesc := `Executes the given shell command in the user's default shell.`
	if t.handler.sandbox != nil {
		shellDesc = `Executes the given shell command inside a sandboxed Linux container (Alpine Linux with /bin/sh). Only mounted paths are accessible. Installed tools persist across calls.`
	}

	return []tools.Tool{
		{
			Name:                    ToolNameShell,
			Category:                "shell",
			Description:             shellDesc,
			Parameters:              tools.MustSchemaFor[RunShellArgs](),
			OutputSchema:            tools.MustSchemaFor[string](),
			Handler:                 tools.NewHandler(t.handler.RunShell),
			Annotations:             tools.ToolAnnotations{Title: "Shell"},
			AddDescriptionParameter: true,
		},
		{
			Name:                    ToolNameRunShellBackground,
			Category:                "shell",
			Description:             `Starts a shell command in the background and returns immediately with a job ID. Use this for long-running processes like servers, watches, or any command that should run while other tasks are performed.`,
			Parameters:              tools.MustSchemaFor[RunShellBackgroundArgs](),
			OutputSchema:            tools.MustSchemaFor[string](),
			Handler:                 tools.NewHandler(t.handler.RunShellBackground),
			Annotations:             tools.ToolAnnotations{Title: "Background Job"},
			AddDescriptionParameter: true,
		},
		{
			Name:                    ToolNameListBackgroundJobs,
			Category:                "shell",
			Description:             `Lists all background jobs with their status, runtime, and other information.`,
			OutputSchema:            tools.MustSchemaFor[string](),
			Handler:                 tools.NewHandler(t.handler.ListBackgroundJobs),
			Annotations:             tools.ToolAnnotations{Title: "List Background Jobs", ReadOnlyHint: true},
			AddDescriptionParameter: true,
		},
		{
			Name:                    ToolNameViewBackgroundJob,
			Category:                "shell",
			Description:             `Views the output and status of a specific background job by job ID.`,
			Parameters:              tools.MustSchemaFor[ViewBackgroundJobArgs](),
			OutputSchema:            tools.MustSchemaFor[string](),
			Handler:                 tools.NewHandler(t.handler.ViewBackgroundJob),
			Annotations:             tools.ToolAnnotations{Title: "View Background Job Output", ReadOnlyHint: true},
			AddDescriptionParameter: true,
		},
		{
			Name:                    ToolNameStopBackgroundJob,
			Category:                "shell",
			Description:             `Stops a running background job by job ID. The process and all its child processes will be terminated.`,
			Parameters:              tools.MustSchemaFor[StopBackgroundJobArgs](),
			OutputSchema:            tools.MustSchemaFor[string](),
			Handler:                 tools.NewHandler(t.handler.StopBackgroundJob),
			Annotations:             tools.ToolAnnotations{Title: "Stop Background Job"},
			AddDescriptionParameter: true,
		},
	}, nil
}

func (t *ShellTool) Start(context.Context) error {
	return nil
}

func (t *ShellTool) Stop(context.Context) error {
	// Terminate all running background jobs
	t.handler.jobs.Range(func(_ string, job *backgroundJob) bool {
		if job.status.CompareAndSwap(statusRunning, statusStopped) {
			_ = kill(job.process, job.processGroup)
		}
		return true
	})

	// Stop sandbox container if running
	if t.handler.sandbox != nil {
		t.handler.sandbox.stop()
	}

	return nil
}
