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
)

type newFlags struct {
	modelParam         string
	maxIterationsParam int
	runConfig          config.RuntimeConfig
}

func newNewCmd() *cobra.Command {
	var flags newFlags

	cmd := &cobra.Command{
		Use:     "new",
		Short:   "Create a new agent configuration",
		Long:    `Create a new agent configuration by asking questions and generating a YAML file`,
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
	// For local runtime, create and pass a title generator
	if localRt, ok := rt.(*runtime.LocalRuntime); ok {
		if model := localRt.CurrentAgent().Model(); model != nil {
			opts = append(opts, app.WithTitleGenerator(sessiontitle.New(model)))
		}
	}

	a := app.New(ctx, rt, sess, opts...)
	m := tui.New(ctx, a)

	p := tea.NewProgram(m, tea.WithContext(ctx))
	go a.Subscribe(ctx, p)

	_, err := p.Run()
	return err
}
