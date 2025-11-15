package root

import (
	"context"
	_ "embed"
	"fmt"
	"io"
	"log/slog"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel"

	"github.com/docker/cagent/pkg/agentfile"
	"github.com/docker/cagent/pkg/app"
	"github.com/docker/cagent/pkg/cli"
	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/team"
	"github.com/docker/cagent/pkg/teamloader"
	"github.com/docker/cagent/pkg/telemetry"
	"github.com/docker/cagent/pkg/tui"
)

//go:embed default-agent.yaml
var defaultAgent []byte

type runExecFlags struct {
	agentName      string
	autoApprove    bool
	attachmentPath string
	remoteAddress  string
	modelOverrides []string
	dryRun         bool
	sessionDB      string
	runConfig      config.RuntimeConfig
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
  echo "INSTRUCTIONS" | cagent run ./echo.yaml -`,
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
	cmd.PersistentFlags().StringVar(&flags.sessionDB, "session-db", "sessions.db", "Path to the session database file")
}

func (f *runExecFlags) runRunCommand(cmd *cobra.Command, args []string) error {
	telemetry.TrackCommand("run", args)

	ctx := cmd.Context()
	out := cli.NewPrinter(cmd.OutOrStdout())

	return f.runOrExec(ctx, out, args, false)
}

func (f *runExecFlags) runOrExec(ctx context.Context, out *cli.Printer, args []string, exec bool) error {
	slog.Debug("Starting agent", "agent", f.agentName)

	var agentFileName string
	if len(args) > 0 {
		agentFileName = args[0]
	}

	var (
		rt   runtime.Runtime
		sess *session.Session
		err  error
	)
	if f.remoteAddress != "" {
		rt, sess, err = f.createRemoteRuntimeAndSession(ctx, agentFileName)
		if err != nil {
			return err
		}
	} else {
		agentFileName, err := agentfile.Resolve(ctx, out, agentFileName)
		if err != nil {
			return err
		}

		var source teamloader.AgentSource
		if agentFileName == "default" {
			source = teamloader.NewBytesSource(f.runConfig.WorkingDir, defaultAgent)
		} else {
			source = teamloader.NewFileSource(agentFileName)
		}

		t, err := f.loadAgentFrom(ctx, source)
		if err != nil {
			return err
		}

		rt, sess, err = f.createLocalRuntimeAndSession(t)
		if err != nil {
			return err
		}
	}

	if f.dryRun {
		out.Println("Dry run mode enabled. Agent initialized but will not execute.")
		return nil
	}

	if exec {
		return f.handleExecMode(ctx, out, agentFileName, rt, sess, args)
	}

	// Create session store for TUI mode (not needed for exec mode)
	var sessionStore session.Store
	if f.sessionDB != "" {
		var err error
		sessionStore, err = session.NewSQLiteSessionStore(f.sessionDB)
		if err != nil {
			return fmt.Errorf("failed to create session store: %w", err)
		}
	}

	return handleRunMode(ctx, agentFileName, rt, sess, sessionStore, args)
}

func (f *runExecFlags) loadAgentFrom(ctx context.Context, source teamloader.AgentSource) (*team.Team, error) {
	t, err := teamloader.LoadFrom(ctx, source, &f.runConfig, teamloader.WithModelOverrides(f.modelOverrides))
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

func (f *runExecFlags) createLocalRuntimeAndSession(t *team.Team) (runtime.Runtime, *session.Session, error) {
	agent, err := t.Agent(f.agentName)
	if err != nil {
		return nil, nil, err
	}

	sess := session.New(
		session.WithMaxIterations(agent.MaxIterations()),
		session.WithToolsApproved(f.autoApprove),
	)

	localRt, err := runtime.New(t,
		runtime.WithCurrentAgent(f.agentName),
		runtime.WithTracer(otel.Tracer(AppName)),
		runtime.WithRootSessionID(sess.ID),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create runtime: %w", err)
	}

	slog.Debug("Using local runtime", "agent", f.agentName)
	return localRt, sess, nil
}

func (f *runExecFlags) handleExecMode(ctx context.Context, out *cli.Printer, agentFilename string, rt runtime.Runtime, sess *session.Session, args []string) error {
	execArgs := []string{"exec"}
	if len(args) == 2 {
		execArgs = append(execArgs, args[1])
	} else {
		execArgs = append(execArgs, "Follow the default instructions")
	}

	err := cli.Run(ctx, out, cli.Config{
		AppName:        AppName,
		AttachmentPath: f.attachmentPath,
	}, agentFilename, rt, sess, execArgs)
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

func handleRunMode(ctx context.Context, agentFilename string, rt runtime.Runtime, sess *session.Session, sessionStore session.Store, args []string) error {
	firstMessage, err := readInitialMessage(args)
	if err != nil {
		return err
	}

	a := app.New(agentFilename, rt, sess, firstMessage, sessionStore)
	m := tui.New(a)

	progOpts := []tea.ProgramOption{
		tea.WithContext(ctx),
		tea.WithFilter(tui.MouseEventFilter),
	}

	p := tea.NewProgram(m, progOpts...)

	go a.Subscribe(ctx, p)

	_, err = p.Run()
	return err
}
