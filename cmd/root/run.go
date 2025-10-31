package root

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel"

	"github.com/docker/cagent/pkg/aliases"
	"github.com/docker/cagent/pkg/app"
	"github.com/docker/cagent/pkg/cli"
	"github.com/docker/cagent/pkg/content"
	"github.com/docker/cagent/pkg/remote"
	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/team"
	"github.com/docker/cagent/pkg/teamloader"
	"github.com/docker/cagent/pkg/telemetry"
	"github.com/docker/cagent/pkg/tui"
)

var (
	agentsDir      string
	autoApprove    bool
	attachmentPath string
	workingDir     string
	useTUI         bool
	remoteAddress  string
	dryRun         bool
	modelOverrides []string
)

// NewRunCmd creates a new run command
func NewRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run <agent-name> [message|-]",
		Short: "Run an agent",
		Long:  `Run an agent with the specified configuration and prompt`,
		Example: `  cagent run ./agent.yaml
  cagent run ./team.yaml --agent root
  cagent run ./echo.yaml "INSTRUCTIONS"
  echo "INSTRUCTIONS" | cagent run ./echo.yaml -`,
		Args: cobra.RangeArgs(1, 2),
		RunE: runCommand,
	}

	cmd.PersistentFlags().StringVarP(&agentName, "agent", "a", "root", "Name of the agent to run")
	cmd.PersistentFlags().StringVar(&workingDir, "working-dir", "", "Set the working directory for the session (applies to tools and relative paths)")
	cmd.PersistentFlags().BoolVar(&autoApprove, "yolo", false, "Automatically approve all tool calls without prompting")
	cmd.PersistentFlags().StringVar(&attachmentPath, "attach", "", "Attach an image file to the message")
	cmd.PersistentFlags().BoolVar(&useTUI, "tui", true, "Run the agent with a Terminal User Interface (TUI)")
	cmd.PersistentFlags().StringVar(&remoteAddress, "remote", "", "Use remote runtime with specified address (only supported with TUI)")
	cmd.PersistentFlags().StringArrayVar(&modelOverrides, "model", nil, "Override agent model: [agent=]provider/model (repeatable)")
	addGatewayFlags(cmd)
	addRuntimeConfigFlags(cmd)

	return cmd
}

func runCommand(cmd *cobra.Command, args []string) error {
	telemetry.TrackCommand("run", args)
	return doRunCommand(cmd.Context(), args, false)
}

func execCommand(cmd *cobra.Command, args []string) error {
	telemetry.TrackCommand("exec", args)
	return doRunCommand(cmd.Context(), args, true)
}

func doRunCommand(ctx context.Context, args []string, exec bool) error {
	slog.Debug("Starting agent", "agent", agentName, "debug_mode", debugMode)

	agentFilename := args[0]

	// Try to resolve as an alias first
	if aliasStore, err := aliases.Load(); err == nil {
		if resolvedPath, ok := aliasStore.Get(agentFilename); ok {
			slog.Debug("Resolved alias", "alias", agentFilename, "path", resolvedPath)
			agentFilename = resolvedPath
		}
	}

	if !strings.Contains(agentFilename, "\n") && (strings.Contains(agentFilename, ".yaml") || strings.Contains(agentFilename, ".yml")) {
		if abs, err := filepath.Abs(agentFilename); err == nil {
			agentFilename = abs
		}
	}

	if enableOtel {
		shutdown, err := initOTelSDK(ctx)
		if err != nil {
			slog.Warn("Failed to initialize OpenTelemetry SDK", "error", err)
		} else if shutdown != nil {
			defer func() {
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := shutdown(shutdownCtx); err != nil {
					slog.Warn("Failed to shutdown OpenTelemetry SDK", "error", err)
				}
			}()
			slog.Debug("OpenTelemetry SDK initialized successfully")
		}
	}

	// If working-dir was provided, validate and change process working directory
	if workingDir != "" {
		absWd, err := filepath.Abs(workingDir)
		if err != nil {
			return fmt.Errorf("invalid working directory: %w", err)
		}
		info, err := os.Stat(absWd)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("working directory does not exist or is not a directory: %s", absWd)
		}
		if err := os.Chdir(absWd); err != nil {
			return fmt.Errorf("failed to change working directory: %w", err)
		}
		_ = os.Setenv("PWD", absWd)
		slog.Debug("Working directory set", "dir", absWd)
	}

	// Skip agent file loading when using remote runtime
	var agents *team.Team
	var err error
	if remoteAddress == "" {
		// Determine how to obtain the agent definition
		ext := strings.ToLower(filepath.Ext(agentFilename))
		if ext == ".yaml" || ext == ".yml" || strings.HasPrefix(agentFilename, "/dev/fd/") {
			// Treat as local YAML file: resolve to absolute path so later chdir doesn't break it
			if !strings.Contains(agentFilename, "\n") {
				if abs, err := filepath.Abs(agentFilename); err == nil {
					agentFilename = abs
				}
			}
			if !fileExists(agentFilename) {
				return fmt.Errorf("agent file not found: %s", agentFilename)
			}
		} else {
			// Treat as an OCI image reference. Try local store first, otherwise pull then load.
			a, err := fromStore(agentFilename)
			if err != nil {
				fmt.Println("Pulling agent", agentFilename)
				if _, pullErr := remote.Pull(agentFilename); pullErr != nil {
					return fmt.Errorf("failed to pull OCI image %s: %w", agentFilename, pullErr)
				}
				// Retry after pull
				a, err = fromStore(agentFilename)
				if err != nil {
					return fmt.Errorf("failed to load agent from store after pull: %w", err)
				}
			}

			// Write the fetched content to a temporary YAML file
			tmpFile, err := os.CreateTemp("", "agentfile-*.yaml")
			if err != nil {
				return err
			}
			defer os.Remove(tmpFile.Name())
			if _, err := tmpFile.WriteString(a); err != nil {
				tmpFile.Close()
				return err
			}
			if err := tmpFile.Close(); err != nil {
				return err
			}
			agentFilename = tmpFile.Name()
		}

		if runConfig.RedirectURI == "" {
			runConfig.RedirectURI = "http://localhost:8083/oauth-callback"
		}

		agents, err = teamloader.Load(ctx, agentFilename, runConfig, teamloader.WithModelOverrides(modelOverrides))
		if err != nil {
			return err
		}
		defer func() {
			if err := agents.StopToolSets(ctx); err != nil {
				slog.Error("Failed to stop tool sets", "error", err)
			}
		}()
	} else {
		// For remote runtime, just store the original agent filename
		// The remote server will handle agent loading
		slog.Debug("Skipping local agent file loading for remote runtime", "filename", agentFilename)
	}

	// Validate remote flag usage
	if remoteAddress != "" && (!useTUI || exec) {
		return fmt.Errorf("--remote flag can only be used with TUI mode")
	}

	tracer := otel.Tracer(AppName)

	var sess *session.Session

	// Create runtime based on whether remote flag is specified
	var rt runtime.Runtime
	if remoteAddress != "" && useTUI && !exec {
		// Create remote runtime for TUI mode
		remoteClient, err := runtime.NewClient(remoteAddress)
		if err != nil {
			return fmt.Errorf("failed to create remote client: %w", err)
		}

		// Attach agent metadata so remote runtimes can attribute usage correctly.
		sessTemplate := session.New(session.WithAgentMetadata(agentName, ""))
		sessTemplate.ToolsApproved = autoApprove
		sess, err = remoteClient.CreateSession(ctx, sessTemplate)
		if err != nil {
			return err
		}

		remoteRt, err := runtime.NewRemoteRuntime(remoteClient,
			runtime.WithRemoteCurrentAgent(agentName),
			runtime.WithRemoteAgentFilename(args[0]),
		)
		if err != nil {
			return fmt.Errorf("failed to create remote runtime: %w", err)
		}
		rt = remoteRt
		slog.Debug("Using remote runtime", "address", remoteAddress, "agent", agentName)
	} else {
		agent, err := agents.Agent(agentName)
		if err != nil {
			return err
		}

		// Create session first to get its ID for OAuth state encoding
		sess = session.New(
			// Provide agent metadata so local sessions report attribution like remote ones.
			session.WithMaxIterations(agent.MaxIterations()),
			session.WithAgentMetadata(agentName, ""),
		)
		sess.ToolsApproved = autoApprove

		// Create local runtime with root session ID for OAuth state encoding
		localRt, err := runtime.New(agents,
			runtime.WithCurrentAgent(agentName),
			runtime.WithTracer(tracer),
			runtime.WithRootSessionID(sess.ID),
		)
		if err != nil {
			return fmt.Errorf("failed to create runtime: %w", err)
		}
		rt = localRt
		slog.Debug("Using local runtime", "agent", agentName)
	}

	// For `cagent exec`
	if exec {
		execArgs := []string{"exec"}
		if len(args) == 2 {
			execArgs = append(execArgs, args[1])
		} else {
			execArgs = append(execArgs, "Follow the default instructions")
		}

		if dryRun {
			fmt.Println("Dry run mode enabled. Agent initialized but will not execute.")
			return nil
		}
		err := cli.Run(ctx, cli.Config{
			AppName:        AppName,
			AttachmentPath: attachmentPath,
		}, agentFilename, rt, sess, execArgs)
		if cliErr, ok := err.(cli.RuntimeError); ok {
			return RuntimeError{Err: cliErr.Err}
		}
		return err
	}

	// For `cagent run --tui=false`
	if !useTUI {
		err := cli.Run(ctx, cli.Config{
			AppName:        AppName,
			AttachmentPath: attachmentPath,
		}, agentFilename, rt, sess, args)
		if cliErr, ok := err.(cli.RuntimeError); ok {
			return RuntimeError{Err: cliErr.Err}
		}
		return err
	}

	// The default is to use the TUI
	var firstMessage *string
	if len(args) == 2 {
		// TODO: attachments
		if args[1] == "-" {
			buf, err := io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("failed to read from stdin: %w", err)
			}
			text := string(buf)
			firstMessage = &text
		} else {
			firstMessage = &args[1]
		}
	}

	a := app.New("cagent", agentFilename, rt, agents, sess, firstMessage)
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
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	exists := err == nil
	return exists
}

func fromStore(reference string) (string, error) {
	store, err := content.NewStore()
	if err != nil {
		return "", err
	}

	img, err := store.GetArtifactImage(reference)
	if err != nil {
		return "", err
	}

	layers, err := img.Layers()
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	layer := layers[0]
	b, err := layer.Uncompressed()
	if err != nil {
		return "", err
	}

	_, err = io.Copy(&buf, b)
	if err != nil {
		return "", err
	}
	b.Close()

	return buf.String(), nil
}
