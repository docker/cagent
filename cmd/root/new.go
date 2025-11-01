package root

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/spf13/cobra"

	"github.com/docker/cagent/pkg/app"
	"github.com/docker/cagent/pkg/cli"
	"github.com/docker/cagent/pkg/creator"
	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/telemetry"
	"github.com/docker/cagent/pkg/tui"
)

var (
	modelParam         string
	maxTokensParam     int
	maxIterationsParam int
)

// NewNewCmd creates a new command to create a new agent configuration
func NewNewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "new",
		Short: "Create a new agent configuration",
		Long:  `Create a new agent configuration by asking questions and generating a YAML file`,
		RunE: func(cmd *cobra.Command, args []string) error {
			telemetry.TrackCommand("new", args)

			ctx := cmd.Context()

			var model string         // final model name
			var modelProvider string // final provider name

			// Parse provider from --model if specified as "provider/model" where provider is recognized
			derivedProvider := ""
			if idx := strings.Index(modelParam, "/"); idx > 0 {
				candidate := strings.ToLower(modelParam[:idx])
				switch candidate {
				case "anthropic", "openai", "google", "dmr":
					derivedProvider = candidate
					model = modelParam[idx+1:]
				}
			}

			// Determine provider
			if derivedProvider != "" {
				modelProvider = derivedProvider
			} else {
				if runConfig.ModelsGateway == "" {
					// Prefer Anthropic, then OpenAI, then Google based on available API keys
					// default to DMR if no provider credentials are found
					switch {
					case os.Getenv("ANTHROPIC_API_KEY") != "":
						modelProvider = "anthropic"
						fmt.Printf("%s\n\n", cli.White("ANTHROPIC_API_KEY found, using Anthropic"))
					case os.Getenv("OPENAI_API_KEY") != "":
						modelProvider = "openai"
						fmt.Printf("%s\n\n", cli.White("OPENAI_API_KEY found, using OpenAI"))
					case os.Getenv("GOOGLE_API_KEY") != "":
						modelProvider = "google"
						fmt.Printf("%s\n\n", cli.White("GOOGLE_API_KEY found, using Google"))
					default:
						modelProvider = "dmr"
						fmt.Printf("%s\n\n", cli.Yellow("⚠️ No provider credentials found, defaulting to Docker Model Runner (DMR)"))
					}
					if modelParam == "" {
						fmt.Printf("%s\n\n", cli.White("use \"--model provider/model\" to use a different model"))
					}
				} else {
					// Using Models Gateway; default to Anthropic if not specified
					modelProvider = "anthropic"
				}
			}

			t, err := creator.Agent(ctx, ".", runConfig, modelProvider, maxTokensParam, model)
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
				session.WithMaxIterations(maxIterationsParam),
				session.WithToolsApproved(true),
			}
			if len(args) > 0 {
				arg := strings.Join(args, " ")
				opts = append(opts, session.WithUserMessage("", arg))
				prompt = &arg
			}

			sess := session.New(opts...)

			a := app.New("cagent", "", rt, t, sess, prompt)
			m := tui.New(a)

			progOpts := []tea.ProgramOption{
				tea.WithAltScreen(),
				tea.WithContext(ctx),
				tea.WithFilter(tui.MouseEventFilter),
				tea.WithMouseCellMotion(),
				tea.WithMouseAllMotion(),
			}

			p := tea.NewProgram(m, progOpts...)

			go a.Subscribe(ctx, p)

			_, err = p.Run()
			return err
		},
	}

	addGatewayFlags(cmd)
	cmd.PersistentFlags().StringVar(&modelParam, "model", "", "Model to use, optionally as provider/model where provider is one of: anthropic, openai, google, dmr. If omitted, provider is auto-selected based on available credentials or gateway")
	cmd.PersistentFlags().IntVar(&maxTokensParam, "max-tokens", 0, "Override max_tokens for the selected model (0 = default)")
	cmd.PersistentFlags().IntVar(&maxIterationsParam, "max-iterations", 0, "Maximum number of agentic loop iterations to prevent infinite loops (default: 20 for DMR, unlimited for other providers)")

	return cmd
}
