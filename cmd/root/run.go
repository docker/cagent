package root

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	goruntime "runtime"
	"runtime/pprof"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel"

	"github.com/docker/cagent/pkg/app"
	"github.com/docker/cagent/pkg/cli"
	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/paths"
	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/teamloader"
	"github.com/docker/cagent/pkg/telemetry"
	"github.com/docker/cagent/pkg/tui/styles"
	"github.com/docker/cagent/pkg/userconfig"
)

type runExecFlags struct {
	agentName         string
	autoApprove       bool
	attachmentPath    string
	remoteAddress     string
	connectRPC        bool
	modelOverrides    []string
	promptFiles       []string
	dryRun            bool
	runConfig         config.RuntimeConfig
	sessionDB         string
	sessionID         string
	recordPath        string
	fakeResponses     string
	fakeStreamDelay   int
	exitAfterResponse bool
	cpuProfile        string
	memProfile        string
	forceTUI          bool

	// Exec only
	hideToolCalls bool
	outputJSON    bool

	// Run only
	hideToolResults bool
}

func newRunCmd() *cobra.Command {
	var flags runExecFlags

	cmd := &cobra.Command{
		Use:   "run [<agent-file>|<registry-ref>] [message]...",
		Short: "Run an agent",
		Long:  "Run an agent with the specified configuration and prompt",
		Example: `  cagent run ./agent.yaml
  cagent run ./team.yaml --agent root
  cagent run # built-in default agent
  cagent run coder # built-in coding agent
  cagent run ./echo.yaml "INSTRUCTIONS"
  cagent run ./echo.yaml "First question" "Follow-up question"
  echo "INSTRUCTIONS" | cagent run ./echo.yaml -
  cagent run ./agent.yaml --record  # Records session to auto-generated file`,
		GroupID:           "core",
		ValidArgsFunction: completeRunExec,
		Args:              cobra.ArbitraryArgs,
		RunE:              flags.runRunCommand,
	}

	addRunOrExecFlags(cmd, &flags)
	addRuntimeConfigFlags(cmd, &flags.runConfig)

	return cmd
}

func addRunOrExecFlags(cmd *cobra.Command, flags *runExecFlags) {
	cmd.PersistentFlags().StringVarP(&flags.agentName, "agent", "a", "root", "Name of the agent to run")
	cmd.PersistentFlags().BoolVar(&flags.autoApprove, "yolo", false, "Automatically approve all tool calls without prompting")
	cmd.PersistentFlags().BoolVar(&flags.hideToolResults, "hide-tool-results", false, "Hide tool call results")
	cmd.PersistentFlags().StringVar(&flags.attachmentPath, "attach", "", "Attach an image file to the message")
	cmd.PersistentFlags().StringArrayVar(&flags.promptFiles, "prompt-file", nil, "Append file contents to the prompt (repeatable)")
	cmd.PersistentFlags().StringArrayVar(&flags.modelOverrides, "model", nil, "Override agent model: [agent=]provider/model (repeatable)")
	cmd.PersistentFlags().BoolVar(&flags.dryRun, "dry-run", false, "Initialize the agent without executing anything")
	cmd.PersistentFlags().StringVar(&flags.remoteAddress, "remote", "", "Use remote runtime with specified address")
	cmd.PersistentFlags().BoolVar(&flags.connectRPC, "connect-rpc", false, "Use Connect-RPC protocol for remote communication (requires --remote)")
	cmd.PersistentFlags().StringVarP(&flags.sessionDB, "session-db", "s", filepath.Join(paths.GetHomeDir(), ".cagent", "session.db"), "Path to the session database")
	cmd.PersistentFlags().StringVar(&flags.sessionID, "session", "", "Continue from a previous session by ID or relative offset (e.g., -1 for last session)")
	cmd.PersistentFlags().StringVar(&flags.fakeResponses, "fake", "", "Replay AI responses from cassette file (for testing)")
	cmd.PersistentFlags().IntVar(&flags.fakeStreamDelay, "fake-stream", 0, "Simulate streaming with delay in ms between chunks (default 15ms if no value given)")
	cmd.Flag("fake-stream").NoOptDefVal = "15" // --fake-stream without value uses 15ms
	cmd.PersistentFlags().StringVar(&flags.recordPath, "record", "", "Record AI API interactions to cassette file (auto-generates filename if empty)")
	cmd.PersistentFlags().Lookup("record").NoOptDefVal = "true"
	cmd.PersistentFlags().BoolVar(&flags.exitAfterResponse, "exit-after-response", false, "Exit TUI after first assistant response completes")
	_ = cmd.PersistentFlags().MarkHidden("exit-after-response")
	cmd.PersistentFlags().StringVar(&flags.cpuProfile, "cpuprofile", "", "Write CPU profile to file")
	_ = cmd.PersistentFlags().MarkHidden("cpuprofile")
	cmd.PersistentFlags().StringVar(&flags.memProfile, "memprofile", "", "Write memory profile to file")
	_ = cmd.PersistentFlags().MarkHidden("memprofile")
	cmd.PersistentFlags().BoolVar(&flags.forceTUI, "force-tui", false, "Force TUI mode even when not in a terminal")
	_ = cmd.PersistentFlags().MarkHidden("force-tui")
	cmd.MarkFlagsMutuallyExclusive("fake", "record")
}

func (f *runExecFlags) runRunCommand(cmd *cobra.Command, args []string) error {
	telemetry.TrackCommand("run", args)

	ctx := cmd.Context()
	out := cli.NewPrinter(cmd.OutOrStdout())

	tui := f.forceTUI || isatty.IsTerminal(os.Stdout.Fd())
	return f.runOrExec(ctx, out, args, tui)
}

func (f *runExecFlags) runOrExec(ctx context.Context, out *cli.Printer, args []string, tui bool) (retErr error) {
	slog.Debug("Starting agent", "agent", f.agentName)

	// Start CPU profiling if requested
	if f.cpuProfile != "" {
		pf, err := os.Create(f.cpuProfile)
		if err != nil {
			return fmt.Errorf("failed to create CPU profile: %w", err)
		}
		defer pf.Close()
		if err := pprof.StartCPUProfile(pf); err != nil {
			return fmt.Errorf("failed to start CPU profile: %w", err)
		}
		defer pprof.StopCPUProfile()
		slog.Info("CPU profiling enabled", "file", f.cpuProfile)
	}

	// Write memory profile at exit if requested
	if f.memProfile != "" {
		defer func() {
			mf, err := os.Create(f.memProfile)
			if err != nil {
				slog.Error("Failed to create memory profile", "error", err)
				return
			}
			defer mf.Close()
			goruntime.GC() // Get up-to-date statistics
			if err := pprof.WriteHeapProfile(mf); err != nil {
				slog.Error("Failed to write memory profile", "error", err)
			}
			slog.Info("Memory profile written", "file", f.memProfile)
		}()
	}

	var agentFileName string
	if len(args) > 0 {
		agentFileName = args[0]
	}

	// Apply global user settings first (lowest priority)
	// User settings only apply if the flag wasn't explicitly set by the user
	userSettings := userconfig.Get()
	if userSettings.HideToolResults && !f.hideToolResults {
		f.hideToolResults = true
		slog.Debug("Applying user settings", "hide_tool_results", true)
	}
	if userSettings.YOLO && !f.autoApprove {
		f.autoApprove = true
		slog.Debug("Applying user settings", "YOLO", true)
	}

	// Apply alias options if this is an alias reference
	// Alias options only apply if the flag wasn't explicitly set by the user
	if alias := config.ResolveAlias(agentFileName); alias != nil {
		slog.Debug("Applying alias options", "yolo", alias.Yolo, "model", alias.Model, "hide_tool_results", alias.HideToolResults)
		if alias.Yolo && !f.autoApprove {
			f.autoApprove = true
		}
		if alias.Model != "" && len(f.modelOverrides) == 0 {
			f.modelOverrides = append(f.modelOverrides, alias.Model)
		}
		if alias.HideToolResults && !f.hideToolResults {
			f.hideToolResults = true
		}
	}

	// Start fake proxy if --fake is specified
	fakeCleanup, err := setupFakeProxy(f.fakeResponses, f.fakeStreamDelay, &f.runConfig)
	if err != nil {
		return err
	}
	defer func() {
		if err := fakeCleanup(); err != nil {
			slog.Error("Failed to cleanup fake proxy", "error", err)
			if retErr == nil {
				retErr = fmt.Errorf("failed to cleanup fake proxy: %w", err)
			}
		}
	}()

	// Record AI API interactions to a cassette file if --record flag is specified.
	cassettePath, recordCleanup, err := setupRecordingProxy(f.recordPath, &f.runConfig)
	if err != nil {
		return err
	}
	if cassettePath != "" {
		defer func() {
			if err := recordCleanup(); err != nil {
				slog.Error("Failed to cleanup recording proxy", "error", err)
				if retErr == nil {
					retErr = fmt.Errorf("failed to cleanup recording proxy: %w", err)
				}
			}
		}()
		out.Println("Recording mode enabled, cassette: " + cassettePath)
	}

	var (
		rt      runtime.Runtime
		sess    *session.Session
		cleanup func()
	)
	if f.remoteAddress != "" {
		rt, sess, err = f.createRemoteRuntimeAndSession(ctx, agentFileName)
		if err != nil {
			return err
		}
		cleanup = func() {} // Remote runtime doesn't need local cleanup
	} else {
		agentSource, err := config.Resolve(agentFileName, f.runConfig.EnvProvider())
		if err != nil {
			return err
		}

		opts := []teamloader.Opt{
			teamloader.WithModelOverrides(f.modelOverrides),
		}
		if len(f.promptFiles) > 0 {
			opts = append(opts, teamloader.WithPromptFiles(f.promptFiles))
		}

		loadResult, err := teamloader.LoadWithConfig(ctx, agentSource, &f.runConfig, opts...)
		if err != nil {
			return err
		}

		rt, sess, err = f.createLocalRuntimeAndSession(ctx, loadResult)
		if err != nil {
			return err
		}

		// Setup cleanup for local runtime
		cleanup = func() {
			// Use a fresh context for cleanup since the original may be canceled
			cleanupCtx := context.WithoutCancel(ctx)
			if err := loadResult.Team.StopToolSets(cleanupCtx); err != nil {
				slog.Error("Failed to stop tool sets", "error", err)
			}
			if err := rt.Close(); err != nil {
				slog.Error("Failed to close runtime", "error", err)
			}
		}
	}
	defer cleanup()

	// Apply theme before TUI starts
	if tui {
		applyTheme()
	}

	if f.dryRun {
		out.Println("Dry run mode enabled. Agent initialized but will not execute.")
		return nil
	}

	if !tui {
		return f.handleExecMode(ctx, out, rt, sess, args)
	}

	return f.handleRunMode(ctx, rt, sess, args)
}

func (f *runExecFlags) createRemoteRuntimeAndSession(ctx context.Context, originalFilename string) (runtime.Runtime, *session.Session, error) {
	var (
		client runtime.RemoteClient
		err    error
	)
	if f.connectRPC {
		client, err = runtime.NewConnectRPCClient(f.remoteAddress)
	} else {
		client, err = runtime.NewClient(f.remoteAddress)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create remote client: %w", err)
	}

	sessTemplate := session.New(
		session.WithToolsApproved(f.autoApprove),
	)

	sess, err := client.CreateSession(ctx, sessTemplate)
	if err != nil {
		return nil, nil, err
	}

	remoteRt, err := runtime.NewRemoteRuntime(client,
		runtime.WithRemoteCurrentAgent(f.agentName),
		runtime.WithRemoteAgentFilename(originalFilename),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create remote runtime: %w", err)
	}

	slog.Debug("Using remote runtime", "address", f.remoteAddress, "agent", f.agentName, "connect_rpc", f.connectRPC)
	return remoteRt, sess, nil
}

func (f *runExecFlags) createLocalRuntimeAndSession(ctx context.Context, loadResult *teamloader.LoadResult) (runtime.Runtime, *session.Session, error) {
	t := loadResult.Team

	agent, err := t.Agent(f.agentName)
	if err != nil {
		return nil, nil, err
	}

	// Expand tilde in session database path
	sessionDB, err := expandTilde(f.sessionDB)
	if err != nil {
		return nil, nil, err
	}

	sessStore, err := session.NewSQLiteSessionStore(sessionDB)
	if err != nil {
		return nil, nil, fmt.Errorf("creating session store: %w", err)
	}

	// Create model switcher config for runtime model switching support
	modelSwitcherCfg := &runtime.ModelSwitcherConfig{
		Models:             loadResult.Models,
		Providers:          loadResult.Providers,
		ModelsGateway:      f.runConfig.ModelsGateway,
		EnvProvider:        f.runConfig.EnvProvider(),
		AgentDefaultModels: loadResult.AgentDefaultModels,
	}

	localRt, err := runtime.New(t,
		runtime.WithSessionStore(sessStore),
		runtime.WithCurrentAgent(f.agentName),
		runtime.WithTracer(otel.Tracer(AppName)),
		runtime.WithModelSwitcherConfig(modelSwitcherCfg),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("creating runtime: %w", err)
	}

	var sess *session.Session
	if f.sessionID != "" {
		// Resolve relative session references (e.g., "-1" for last session)
		resolvedID, err := session.ResolveSessionID(ctx, sessStore, f.sessionID)
		if err != nil {
			return nil, nil, fmt.Errorf("resolving session %q: %w", f.sessionID, err)
		}

		// Load existing session
		sess, err = sessStore.GetSession(ctx, resolvedID)
		if err != nil {
			return nil, nil, fmt.Errorf("loading session %q: %w", resolvedID, err)
		}
		sess.ToolsApproved = f.autoApprove
		sess.HideToolResults = f.hideToolResults

		// Apply any stored model overrides from the session
		if len(sess.AgentModelOverrides) > 0 {
			if modelSwitcher, ok := localRt.(runtime.ModelSwitcher); ok {
				for agentName, modelRef := range sess.AgentModelOverrides {
					if err := modelSwitcher.SetAgentModel(ctx, agentName, modelRef); err != nil {
						slog.Warn("Failed to apply stored model override", "agent", agentName, "model", modelRef, "error", err)
					}
				}
			}
		}

		slog.Debug("Loaded existing session", "session_id", resolvedID, "session_ref", f.sessionID, "agent", f.agentName)
	} else {
		sess = session.New(
			session.WithMaxIterations(agent.MaxIterations()),
			session.WithToolsApproved(f.autoApprove),
			session.WithHideToolResults(f.hideToolResults),
			session.WithThinking(agent.ThinkingConfigured()),
		)
		// Session is stored lazily on first UpdateSession call (when content is added)
		// This avoids creating empty sessions in the database
		slog.Debug("Using local runtime", "agent", f.agentName, "thinking", agent.ThinkingConfigured())
	}

	return localRt, sess, nil
}

func (f *runExecFlags) handleExecMode(ctx context.Context, out *cli.Printer, rt runtime.Runtime, sess *session.Session, args []string) error {
	// args[0] is the agent file; args[1:] are user messages for multi-turn conversation
	userMessages := args[1:]

	err := cli.Run(ctx, out, cli.Config{
		AppName:        AppName,
		AttachmentPath: f.attachmentPath,
		HideToolCalls:  f.hideToolCalls,
		OutputJSON:     f.outputJSON,
		AutoApprove:    f.autoApprove,
	}, rt, sess, userMessages)
	if cliErr, ok := errors.AsType[cli.RuntimeError](err); ok {
		return RuntimeError{Err: cliErr.Err}
	}
	return err
}

func readInitialMessage(args []string) (*string, error) {
	if len(args) < 2 {
		return nil, nil
	}

	if args[1] == "-" {
		buf, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("failed to read from stdin: %w", err)
		}
		text := string(buf)
		return &text, nil
	}

	return &args[1], nil
}

func (f *runExecFlags) handleRunMode(ctx context.Context, rt runtime.Runtime, sess *session.Session, args []string) error {
	firstMessage, err := readInitialMessage(args)
	if err != nil {
		return err
	}

	var opts []app.Opt
	if firstMessage != nil {
		opts = append(opts, app.WithFirstMessage(*firstMessage))
	}
	if len(args) > 2 {
		opts = append(opts, app.WithQueuedMessages(args[2:]))
	}
	if f.attachmentPath != "" {
		opts = append(opts, app.WithFirstMessageAttachment(f.attachmentPath))
	}
	if f.exitAfterResponse {
		opts = append(opts, app.WithExitAfterFirstResponse())
	}

	return runTUI(ctx, rt, sess, opts...)
}

// applyTheme applies the theme from user config, or the built-in default.
func applyTheme() {
	// Resolve theme from user config > built-in default
	themeRef := styles.DefaultThemeRef
	if userSettings := userconfig.Get(); userSettings.Theme != "" {
		themeRef = userSettings.Theme
	}

	theme, err := styles.LoadTheme(themeRef)
	if err != nil {
		slog.Warn("Failed to load theme, using default", "theme", themeRef, "error", err)
		theme = styles.DefaultTheme()
	}

	styles.ApplyTheme(theme)
	slog.Debug("Applied theme", "theme_ref", themeRef, "theme_name", theme.Name)
}
