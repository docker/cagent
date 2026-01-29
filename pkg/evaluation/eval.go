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
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/model/provider"
	"github.com/docker/cagent/pkg/model/provider/options"
)

// Runner runs evaluations against an agent.
type Runner struct {
	Config
	agentSource config.Source
	judge       *Judge
	runConfig   *config.RuntimeConfig

	// imageCache caches built Docker images by working directory.
	// Key is the working directory (empty string for no working dir).
	imageCache   map[string]string
	imageCacheMu sync.Mutex
}

// newRunner creates a new evaluation runner.
func newRunner(agentSource config.Source, runConfig *config.RuntimeConfig, judgeModel provider.Provider, cfg Config) *Runner {
	var judge *Judge
	if judgeModel != nil {
		judge = NewJudge(judgeModel, runConfig, cfg.Concurrency)
	}
	return &Runner{
		Config:      cfg,
		agentSource: agentSource,
		judge:       judge,
		runConfig:   runConfig,
		imageCache:  make(map[string]string),
	}
}

// Evaluate runs evaluations with a specified run name.
// ttyOut is used for progress bar rendering (should be the console/TTY).
// out is used for results and status messages (can be tee'd to a log file).
func Evaluate(ctx context.Context, ttyOut, out io.Writer, isTTY bool, runName string, runConfig *config.RuntimeConfig, cfg Config) (*EvalRun, error) {
	agentSource, err := config.Resolve(cfg.AgentFilename)
	if err != nil {
		return nil, fmt.Errorf("resolving agent: %w", err)
	}

	// Create judge model provider for relevance checking
	judgeModel, err := createJudgeModel(ctx, cfg.JudgeModel, runConfig)
	if err != nil {
		return nil, err
	}

	runner := newRunner(agentSource, runConfig, judgeModel, cfg)

	fmt.Fprintf(out, "Evaluation run: %s\n", runName)

	startTime := time.Now()
	results, err := runner.Run(ctx, ttyOut, out, isTTY)
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
// ttyOut is used for progress bar rendering (should be the console/TTY).
// out is used for results and status messages (can be tee'd to a log file).
func (r *Runner) Run(ctx context.Context, ttyOut, out io.Writer, isTTY bool) ([]Result, error) {
	fmt.Fprintln(out, "Loading evaluation sessions...")
	evals, err := r.loadEvalSessions(ctx)
	if err != nil {
		return nil, fmt.Errorf("loading evaluations: %w", err)
	}

	// Pre-build all unique Docker images in parallel before running evaluations.
	// This avoids serialized builds when multiple workers need the same image.
	if err := r.preBuildImages(ctx, out, evals); err != nil {
		return nil, fmt.Errorf("pre-building images: %w", err)
	}

	fmt.Fprintf(out, "Running %d evaluations with concurrency %d\n\n", len(evals), r.Concurrency)

	progress := newProgressBar(ttyOut, out, r.TTYFd, len(evals), isTTY)
	progress.start()
	defer progress.stop()

	results := make([]Result, len(evals))

	work := make(chan workItem, len(evals))
	for i := range evals {
		work <- workItem{index: i, eval: &evals[i]}
	}
	close(work)

	var wg sync.WaitGroup
	for range r.Concurrency {
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
	entries, err := os.ReadDir(r.EvalsDir)
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
		if len(r.Only) > 0 && !matchesAnyPattern(fileName, r.Only) {
			continue
		}

		if entry.IsDir() || !strings.HasSuffix(fileName, ".json") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(r.EvalsDir, fileName))
		if err != nil {
			return nil, err
		}

		var evalSess EvalSession
		if err := json.Unmarshal(data, &evalSess); err != nil {
			return nil, err
		}

		evalSess.SourcePath = filepath.Join(r.EvalsDir, fileName)

		if evalSess.Title == "" {
			evalSess.Title = strings.TrimSuffix(fileName, ".json")
		}

		evals = append(evals, evalSess)
	}

	// Sort by duration (longest first) to avoid long tail
	slices.SortFunc(evals, func(a, b EvalSession) int {
		return cmp.Compare(b.Duration(), a.Duration())
	})

	return evals, nil
}

// preBuildImages pre-builds all unique Docker images needed for the evaluations.
// This is done in parallel to avoid serialized builds during evaluation.
func (r *Runner) preBuildImages(ctx context.Context, out io.Writer, evals []EvalSession) error {
	// Collect unique working directories
	workingDirs := make(map[string]struct{})
	for _, eval := range evals {
		workingDirs[eval.Evals.WorkingDir] = struct{}{}
	}

	if len(workingDirs) == 0 {
		return nil
	}

	fmt.Fprintf(out, "Pre-building %d Docker image(s)...\n", len(workingDirs))

	// Build images in parallel with limited concurrency
	type buildResult struct {
		workingDir string
		err        error
	}

	work := make(chan string, len(workingDirs))
	for wd := range workingDirs {
		work <- wd
	}
	close(work)

	results := make(chan buildResult, len(workingDirs))

	// Use same concurrency as evaluation runs for image builds
	buildWorkers := min(r.Concurrency, len(workingDirs))
	var wg sync.WaitGroup
	for range buildWorkers {
		wg.Go(func() {
			for wd := range work {
				if ctx.Err() != nil {
					results <- buildResult{workingDir: wd, err: ctx.Err()}
					continue
				}
				_, err := r.getOrBuildImage(ctx, wd)
				results <- buildResult{workingDir: wd, err: err}
			}
		})
	}

	// Wait for all builds to complete
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect errors
	var errs []error
	for result := range results {
		if result.err != nil {
			errs = append(errs, fmt.Errorf("building image for %q: %w", result.workingDir, result.err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed to build %d image(s): %v", len(errs), errs[0])
	}

	return nil
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

	expectedToolCalls := extractToolCalls(evalSess.Messages)
	if len(expectedToolCalls) > 0 {
		result.ToolCallsExpected = 1.0
	}

	workingDir := evalSess.Evals.WorkingDir

	imageID, err := r.getOrBuildImage(ctx, workingDir)
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

	if r.judge != nil && len(evalSess.Evals.Relevance) > 0 {
		passed, failed, errs := r.judge.CheckRelevance(ctx, result.Response, evalSess.Evals.Relevance)
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
	}
	if !r.KeepContainers {
		args = append(args, "--rm")
	}
	args = append(args,
		"-i",
		"-v", agentDir+":/configs:ro",
	)

	var env []string

	for _, name := range []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "GOOGLE_API_KEY", "MISTRAL_API_KEY", "XAI_API_KEY", "NEBIUS_API_KEY"} {
		if val, ok := r.runConfig.EnvProvider().Get(ctx, name); ok && val != "" {
			args = append(args, "-e", name)
			env = append(env, name+"="+val)
		}
	}

	if r.runConfig.ModelsGateway != "" {
		args = append(args, "-e", "CAGENT_MODELS_GATEWAY")
		env = append(env, "CAGENT_MODELS_GATEWAY="+r.runConfig.ModelsGateway)

		if token, ok := r.runConfig.EnvProvider().Get(ctx, environment.DockerDesktopTokenEnv); ok && token != "" {
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

	var stderrData []byte
	go func() {
		stderrData, _ = io.ReadAll(stderr)
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
		slog.Debug("Container exited with error", "stderr", string(stderrData), "error", waitErr)
	}

	if len(events) == 0 {
		stderrStr := strings.TrimSpace(string(stderrData))
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
	var responseBuf strings.Builder
	for _, event := range events {
		eventType, _ := event["type"].(string)

		switch eventType {
		case "agent_choice":
			if content, ok := event["content"].(string); ok {
				responseBuf.WriteString(content)
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

	return responseBuf.String(), cost, outputTokens, toolCalls
}

// matchesAnyPattern returns true if the name contains any of the patterns (case-insensitive).
func matchesAnyPattern(name string, patterns []string) bool {
	nameLower := strings.ToLower(name)
	return slices.ContainsFunc(patterns, func(pattern string) bool {
		return strings.Contains(nameLower, strings.ToLower(pattern))
	})
}

// createJudgeModel creates a provider.Provider from a model string (format: provider/model).
// Returns nil if judgeModel is empty.
func createJudgeModel(ctx context.Context, judgeModel string, runConfig *config.RuntimeConfig) (provider.Provider, error) {
	if judgeModel == "" {
		return nil, nil
	}

	providerName, model, ok := strings.Cut(judgeModel, "/")
	if !ok {
		return nil, fmt.Errorf("invalid judge model format %q: expected 'provider/model'", judgeModel)
	}

	cfg := &latest.ModelConfig{
		Provider: providerName,
		Model:    model,
	}

	var opts []options.Opt
	if runConfig.ModelsGateway != "" {
		opts = append(opts, options.WithGateway(runConfig.ModelsGateway))
	}

	judge, err := provider.New(ctx, cfg, runConfig.EnvProvider(), opts...)
	if err != nil {
		return nil, fmt.Errorf("creating judge model: %w", err)
	}

	return judge, nil
}
