package hooks

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"
)

// Executor handles the execution of hooks
type Executor struct {
	config     *Config
	workingDir string
	env        []string

	// Shell configuration
	shell           string
	shellArgsPrefix []string

	// Cached compiled regexes
	preToolUseMatchers  []compiledMatcher
	postToolUseMatchers []compiledMatcher
}

type compiledMatcher struct {
	config  MatcherConfig
	pattern *regexp.Regexp
}

// hookResult represents the result of executing a single hook
type hookResult struct {
	output   *Output
	stdout   string
	stderr   string
	exitCode int
	err      error
}

// NewExecutor creates a new hook executor
func NewExecutor(config *Config, workingDir string, env []string) *Executor {
	if config == nil {
		config = &Config{}
	}

	e := &Executor{
		config:     config,
		workingDir: workingDir,
		env:        env,
	}

	e.initShell()
	e.compileMatchers()

	return e
}

// initShell initializes the shell configuration based on the OS
func (e *Executor) initShell() {
	if runtime.GOOS == "windows" {
		// Prefer PowerShell when available
		if path, err := exec.LookPath("pwsh.exe"); err == nil {
			e.shell = path
			e.shellArgsPrefix = []string{"-NoProfile", "-NonInteractive", "-Command"}
		} else if path, err := exec.LookPath("powershell.exe"); err == nil {
			e.shell = path
			e.shellArgsPrefix = []string{"-NoProfile", "-NonInteractive", "-Command"}
		} else {
			e.shell = cmp.Or(os.Getenv("ComSpec"), "cmd.exe")
			e.shellArgsPrefix = []string{"/C"}
		}
	} else {
		e.shell = cmp.Or(os.Getenv("SHELL"), "/bin/sh")
		e.shellArgsPrefix = []string{"-c"}
	}
}

// compileMatchers pre-compiles all matcher regex patterns
func (e *Executor) compileMatchers() {
	e.preToolUseMatchers = e.compileMatcherList(e.config.PreToolUse)
	e.postToolUseMatchers = e.compileMatcherList(e.config.PostToolUse)
}

func (e *Executor) compileMatcherList(configs []MatcherConfig) []compiledMatcher {
	var result []compiledMatcher
	for _, mc := range configs {
		var pattern *regexp.Regexp
		if mc.Matcher != "" && mc.Matcher != "*" {
			// Compile as regex, case-sensitive
			p, err := regexp.Compile("^(?:" + mc.Matcher + ")$")
			if err != nil {
				slog.Warn("Invalid hook matcher pattern", "pattern", mc.Matcher, "error", err)
				continue
			}
			pattern = p
		}
		result = append(result, compiledMatcher{
			config:  mc,
			pattern: pattern,
		})
	}
	return result
}

// matchTool checks if a tool name matches the given pattern
func (cm *compiledMatcher) matchTool(toolName string) bool {
	// "*" or empty matcher matches all
	if cm.config.Matcher == "" || cm.config.Matcher == "*" {
		return true
	}
	if cm.pattern == nil {
		return false
	}
	return cm.pattern.MatchString(toolName)
}

// ExecutePreToolUse runs pre-tool-use hooks for a tool
func (e *Executor) ExecutePreToolUse(ctx context.Context, input *Input) (*Result, error) {
	if e.config == nil || len(e.preToolUseMatchers) == 0 {
		return &Result{Allowed: true}, nil
	}

	input.HookEventName = EventPreToolUse

	// Find all matching hooks
	var hooksToRun []Hook
	for _, cm := range e.preToolUseMatchers {
		if cm.matchTool(input.ToolName) {
			hooksToRun = append(hooksToRun, cm.config.Hooks...)
		}
	}

	if len(hooksToRun) == 0 {
		return &Result{Allowed: true}, nil
	}

	return e.executeHooks(ctx, hooksToRun, input, EventPreToolUse)
}

// ExecutePostToolUse runs post-tool-use hooks for a tool
func (e *Executor) ExecutePostToolUse(ctx context.Context, input *Input) (*Result, error) {
	if e.config == nil || len(e.postToolUseMatchers) == 0 {
		return &Result{Allowed: true}, nil
	}

	input.HookEventName = EventPostToolUse

	// Find all matching hooks
	var hooksToRun []Hook
	for _, cm := range e.postToolUseMatchers {
		if cm.matchTool(input.ToolName) {
			hooksToRun = append(hooksToRun, cm.config.Hooks...)
		}
	}

	if len(hooksToRun) == 0 {
		return &Result{Allowed: true}, nil
	}

	return e.executeHooks(ctx, hooksToRun, input, EventPostToolUse)
}

// ExecuteSessionStart runs session start hooks
func (e *Executor) ExecuteSessionStart(ctx context.Context, input *Input) (*Result, error) {
	if e.config == nil || len(e.config.SessionStart) == 0 {
		return &Result{Allowed: true}, nil
	}

	input.HookEventName = EventSessionStart

	return e.executeHooks(ctx, e.config.SessionStart, input, EventSessionStart)
}

// ExecuteSessionEnd runs session end hooks
func (e *Executor) ExecuteSessionEnd(ctx context.Context, input *Input) (*Result, error) {
	if e.config == nil || len(e.config.SessionEnd) == 0 {
		return &Result{Allowed: true}, nil
	}

	input.HookEventName = EventSessionEnd

	return e.executeHooks(ctx, e.config.SessionEnd, input, EventSessionEnd)
}

// executeHooks runs a list of hooks in parallel and aggregates results
func (e *Executor) executeHooks(ctx context.Context, hooks []Hook, input *Input, eventType EventType) (*Result, error) {
	// Deduplicate hooks by command
	seen := make(map[string]bool)
	var uniqueHooks []Hook
	for _, h := range hooks {
		key := fmt.Sprintf("%s:%s", h.Type, h.Command)
		if !seen[key] {
			seen[key] = true
			uniqueHooks = append(uniqueHooks, h)
		}
	}

	if len(uniqueHooks) == 0 {
		return &Result{Allowed: true}, nil
	}

	// Serialize input to JSON
	inputJSON, err := input.ToJSON()
	if err != nil {
		return nil, fmt.Errorf("failed to serialize hook input: %w", err)
	}

	// Execute hooks in parallel
	results := make([]hookResult, len(uniqueHooks))
	var wg sync.WaitGroup

	for i, hook := range uniqueHooks {
		wg.Go(func() {
			output, stdout, stderr, exitCode, err := e.executeHook(ctx, hook, inputJSON)
			results[i] = hookResult{
				output:   output,
				stdout:   stdout,
				stderr:   stderr,
				exitCode: exitCode,
				err:      err,
			}
		})
	}

	wg.Wait()

	// Aggregate results
	return e.aggregateResults(results, eventType)
}

// executeHook runs a single hook and returns its output
func (e *Executor) executeHook(ctx context.Context, hook Hook, inputJSON []byte) (*Output, string, string, int, error) {
	if hook.Type != HookTypeCommand {
		return nil, "", "", 0, fmt.Errorf("unsupported hook type: %s", hook.Type)
	}

	// Create timeout context
	timeoutCtx, cancel := context.WithTimeout(ctx, hook.GetTimeout())
	defer cancel()

	// Build command
	cmd := exec.CommandContext(timeoutCtx, e.shell, append(e.shellArgsPrefix, hook.Command)...)
	cmd.Dir = e.workingDir
	cmd.Env = e.env

	// Provide input via stdin
	cmd.Stdin = bytes.NewReader(inputJSON)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Run command
	err := cmd.Run()

	exitCode := 0
	if err != nil {
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, stdout.String(), stderr.String(), -1, err
		}
	}

	// Parse output if exit code is 0
	var output *Output
	if exitCode == 0 && stdout.Len() > 0 {
		stdoutStr := strings.TrimSpace(stdout.String())
		// Try to parse as JSON
		if strings.HasPrefix(stdoutStr, "{") {
			var parsed Output
			if err := json.Unmarshal([]byte(stdoutStr), &parsed); err == nil {
				output = &parsed
			}
		}
	}

	return output, stdout.String(), stderr.String(), exitCode, nil
}

// aggregateResults combines results from multiple hooks
func (e *Executor) aggregateResults(results []hookResult, eventType EventType) (*Result, error) {
	finalResult := &Result{
		Allowed: true,
	}

	var messages []string
	var additionalContexts []string
	var systemMessages []string

	for _, r := range results {
		if r.err != nil {
			slog.Warn("Hook execution error", "error", r.err)
			continue
		}

		// Exit code 2 is a blocking error
		if r.exitCode == 2 {
			finalResult.Allowed = false
			finalResult.ExitCode = 2
			if r.stderr != "" {
				finalResult.Stderr = r.stderr
				messages = append(messages, strings.TrimSpace(r.stderr))
			}
			continue
		}

		// Non-zero, non-2 exit codes are non-blocking errors
		if r.exitCode != 0 {
			slog.Debug("Hook returned non-zero exit code", "exit_code", r.exitCode, "stderr", r.stderr)
			continue
		}

		// Process successful output
		if r.output != nil {
			// Check continue flag
			if !r.output.ShouldContinue() {
				finalResult.Allowed = false
				if r.output.StopReason != "" {
					messages = append(messages, r.output.StopReason)
				}
			}

			// Check decision
			if r.output.IsBlocked() {
				finalResult.Allowed = false
				if r.output.Reason != "" {
					messages = append(messages, r.output.Reason)
				}
			}

			// Collect system messages
			if r.output.SystemMessage != "" {
				systemMessages = append(systemMessages, r.output.SystemMessage)
			}

			// Process hook-specific output
			if r.output.HookSpecificOutput != nil {
				hso := r.output.HookSpecificOutput

				// PreToolUse permission decision
				if eventType == EventPreToolUse {
					switch hso.PermissionDecision {
					case DecisionDeny:
						finalResult.Allowed = false
						if hso.PermissionDecisionReason != "" {
							messages = append(messages, hso.PermissionDecisionReason)
						}
					case DecisionAsk:
						// Ask leaves it up to the normal approval flow
						// Don't change Allowed
					}

					// Merge updated input
					if hso.UpdatedInput != nil {
						if finalResult.ModifiedInput == nil {
							finalResult.ModifiedInput = make(map[string]any)
						}
						maps.Copy(finalResult.ModifiedInput, hso.UpdatedInput)
					}
				}

				// Additional context
				if hso.AdditionalContext != "" {
					additionalContexts = append(additionalContexts, hso.AdditionalContext)
				}
			}
		} else if r.stdout != "" {
			// Plain text stdout is added as context for some events
			if eventType == EventSessionStart || eventType == EventPostToolUse {
				additionalContexts = append(additionalContexts, strings.TrimSpace(r.stdout))
			}
		}
	}

	// Combine messages
	if len(messages) > 0 {
		finalResult.Message = strings.Join(messages, "\n")
	}
	if len(additionalContexts) > 0 {
		finalResult.AdditionalContext = strings.Join(additionalContexts, "\n")
	}
	if len(systemMessages) > 0 {
		finalResult.SystemMessage = strings.Join(systemMessages, "\n")
	}

	return finalResult, nil
}

// HasPreToolUseHooks returns true if there are any pre-tool-use hooks configured
func (e *Executor) HasPreToolUseHooks() bool {
	return e.config != nil && len(e.preToolUseMatchers) > 0
}

// HasPostToolUseHooks returns true if there are any post-tool-use hooks configured
func (e *Executor) HasPostToolUseHooks() bool {
	return e.config != nil && len(e.postToolUseMatchers) > 0
}

// HasSessionStartHooks returns true if there are any session start hooks configured
func (e *Executor) HasSessionStartHooks() bool {
	return e.config != nil && len(e.config.SessionStart) > 0
}

// HasSessionEndHooks returns true if there are any session end hooks configured
func (e *Executor) HasSessionEndHooks() bool {
	return e.config != nil && len(e.config.SessionEnd) > 0
}
