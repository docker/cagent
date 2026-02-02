package root

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"

	"github.com/docker/cagent/pkg/app"
	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/creator"
	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/sessiontitle"
	"github.com/docker/cagent/pkg/telemetry"
	"github.com/docker/cagent/pkg/tui"
	tuiinput "github.com/docker/cagent/pkg/tui/input"
)

type newFlags struct {
	modelParam         string
	maxIterationsParam int
	runConfig          config.RuntimeConfig
}

func newNewCmd() *cobra.Command {
	var flags newFlags

	cmd := &cobra.Command{
		Use:   "new [description]",
		Short: "Create a new agent configuration",
		Long: `Create a new agent configuration interactively.

The agent builder will ask questions about what you want the agent to do,
then generate a YAML configuration file you can use with 'cagent run'.

Optionally provide a description as an argument to skip the initial prompt.`,
		Example: `  cagent new
  cagent new "a web scraper that extracts product prices"
  cagent new --model openai/gpt-4o "a code reviewer agent"`,
		GroupID: "core",
		RunE:    flags.runNewCommand,
	}

	cmd.PersistentFlags().StringVar(&flags.modelParam, "model", "", "Model to use, optionally as provider/model where provider is one of: anthropic, openai, google, dmr. If omitted, provider is auto-selected based on available credentials or gateway")
	cmd.PersistentFlags().IntVar(&flags.maxIterationsParam, "max-iterations", 0, "Maximum number of agentic loop iterations to prevent infinite loops (default: 20 for DMR, unlimited for other providers)")
	addRuntimeConfigFlags(cmd, &flags.runConfig)

	return cmd
}

func (f *newFlags) runNewCommand(cmd *cobra.Command, args []string) error {
	telemetry.TrackCommand("new", args)

	ctx := cmd.Context()

	t, err := creator.Agent(ctx, &f.runConfig, f.modelParam)
	if err != nil {
		return err
	}
	defer func() {
		// Use a fresh context for cleanup since the original may be canceled
		cleanupCtx := context.WithoutCancel(ctx)
		_ = t.StopToolSets(cleanupCtx)
	}()

	rt, err := runtime.New(t)
	if err != nil {
		return err
	}

	var appOpts []app.Opt
	sessOpts := []session.Opt{
		session.WithTitle("New agent"),
		session.WithMaxIterations(f.maxIterationsParam),
		session.WithToolsApproved(true),
	}
	if len(args) > 0 {
		arg := strings.Join(args, " ")
		sessOpts = append(sessOpts, session.WithUserMessage(arg))
		appOpts = append(appOpts, app.WithFirstMessage(arg))
	}

	sess := session.New(sessOpts...)

	return runTUI(ctx, rt, sess, appOpts...)
}

func runTUI(ctx context.Context, rt runtime.Runtime, sess *session.Session, opts ...app.Opt) error {
	// For local runtime, create and pass a title generator.
	if pr, ok := rt.(*runtime.PersistentRuntime); ok {
		if model := pr.CurrentAgent().Model(); model != nil {
			opts = append(opts, app.WithTitleGenerator(sessiontitle.New(model)))
		}
	}

	a := app.New(ctx, rt, sess, opts...)
	m := tui.New(ctx, a)

	coalescer := tuiinput.NewWheelCoalescer()
	filter := func(model tea.Model, msg tea.Msg) tea.Msg {
		wheelMsg, ok := msg.(tea.MouseWheelMsg)
		if !ok {
			return msg
		}
		if coalescer.Handle(wheelMsg) {
			return nil
		}
		return msg
	}

	p := tea.NewProgram(m, tea.WithContext(ctx), tea.WithFilter(filter))
	coalescer.SetSender(p.Send)
	go a.Subscribe(ctx, p)

	_, err := p.Run()
	return err
}
