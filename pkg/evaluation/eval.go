// Package evaluation provides an evaluation framework for testing agents.
package evaluation

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/model/provider"
)

// Runner runs evaluations against an agent.
type Runner struct {
	agentSource   config.Source
	evalsDir      string
	judgeModel    provider.Provider
	concurrency   int
	modelsGateway string
	envProvider   environment.Provider
	ttyFd         int
	only          []string
	baseImage     string
}

// NewRunner creates a new evaluation runner.
func NewRunner(agentSource config.Source, runConfig *config.RuntimeConfig, evalsDir string, cfg Config) *Runner {
	return &Runner{
		agentSource:   agentSource,
		evalsDir:      evalsDir,
		judgeModel:    cfg.JudgeModel,
		concurrency:   cmp.Or(cfg.Concurrency, goruntime.NumCPU()),
		modelsGateway: runConfig.ModelsGateway,
		envProvider:   runConfig.EnvProvider(),
		ttyFd:         cfg.TTYFd,
		only:          cfg.Only,
		baseImage:     cfg.BaseImage,
	}
}

// Evaluate is the main entry point for running evaluations.
func Evaluate(ctx context.Context, out io.Writer, isTTY bool, ttyFd int, agentFilename, evalsDir string, runConfig *config.RuntimeConfig, concurrency int, judgeModel provider.Provider, only []string, baseImage string) (*EvalRun, error) {
	return EvaluateWithName(ctx, out, isTTY, ttyFd, GenerateRunName(), agentFilename, evalsDir, runConfig, concurrency, judgeModel, only, baseImage)
}

// EvaluateWithName runs evaluations with a specified run name.
func EvaluateWithName(ctx context.Context, out io.Writer, isTTY bool, ttyFd int, runName, agentFilename, evalsDir string, runConfig *config.RuntimeConfig, concurrency int, judgeModel provider.Provider, only []string, baseImage string) (*EvalRun, error) {
	agentSource, err := config.Resolve(agentFilename)
	if err != nil {
		return nil, fmt.Errorf("resolving agent: %w", err)
	}

	runner := NewRunner(agentSource, runConfig, evalsDir, Config{
		Concurrency: concurrency,
		JudgeModel:  judgeModel,
		TTYFd:       ttyFd,
		Only:        only,
		BaseImage:   baseImage,
	})

	fmt.Fprintf(out, "Evaluation run: %s\n", runName)

	startTime := time.Now()
	results, err := runner.Run(ctx, out, isTTY)
	duration := time.Since(startTime)

	summary := computeSummary(results)
	printSummary(out, summary, duration)

	run := &EvalRun{
		Name:      runName,
		Timestamp: startTime,
		Duration:  duration,
		Results:   results,
		Summary:   summary,
	}

	if err != nil {
		return run, fmt.Errorf("running evaluations: %w", err)
	}

	return run, nil
}

// workItem represents a single evaluation to be processed.
type workItem struct {
	index int
	eval  *EvalSession
}

// Run executes all evaluations concurrently and returns results.
func (r *Runner) Run(ctx context.Context, out io.Writer, isTTY bool) ([]Result, error) {
	fmt.Fprintln(out, "Loading evaluation sessions...")
	evals, err := r.loadEvalSessions(ctx)
	if err != nil {
		return nil, fmt.Errorf("loading evaluations: %w", err)
	}
	fmt.Fprintf(out, "Running %d evaluations with concurrency %d\n\n", len(evals), r.concurrency)

	progress := newProgressBar(out, r.ttyFd, len(evals), isTTY)
	progress.start()
	defer progress.stop()

	results := make([]Result, len(evals))

	work := make(chan workItem, len(evals))
	for i := range evals {
		work <- workItem{index: i, eval: &evals[i]}
	}
	close(work)

	var wg sync.WaitGroup
	for range r.concurrency {
		wg.Go(func() {
			for item := range work {
				if ctx.Err() != nil {
					return
				}

				progress.setRunning(item.eval.Title)
				result, runErr := r.runSingleEval(ctx, item.eval)
				if runErr != nil {
					result.Error = runErr.Error()
					slog.Error("Evaluation failed", "title", item.eval.Title, "error", runErr)
				}

				results[item.index] = result
				_, failures := result.checkResults()
				progress.complete(result.Title, len(failures) == 0)
				progress.printResult(result)
			}
		})
	}

	wg.Wait()

	if ctx.Err() != nil {
		return results, ctx.Err()
	}

	return results, nil
}

func (r *Runner) loadEvalSessions(ctx context.Context) ([]EvalSession, error) {
	entries, err := os.ReadDir(r.evalsDir)
	if err != nil {
		return nil, err
	}

	var evals []EvalSession
	for _, entry := range entries {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Filter by --only patterns against file name if specified
		fileName := entry.Name()
		if len(r.only) > 0 && !matchesAnyPattern(fileName, r.only) {
			continue
		}

		if entry.IsDir() || !strings.HasSuffix(fileName, ".json") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(r.evalsDir, fileName))
		if err != nil {
			return nil, err
		}

		var evalSess EvalSession
		if err := json.Unmarshal(data, &evalSess); err != nil {
			return nil, err
		}

		evalSess.SourcePath = filepath.Join(r.evalsDir, fileName)

		if evalSess.Title == "" {
			evalSess.Title = strings.TrimSuffix(fileName, ".json")
		}

		evals = append(evals, evalSess)
	}

	// Sort by duration (longest first) to avoid long tail
	slices.SortFunc(evals, func(a, b EvalSession) int {
		durA := a.Duration()
		durB := b.Duration()
		if durA > durB {
			return -1
		}
		if durA < durB {
			return 1
		}
		return 0
	})

	return evals, nil
}

func (r *Runner) runSingleEval(ctx context.Context, evalSess *EvalSession) (Result, error) {
	startTime := time.Now()
	slog.Debug("Starting evaluation", "title", evalSess.Title)

	result := Result{
		InputPath:         evalSess.SourcePath,
		Title:             evalSess.Title,
		Question:          getFirstUserMessage(&evalSess.Session),
		SizeExpected:      evalSess.Evals.Size,
		RelevanceExpected: float64(len(evalSess.Evals.Relevance)),
	}

	expectedToolCalls := extractToolCalls(&evalSess.Session)
	if len(expectedToolCalls) > 0 {
		result.ToolCallsExpected = 1.0
	}

	workingDir := evalSess.Evals.WorkingDir

	imageID, err := r.buildEvalImage(ctx, workingDir)
	if err != nil {
		return result, fmt.Errorf("building eval image: %w", err)
	}

	events, err := r.runCagentInContainer(ctx, imageID, result.Question)
	if err != nil {
		return result, fmt.Errorf("running cagent in container: %w", err)
	}

	response, cost, outputTokens, actualToolCalls := parseContainerEvents(events)

	result.Response = response
	result.Cost = cost
	result.OutputTokens = outputTokens
	result.RawOutput = events
	result.Size = getResponseSize(result.Response)

	if len(expectedToolCalls) > 0 || len(actualToolCalls) > 0 {
		result.ToolCallsScore = toolCallF1Score(expectedToolCalls, actualToolCalls)
	}

	result.HandoffsMatch = countHandoffs(expectedToolCalls) == countHandoffs(actualToolCalls)

	if r.judgeModel != nil && len(evalSess.Evals.Relevance) > 0 {
		passed, failed, errs := r.checkRelevance(ctx, result.Response, evalSess.Evals.Relevance)
		result.RelevancePassed = float64(passed)
		result.FailedRelevance = failed
		for _, e := range errs {
			slog.Warn("Relevance check error", "title", evalSess.Title, "error", e)
		}
	}

	slog.Debug("Evaluation complete", "title", evalSess.Title, "duration", time.Since(startTime))
	return result, nil
}

func (r *Runner) runCagentInContainer(ctx context.Context, imageID, question string) ([]map[string]any, error) {
	agentDir := r.agentSource.ParentDir()
	agentFile := filepath.Base(r.agentSource.Name())
	containerName := fmt.Sprintf("cagent-eval-%d", uuid.New().ID())

	args := []string{
		"run",
		"--name", containerName,
		"--privileged",
		"--init",
		"--rm",
		"-i",
		"-v", agentDir + ":/configs:ro",
	}

	var env []string

	for _, name := range []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "GOOGLE_API_KEY", "MISTRAL_API_KEY", "XAI_API_KEY", "NEBIUS_API_KEY"} {
		if val, ok := r.envProvider.Get(ctx, name); ok && val != "" {
			args = append(args, "-e", name)
			env = append(env, name+"="+val)
		}
	}

	if r.modelsGateway != "" {
		args = append(args, "-e", "CAGENT_MODELS_GATEWAY")
		env = append(env, "CAGENT_MODELS_GATEWAY="+r.modelsGateway)

		if token, ok := r.envProvider.Get(ctx, environment.DockerDesktopTokenEnv); ok && token != "" {
			args = append(args, "-e", environment.DockerDesktopTokenEnv)
			env = append(env, environment.DockerDesktopTokenEnv+"="+token)
		}
	}

	args = append(args, imageID, "/configs/"+agentFile, question)

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Env = append(env, os.Environ()...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting docker run: %w", err)
	}

	var stderrBuf strings.Builder
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := stderr.Read(buf)
			if n > 0 {
				stderrBuf.Write(buf[:n])
			}
			if err != nil {
				break
			}
		}
	}()

	var events []map[string]any
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			slog.Debug("Failed to parse JSON event", "line", line, "error", err)
			continue
		}
		events = append(events, event)
	}

	if err := scanner.Err(); err != nil {
		slog.Warn("Error reading container output", "error", err)
	}

	waitErr := cmd.Wait()
	if waitErr != nil {
		slog.Debug("Container exited with error", "stderr", stderrBuf.String(), "error", waitErr)
	}

	if len(events) == 0 {
		stderrStr := strings.TrimSpace(stderrBuf.String())
		if waitErr != nil {
			return nil, fmt.Errorf("container failed: %w (stderr: %s)", waitErr, stderrStr)
		}
		if stderrStr != "" {
			return nil, fmt.Errorf("no events received from container (stderr: %s)", stderrStr)
		}
		return nil, fmt.Errorf("no events received from container")
	}

	return events, nil
}

func parseContainerEvents(events []map[string]any) (response string, cost float64, outputTokens int64, toolCalls []string) {
	for _, event := range events {
		eventType, _ := event["type"].(string)

		switch eventType {
		case "agent_choice":
			if content, ok := event["content"].(string); ok {
				response += content
			}
		case "tool_call":
			if tc, ok := event["tool_call"].(map[string]any); ok {
				if fn, ok := tc["function"].(map[string]any); ok {
					if name, ok := fn["name"].(string); ok {
						toolCalls = append(toolCalls, name)
					}
				}
			}
		case "token_usage":
			if usage, ok := event["usage"].(map[string]any); ok {
				if c, ok := usage["cost"].(float64); ok {
					cost = c
				}
				if tokens, ok := usage["output_tokens"].(float64); ok {
					outputTokens += int64(tokens)
				}
			}
		}
	}

	return response, cost, outputTokens, toolCalls
}

// matchesAnyPattern returns true if the name contains any of the patterns (case-insensitive).
func matchesAnyPattern(name string, patterns []string) bool {
	nameLower := strings.ToLower(name)
	for _, pattern := range patterns {
		if strings.Contains(nameLower, strings.ToLower(pattern)) {
			return true
		}
	}
	return false
}
