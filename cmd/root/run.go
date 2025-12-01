package root

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel"

	"github.com/docker/cagent/pkg/cli"
	"github.com/docker/cagent/pkg/config"
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

	var agentFileName string
	if len(args) > 0 {
		agentFileName = args[0]
	}

	var (
		rt          runtime.Runtime
		sess        *session.Session
		err         error
		agentSource config.Source
	)
	if f.remoteAddress != "" {
		rt, sess, err = f.createRemoteRuntimeAndSession(ctx, agentFileName)
		if err != nil {
			return err
		}
	} else {
		agentSource, err = config.Resolve(agentFileName)
		if err != nil {
			return err
		}

		t, err := f.loadAgentFrom(ctx, agentSource)
		if err != nil {
			return err
		}

		rt, sess, err = f.createLocalRuntimeAndSession(t, agentSource)
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

	return handleRunMode(ctx, rt, sess, args)
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

func (f *runExecFlags) createLocalRuntimeAndSession(t *team.Team, agentSource config.Source) (runtime.Runtime, *session.Session, error) {
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
		runtime.WithConfigSource(agentSource),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create runtime: %w", err)
	}

	slog.Debug("Using local runtime", "agent", f.agentName)
	return localRt, sess, nil
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

func handleRunMode(ctx context.Context, rt runtime.Runtime, sess *session.Session, args []string) error {
	firstMessage, err := readInitialMessage(args)
	if err != nil {
		return err
	}

	return runTUI(ctx, rt, sess, firstMessage)
}
