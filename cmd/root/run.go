package root

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel"

	"github.com/docker/cagent/pkg/cli"
	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/paths"
	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/team"
	"github.com/docker/cagent/pkg/teamloader"
	"github.com/docker/cagent/pkg/telemetry"
)

type runExecFlags struct {
	agentName      string
	autoApprove    bool
	attachmentPath string
	remoteAddress  string
	modelOverrides []string
	dryRun         bool
	runConfig      config.RuntimeConfig
	sessionDB      string

	// Shared between run and exec
	recordPath string

	// Exec only
	hideToolCalls bool
	outputJSON    bool
}

func newRunCmd() *cobra.Command {
	var flags runExecFlags

	cmd := &cobra.Command{
		Use:   "run [<agent-file>|<registry-ref>] [message|-]",
		Short: "Run an agent",
		Long:  "Run an agent with the specified configuration and prompt",
		Example: `  cagent run ./agent.yaml
  cagent run ./team.yaml --agent root
  cagent run # built-in default agent
  cagent run ./echo.yaml "INSTRUCTIONS"
  echo "INSTRUCTIONS" | cagent run ./echo.yaml -
  cagent run ./agent.yaml --record  # Records session to auto-generated file`,
		GroupID: "core",
		Args:    cobra.RangeArgs(0, 2),
		RunE:    flags.runRunCommand,
	}

	addRunOrExecFlags(cmd, &flags)
	addRuntimeConfigFlags(cmd, &flags.runConfig)

	return cmd
}

func addRunOrExecFlags(cmd *cobra.Command, flags *runExecFlags) {
	cmd.PersistentFlags().StringVarP(&flags.agentName, "agent", "a", "root", "Name of the agent to run")
	cmd.PersistentFlags().BoolVar(&flags.autoApprove, "yolo", false, "Automatically approve all tool calls without prompting")
	cmd.PersistentFlags().StringVar(&flags.attachmentPath, "attach", "", "Attach an image file to the message")
	cmd.PersistentFlags().StringArrayVar(&flags.modelOverrides, "model", nil, "Override agent model: [agent=]provider/model (repeatable)")
	cmd.PersistentFlags().BoolVar(&flags.dryRun, "dry-run", false, "Initialize the agent without executing anything")
	cmd.PersistentFlags().StringVar(&flags.remoteAddress, "remote", "", "Use remote runtime with specified address")
	cmd.PersistentFlags().StringVarP(&flags.sessionDB, "session-db", "s", filepath.Join(paths.GetHomeDir(), ".cagent", "session.db"), "Path to the session database")
	cmd.PersistentFlags().StringVar(&flags.recordPath, "record", "", "Record AI API interactions to cassette file (auto-generates filename if empty)")
	cmd.PersistentFlags().Lookup("record").NoOptDefVal = "true"
}

func (f *runExecFlags) runRunCommand(cmd *cobra.Command, args []string) error {
	telemetry.TrackCommand("run", args)

	ctx := cmd.Context()
	out := cli.NewPrinter(cmd.OutOrStdout())

	tui := isatty.IsTerminal(os.Stdout.Fd())
	return f.runOrExec(ctx, out, args, tui)
}

func (f *runExecFlags) runOrExec(ctx context.Context, out *cli.Printer, args []string, tui bool) error {
	slog.Debug("Starting agent", "agent", f.agentName)

	// Record AI API interactions to a cassette file if --record flag is specified.
	if cassettePath, cleanup, err := setupRecordingProxy(f.recordPath, &f.runConfig); err != nil {
		return err
	} else if cassettePath != "" {
		defer cleanup()
		out.Println("Recording mode enabled, cassette: " + cassettePath)
	}

	var agentFileName string
	if len(args) > 0 {
		agentFileName = args[0]
	}

	var (
		rt    runtime.Runtime
		sess  *session.Session
		store session.Store
		err   error
	)
	if f.remoteAddress != "" {
		rt, sess, err = f.createRemoteRuntimeAndSession(ctx, agentFileName)
		if err != nil {
			return err
		}
	} else {
		agentSource, err := config.Resolve(agentFileName)
		if err != nil {
			return err
		}

		t, err := f.loadAgentFrom(ctx, agentSource)
		if err != nil {
			return err
		}

		rt, sess, store, err = f.createLocalRuntimeAndSession(ctx, t)
		if err != nil {
			return err
		}
	}

	if f.dryRun {
		out.Println("Dry run mode enabled. Agent initialized but will not execute.")
		return nil
	}

	if !tui {
		return f.handleExecMode(ctx, out, rt, sess, args)
	}

	return handleRunMode(ctx, rt, sess, store, args)
}

func (f *runExecFlags) loadAgentFrom(ctx context.Context, agentSource config.Source) (*team.Team, error) {
	t, err := teamloader.Load(ctx, agentSource, &f.runConfig, teamloader.WithModelOverrides(f.modelOverrides))
	if err != nil {
		return nil, err
	}

	go func() {
		<-ctx.Done()
		if err := t.StopToolSets(ctx); err != nil {
			slog.Error("Failed to stop tool sets", "error", err)
		}
	}()

	return t, nil
}

func (f *runExecFlags) createRemoteRuntimeAndSession(ctx context.Context, originalFilename string) (runtime.Runtime, *session.Session, error) {
	remoteClient, err := runtime.NewClient(f.remoteAddress)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create remote client: %w", err)
	}

	sessTemplate := session.New(
		session.WithToolsApproved(f.autoApprove),
	)

	sess, err := remoteClient.CreateSession(ctx, sessTemplate)
	if err != nil {
		return nil, nil, err
	}

	remoteRt, err := runtime.NewRemoteRuntime(remoteClient,
		runtime.WithRemoteCurrentAgent(f.agentName),
		runtime.WithRemoteAgentFilename(originalFilename),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create remote runtime: %w", err)
	}

	slog.Debug("Using remote runtime", "address", f.remoteAddress, "agent", f.agentName)
	return remoteRt, sess, nil
}

func (f *runExecFlags) createLocalRuntimeAndSession(ctx context.Context, t *team.Team) (runtime.Runtime, *session.Session, session.Store, error) {
	agent, err := t.Agent(f.agentName)
	if err != nil {
		return nil, nil, nil, err
	}

	sessStore, err := session.NewSQLiteSessionStore(f.sessionDB)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create session store: %w", err)
	}

	localRt, err := runtime.New(t,
		runtime.WithSessionStore(sessStore),
		runtime.WithCurrentAgent(f.agentName),
		runtime.WithTracer(otel.Tracer(AppName)),
	)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create runtime: %w", err)
	}

	sess := session.New(
		session.WithMaxIterations(agent.MaxIterations()),
		session.WithToolsApproved(f.autoApprove),
	)

	if err := sessStore.AddSession(ctx, sess); err != nil {
		return nil, nil, nil, err
	}

	slog.Debug("Using local runtime", "agent", f.agentName)
	return localRt, sess, sessStore, nil
}

func (f *runExecFlags) handleExecMode(ctx context.Context, out *cli.Printer, rt runtime.Runtime, sess *session.Session, args []string) error {
	execArgs := []string{"exec"}
	if len(args) == 2 {
		execArgs = append(execArgs, args[1])
	} else {
		execArgs = append(execArgs, "Follow the default instructions")
	}

	err := cli.Run(ctx, out, cli.Config{
		AppName:        AppName,
		AttachmentPath: f.attachmentPath,
		HideToolCalls:  f.hideToolCalls,
		OutputJSON:     f.outputJSON,
		AutoApprove:    f.autoApprove,
	}, rt, sess, execArgs)
	if cliErr, ok := err.(cli.RuntimeError); ok {
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

func handleRunMode(ctx context.Context, rt runtime.Runtime, sess *session.Session, store session.Store, args []string) error {
	firstMessage, err := readInitialMessage(args)
	if err != nil {
		return err
	}

	return runTUI(ctx, rt, sess, firstMessage, store)
}
