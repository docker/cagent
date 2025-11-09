package root

import (
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"

	"github.com/docker/cagent/pkg/app"
	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/creator"
	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/telemetry"
	"github.com/docker/cagent/pkg/tui"
)

type newFlags struct {
	modelParam         string
	maxTokensParam     int
	maxIterationsParam int
	runConfig          config.RuntimeConfig
}

func newNewCmd() *cobra.Command {
	var flags newFlags

	cmd := &cobra.Command{
		Use:   "new",
		Short: "Create a new agent configuration",
		Long:  `Create a new agent configuration by asking questions and generating a YAML file`,
		RunE:  flags.runNewCommand,
	}

	cmd.PersistentFlags().StringVar(&flags.modelParam, "model", "", "Model to use, optionally as provider/model where provider is one of: anthropic, openai, google, dmr. If omitted, provider is auto-selected based on available credentials or gateway")
	cmd.PersistentFlags().IntVar(&flags.maxTokensParam, "max-tokens", 0, "Override max_tokens for the selected model (0 = default)")
	cmd.PersistentFlags().IntVar(&flags.maxIterationsParam, "max-iterations", 0, "Maximum number of agentic loop iterations to prevent infinite loops (default: 20 for DMR, unlimited for other providers)")

	return cmd
}

func (f *newFlags) runNewCommand(cmd *cobra.Command, args []string) error {
	telemetry.TrackCommand("new", args)

	ctx := cmd.Context()

	var model string         // final model name
	var modelProvider string // final provider name

	// Parse provider from --model if specified as "provider/model" where provider is recognized
	derivedProvider := ""
	if idx := strings.Index(f.modelParam, "/"); idx > 0 {
		candidate := strings.ToLower(f.modelParam[:idx])
		switch candidate {
		case "anthropic", "openai", "google", "mistral", "dmr":
			derivedProvider = candidate
			model = f.modelParam[idx+1:]
		}
	}

	// Determine provider
	if derivedProvider != "" {
		modelProvider = derivedProvider
	} else {
		if f.runConfig.ModelsGateway == "" {
			switch {
			case os.Getenv("ANTHROPIC_API_KEY") != "":
				modelProvider = "anthropic"
			case os.Getenv("OPENAI_API_KEY") != "":
				modelProvider = "openai"
			case os.Getenv("GOOGLE_API_KEY") != "":
				modelProvider = "google"
			case os.Getenv("MISTRAL_API_KEY") != "":
				modelProvider = "mistral"
			default:
				modelProvider = "dmr"
			}
		} else {
			// Using Models Gateway; default to Anthropic if not specified
			modelProvider = "anthropic"
		}
	}

	t, err := creator.Agent(ctx, ".", f.runConfig, modelProvider, f.maxTokensParam, model)
	if err != nil {
		return err
	}
	rt, err := runtime.New(t)
	if err != nil {
		return err
	}

	var prompt *string
	opts := []session.Opt{
		session.WithTitle("New agent"),
		session.WithMaxIterations(f.maxIterationsParam),
		session.WithToolsApproved(true),
	}
	if len(args) > 0 {
		arg := strings.Join(args, " ")
		opts = append(opts, session.WithUserMessage("", arg))
		prompt = &arg
	}

	sess := session.New(opts...)

	a := app.New("", rt, sess, prompt)
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
