package root

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	goRuntime "runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel"

	"github.com/docker/cagent/internal/app"
	"github.com/docker/cagent/internal/telemetry"
	"github.com/docker/cagent/internal/tui"
	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/content"
	"github.com/docker/cagent/pkg/evaluation"
	"github.com/docker/cagent/pkg/remote"
	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/teamloader"
)

var (
	agentsDir           string
	autoApprove         bool
	attachmentPath      string
	workingDir          string
	useTUI              bool
	hideOutputFor       string
	showTokensEveryStep bool
	showTimestamps      bool
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
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCommand(cmd, args, false)
		},
	}

	cmd.PersistentFlags().StringVarP(&agentName, "agent", "a", "root", "Name of the agent to run")
	cmd.PersistentFlags().StringSliceVar(&runConfig.EnvFiles, "env-from-file", nil, "Set environment variables from file")
	cmd.PersistentFlags().StringVar(&workingDir, "working-dir", "", "Set the working directory for the session (applies to tools and relative paths)")
	cmd.PersistentFlags().BoolVar(&autoApprove, "yolo", false, "Automatically approve all tool calls without prompting")
	cmd.PersistentFlags().StringVar(&attachmentPath, "attach", "", "Attach an image file to the message")
	cmd.PersistentFlags().BoolVar(&useTUI, "tui", true, "Run the agent with a Terminal User Interface (TUI)")
	allOptions := GetAllHideOutputOptions()
	helpText := fmt.Sprintf("Hide output for specific tools (comma-separated). Available: %s", strings.Join(allOptions, ","))
	cmd.PersistentFlags().StringVar(&hideOutputFor, "hide-output-for", "", helpText)
	cmd.PersistentFlags().BoolVar(&showTokensEveryStep, "show-tokens-every-step", false, "Show token usage after every AI API call")
	cmd.PersistentFlags().BoolVar(&showTimestamps, "show-timestamps", false, "Show datetime timestamp before every tool call")
	cmd.PersistentFlags().BoolVar(&runConfig.RetryOnFailure, "retry", false, "Retry chat completion and gateway errors up to 3 times before failing")
	addGatewayFlags(cmd)

	return cmd
}

func NewTuiCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "tui",
		Short:  "Deprecated. Use `cagent run` instead.",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(os.Stderr, "'cagent tui' is deprecated. Please use 'cagent run' instead.")
			return nil
		},
	}
}

func runCommand(_ *cobra.Command, args []string, exec bool) error {
	// Track the run command
	telemetry.TrackCommand("run", args)

	// Validate hide-output-for options
	if err := ValidateHideOutputOptions(hideOutputFor); err != nil {
		return err
	}

	ctx := context.Background()

	slog.Debug("Starting agent", "agent", agentName, "debug_mode", debugMode)

	agentFilename := args[0]
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
			fmt.Println("Pulling agent ", agentFilename)
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

	agents, err := teamloader.Load(ctx, agentFilename, runConfig)
	if err != nil {
		return err
	}
	defer func() {
		if err := agents.StopToolSets(); err != nil {
			slog.Error("Failed to stop tool sets", "error", err)
		}
	}()

	tracer := otel.Tracer(APP_NAME)

	rt, err := runtime.New(agents,
		runtime.WithCurrentAgent(agentName),
		runtime.WithAutoRunTools(autoApprove),
		runtime.WithTracer(tracer),
		runtime.WithRetryOnFailure(runConfig.RetryOnFailure),
	)
	if err != nil {
		return fmt.Errorf("failed to create runtime: %w", err)
	}

	sess := session.New()

	// For `cagent exec`
	if exec {
		return runWithoutTUI(ctx, agentFilename, rt, sess, []string{"exec", "Follow the default instructions"})
	}

	// For `cagent run --tui=false`
	if !useTUI {
		return runWithoutTUI(ctx, agentFilename, rt, sess, args)
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

	a := app.New(agentFilename, rt, sess, firstMessage)
	m := tui.New(a)

	progOpts := []tea.ProgramOption{
		tea.WithAltScreen(),
		tea.WithContext(ctx),
		tea.WithFilter(tui.MouseEventFilter),
		tea.WithMouseCellMotion(),
		tea.WithMouseAllMotion(),
	}
	if goRuntime.GOOS == "windows" {
		// WithInputTTY seems to be required for proper keyboard input
		progOpts = append(progOpts, tea.WithInputTTY())
	}

	p := tea.NewProgram(m, progOpts...)

	go a.Subscribe(ctx, p)

	_, err = p.Run()
	return err
}

func runWithoutTUI(ctx context.Context, agentFilename string, rt *runtime.Runtime, sess *session.Session, args []string) error {
	sess.Title = "Running agent"
	// If the last received event was an error, return it. That way the exit code
	// will be non-zero if the agent failed.
	var lastErr error

	// Track if this is a one-shot run (user provided a command/instruction)
	isOneShotRun := len(args) == 2

	// Track the latest token usage across all loops for interactive mode
	var globalLastTokenUsage *runtime.Usage

	oneLoop := func(text string, scannerConfirmations *bufio.Scanner) error {
		userInput := strings.TrimSpace(text)
		if userInput == "" {
			return nil
		}

		handled, err := runUserCommand(userInput, sess, rt, ctx)
		if err != nil {
			return err
		}

		if handled {
			return nil
		}

		// Create a cancellable context for this agentic loop and wire Ctrl+C to cancel it
		loopCtx, loopCancel := context.WithCancel(ctx)

		// Ensure telemetry is initialized and add to context so runtime can access it
		telemetry.EnsureGlobalTelemetryInitialized()
		if telemetryClient := telemetry.GetGlobalTelemetryClient(); telemetryClient != nil {
			loopCtx = telemetry.WithClient(loopCtx, telemetryClient)
		}

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt)
		go func() {
			<-sigCh
			// Ensure we break any inline typing output nicely
			fmt.Println()
			loopCancel()
		}()
		defer signal.Stop(sigCh)

		// Parse for /attach commands in the message
		messageText, attachPath := parseAttachCommand(userInput)

		// Use either the per-message attachment or the global one
		finalAttachPath := attachPath
		if finalAttachPath == "" {
			finalAttachPath = attachmentPath
		}

		sess.AddMessage(createUserMessageWithAttachment(agentFilename, messageText, finalAttachPath))

		firstLoop := true
		lastAgent := rt.CurrentAgent().Name()
		llmIsTyping := false
		var lastConfirmedToolCallID string
		var lastTokenUsage *runtime.Usage
		for event := range rt.RunStream(loopCtx, sess) {
			if event.GetAgentName() != "" && (firstLoop || lastAgent != event.GetAgentName()) {
				if !firstLoop {
					if llmIsTyping {
						fmt.Println()
						llmIsTyping = false
					}
					fmt.Println()
				}
				printAgentName(event.GetAgentName())
				firstLoop = false
				lastAgent = event.GetAgentName()
			}
			switch e := event.(type) {
			case *runtime.AgentChoiceEvent:
				agentChanged := lastAgent != e.AgentName
				if !llmIsTyping {
					// Only add newline if we're not already typing
					if !agentChanged {
						fmt.Println()
					}
					llmIsTyping = true
				}
				fmt.Printf("%s", e.Content)
			case *runtime.AgentChoiceReasoningEvent:
				fmt.Printf("%s", gray(e.Content))
			case *runtime.ToolCallConfirmationEvent:
				if llmIsTyping {
					fmt.Println()
					llmIsTyping = false
				}
				result := printToolCallWithConfirmation(e.ToolCall, showTimestamps, scannerConfirmations)
				// If interrupted, skip resuming; the runtime will notice context cancellation and stop
				if loopCtx.Err() != nil {
					continue
				}
				lastConfirmedToolCallID = e.ToolCall.ID // Store the ID to avoid duplicate printing
				switch result {
				case ConfirmationApprove:
					rt.Resume(ctx, string(runtime.ResumeTypeApprove))
				case ConfirmationApproveSession:
					sess.ToolsApproved = true
					rt.Resume(ctx, string(runtime.ResumeTypeApproveSession))
				case ConfirmationReject:
					rt.Resume(ctx, string(runtime.ResumeTypeReject))
					lastConfirmedToolCallID = "" // Clear on reject since tool won't execute
				case ConfirmationAbort:
					// Stop the agent loop immediately
					loopCancel()
					continue
				}
			case *runtime.ToolCallEvent:
				if llmIsTyping {
					fmt.Println()
					llmIsTyping = false
				}
				// Only print if this wasn't already shown during confirmation
				if e.ToolCall.ID != lastConfirmedToolCallID {
					printToolCall(e.ToolCall, showTimestamps)
				}
			case *runtime.ToolCallResponseEvent:
				if llmIsTyping {
					fmt.Println()
					llmIsTyping = false
				}
				printToolCallResponse(e.ToolCall, e.Response, hideOutputFor)
				// Clear the confirmed ID after the tool completes
				if e.ToolCall.ID == lastConfirmedToolCallID {
					lastConfirmedToolCallID = ""
				}
			case *runtime.ErrorEvent:
				if llmIsTyping {
					fmt.Println()
					llmIsTyping = false
				}
				lowerErr := strings.ToLower(e.Error)
				if strings.Contains(lowerErr, "context cancel") && loopCtx.Err() != nil { // treat Ctrl+C cancellations as non-errors
					lastErr = nil
				} else {
					lastErr = fmt.Errorf("%s", e.Error)
					printError(lastErr)
				}
			case *runtime.TokenUsageEvent:
				// Track the latest token usage for display at the end
				lastTokenUsage = e.Usage
				globalLastTokenUsage = e.Usage // Also update global tracking for interactive mode

				// Show token usage after every step if flag is enabled
				if showTokensEveryStep {
					if llmIsTyping {
						fmt.Println()
						llmIsTyping = false
					}
					printTokenUsageStep(e.Usage)
				}
			}
		}

		// If the loop ended due to Ctrl+C, inform the user succinctly
		if loopCtx.Err() != nil {
			fmt.Println(yellow("\n⚠️  agent stopped  ⚠️"))
		}

		// Display token usage and cost at the end of one-shot runs only
		if lastTokenUsage != nil && isOneShotRun {
			printTokenUsageSummary(lastTokenUsage)
		}

		// Wrap runtime errors to prevent duplicate error messages and usage display
		if lastErr != nil {
			return RuntimeError{Err: lastErr}
		}
		return nil
	}

	if len(args) == 2 {
		if args[1] == "-" {
			buf, err := io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("failed to read from stdin: %w", err)
			}

			if err := oneLoop(string(buf), bufio.NewScanner(os.Stdin)); err != nil {
				return err
			}
		} else {
			if err := oneLoop(args[1], bufio.NewScanner(os.Stdin)); err != nil {
				return err
			}
		}
	} else {
		// Interactive mode - set up global signal handler for Ctrl+C
		globalSigCh := make(chan os.Signal, 1)
		signal.Notify(globalSigCh, os.Interrupt)
		go func() {
			<-globalSigCh
			// Display token summary if available
			if globalLastTokenUsage != nil {
				printTokenUsageSummary(globalLastTokenUsage)
			}
			os.Exit(0)
		}()
		defer signal.Stop(globalSigCh)

		printWelcomeMessage()
		scanner := bufio.NewScanner(os.Stdin)
		firstQuestion := true
		for {
			if !firstQuestion {
				fmt.Print("\n\n")
			}
			fmt.Print(blue("> "))
			firstQuestion = false

			if !scanner.Scan() {
				break
			}

			if err := oneLoop(scanner.Text(), scanner); err != nil {
				return err
			}
		}

		if err := scanner.Err(); err != nil {
			return err
		}
	}

	// Wrap runtime errors to prevent duplicate error messages and usage display
	if lastErr != nil {
		return RuntimeError{Err: lastErr}
	}
	return nil
}

func runUserCommand(userInput string, sess *session.Session, rt *runtime.Runtime, ctx context.Context) (bool, error) {
	yellow := color.New(color.FgYellow).SprintfFunc()
	switch userInput {
	case "/exit":
		os.Exit(0)
	case "/eval":
		err := evaluation.Save(sess)
		if err == nil {
			fmt.Printf("%s\n", yellow("Evaluation saved"))
			return true, err
		}
		return true, nil
	case "/usage":
		fmt.Printf("%s\n", yellow("Input tokens: %d", sess.InputTokens))
		fmt.Printf("%s\n", yellow("Output tokens: %d", sess.OutputTokens))
		return true, nil
	case "/reset":
		// Reset session items
		sess.Messages = []session.Item{}
		return true, nil
	case "/compact":
		// Generate a summary of the session and compact the history
		fmt.Printf("%s\n", yellow("Generating summary..."))

		// Create a channel to capture summary events
		events := make(chan runtime.Event, 100)

		// Generate the summary
		rt.Summarize(ctx, sess, events)

		// Process events and show the summary
		close(events)
		summaryGenerated := false
		for event := range events {
			if summaryEvent, ok := event.(*runtime.SessionSummaryEvent); ok {
				fmt.Printf("%s\n", yellow("Summary generated and added to session"))
				fmt.Printf("Summary: %s\n", summaryEvent.Summary)
				summaryGenerated = true
			}
		}

		if !summaryGenerated {
			fmt.Printf("%s\n", yellow("No summary generated"))
		}

		return true, nil
	}

	return false, nil
}

// parseAttachCommand parses user input for /attach commands
// Returns the message text (with /attach commands removed) and the attachment path
func parseAttachCommand(input string) (messageText, attachPath string) {
	lines := strings.Split(input, "\n")
	var messageLines []string

	for _, line := range lines {
		// Look for /attach anywhere in the line
		attachIndex := strings.Index(line, "/attach ")
		if attachIndex != -1 {
			// Extract the part before /attach
			beforeAttach := line[:attachIndex]

			// Extract the part after /attach (starting after "/attach ")
			afterAttachStart := attachIndex + 8 // Length of "/attach "
			if afterAttachStart < len(line) {
				afterAttach := line[afterAttachStart:]

				// Split on spaces to get the file path (first token) and any remaining text
				tokens := strings.Fields(afterAttach)
				if len(tokens) > 0 {
					attachPath = tokens[0]

					// Reconstruct the line with /attach and file path removed
					var remainingText string
					if len(tokens) > 1 {
						remainingText = strings.Join(tokens[1:], " ")
					}

					// Combine the text before /attach and any text after the file path
					var parts []string
					if strings.TrimSpace(beforeAttach) != "" {
						parts = append(parts, strings.TrimSpace(beforeAttach))
					}
					if remainingText != "" {
						parts = append(parts, remainingText)
					}
					reconstructedLine := strings.Join(parts, " ")
					if reconstructedLine != "" {
						messageLines = append(messageLines, reconstructedLine)
					}
				}
			}
		} else {
			// Keep lines without /attach commands
			messageLines = append(messageLines, line)
		}
	}

	// Join the message lines back together
	messageText = strings.TrimSpace(strings.Join(messageLines, "\n"))
	return messageText, attachPath
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

// createUserMessageWithAttachment creates a user message with optional image attachment
func createUserMessageWithAttachment(agentFilename, userContent, attachmentPath string) *session.Message {
	if attachmentPath == "" {
		return session.UserMessage(agentFilename, userContent)
	}

	// Convert file to data URL
	dataURL, err := fileToDataURL(attachmentPath)
	if err != nil {
		fmt.Printf("Warning: Failed to attach file %s: %v\n", attachmentPath, err)
		return session.UserMessage(agentFilename, userContent)
	}

	// Ensure we have some text content when attaching a file
	textContent := userContent
	if strings.TrimSpace(textContent) == "" {
		textContent = "Please analyze this attached file."
	}

	// Create message with multi-content including text and image
	multiContent := []chat.MessagePart{
		{
			Type: chat.MessagePartTypeText,
			Text: textContent,
		},
		{
			Type: chat.MessagePartTypeImageURL,
			ImageURL: &chat.MessageImageURL{
				URL:    dataURL,
				Detail: chat.ImageURLDetailAuto,
			},
		},
	}

	return &session.Message{
		AgentFilename: agentFilename,
		AgentName:     "",
		Message: chat.Message{
			Role:         chat.MessageRoleUser,
			MultiContent: multiContent,
			CreatedAt:    time.Now().Format(time.RFC3339),
		},
	}
}

// fileToDataURL converts a file to a data URL
func fileToDataURL(filePath string) (string, error) {
	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return "", fmt.Errorf("file does not exist: %s", filePath)
	}

	// Read file content
	fileBytes, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	// Determine MIME type based on file extension
	ext := strings.ToLower(filepath.Ext(filePath))
	var mimeType string
	switch ext {
	case ".jpg", ".jpeg":
		mimeType = "image/jpeg"
	case ".png":
		mimeType = "image/png"
	case ".gif":
		mimeType = "image/gif"
	case ".webp":
		mimeType = "image/webp"
	case ".bmp":
		mimeType = "image/bmp"
	case ".svg":
		mimeType = "image/svg+xml"
	default:
		return "", fmt.Errorf("unsupported image format: %s", ext)
	}

	// Encode to base64
	encoded := base64.StdEncoding.EncodeToString(fileBytes)

	// Create data URL
	dataURL := fmt.Sprintf("data:%s;base64,%s", mimeType, encoded)

	return dataURL, nil
}
