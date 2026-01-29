package root

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/evaluation"
	"github.com/docker/cagent/pkg/telemetry"
)

const defaultJudgeModel = "anthropic/claude-opus-4-5-20251101"

type evalFlags struct {
	evaluation.Config
	runConfig config.RuntimeConfig
	outputDir string
}

func newEvalCmd() *cobra.Command {
	var flags evalFlags

	cmd := &cobra.Command{
		Use:     "eval <agent-file>|<registry-ref> [<eval-dir>|./evals]",
		Short:   "Run evaluations for an agent",
		GroupID: "advanced",
		Args:    cobra.RangeArgs(1, 2),
		RunE:    flags.runEvalCommand,
	}

	addRuntimeConfigFlags(cmd, &flags.runConfig)
	cmd.Flags().IntVarP(&flags.Concurrency, "concurrency", "c", runtime.NumCPU(), "Number of concurrent evaluation runs")
	cmd.Flags().StringVar(&flags.JudgeModel, "judge-model", defaultJudgeModel, "Model to use for relevance checking (format: provider/model)")
	cmd.Flags().StringVar(&flags.outputDir, "output", "", "Directory for results and logs (default: <eval-dir>/results)")
	cmd.Flags().StringSliceVar(&flags.Only, "only", nil, "Only run evaluations with file names matching these patterns (can be specified multiple times)")
	cmd.Flags().StringVar(&flags.BaseImage, "base-image", "", "Custom base Docker image for running evaluations")
	cmd.Flags().BoolVar(&flags.KeepContainers, "keep-containers", false, "Keep containers after evaluation (don't use --rm)")

	return cmd
}

func (f *evalFlags) runEvalCommand(cmd *cobra.Command, args []string) error {
	telemetry.TrackCommand("eval", args)

	ctx := cmd.Context()
	agentFilename := args[0]
	evalsDir := "./evals"
	if len(args) >= 2 {
		evalsDir = args[1]
	}

	// Output directory defaults to <evals-dir>/results
	outputDir := f.outputDir
	if outputDir == "" {
		outputDir = filepath.Join(evalsDir, "results")
	}

	// Create output directory
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	// Generate run name upfront so we can set up logging
	runName := evaluation.GenerateRunName()

	// Set up log file with debug logging
	logPath := filepath.Join(outputDir, runName+".log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("creating log file: %w", err)
	}
	defer logFile.Close()

	// Set up slog to write debug logs to the log file
	logHandler := slog.NewTextHandler(logFile, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	originalLogger := slog.Default()
	slog.SetDefault(slog.New(logHandler))
	defer slog.SetDefault(originalLogger)

	// Write header to log file
	fmt.Fprintf(logFile, "=== Evaluation Run: %s ===\n", runName)
	fmt.Fprintf(logFile, "Started: %s\n", time.Now().Format(time.RFC3339))
	fmt.Fprintf(logFile, "Agent: %s\n", agentFilename)
	fmt.Fprintf(logFile, "Evals dir: %s\n", evalsDir)
	fmt.Fprintf(logFile, "Judge model: %s\n", f.JudgeModel)
	fmt.Fprintf(logFile, "Concurrency: %d\n", f.Concurrency)
	fmt.Fprintf(logFile, "\n")

	// Create tee writer to write to both console and log file
	consoleOut := cmd.OutOrStdout()
	teeOut := io.MultiWriter(consoleOut, logFile)

	// Check if console is a TTY (for colored output)
	isTTY := false
	if file, ok := consoleOut.(*os.File); ok {
		f.TTYFd = int(file.Fd())
		isTTY = term.IsTerminal(f.TTYFd)
	}

	// Set remaining config fields
	f.AgentFilename = agentFilename
	f.EvalsDir = evalsDir

	// Run evaluation
	// Pass consoleOut for TTY progress bar, teeOut for results that should go to both console and log
	run, evalErr := evaluation.Evaluate(ctx, consoleOut, teeOut, isTTY, runName, &f.runConfig, f.Config)
	if run == nil {
		return evalErr
	}

	// Save results JSON
	resultsPath, err := evaluation.SaveRunJSON(run, outputDir)
	if err != nil {
		slog.Error("Failed to save results", "error", err)
	} else {
		fmt.Fprintf(teeOut, "\nResults: %s\n", resultsPath)
		fmt.Fprintf(teeOut, "Log: %s\n", logPath)
	}

	return evalErr
}
