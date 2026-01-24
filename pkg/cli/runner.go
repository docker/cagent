package cli

import (
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/input"
	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/telemetry"
)

// RuntimeError wraps runtime errors to distinguish them from usage errors
type RuntimeError struct {
	Err error
}

func (e RuntimeError) Error() string {
	return e.Err.Error()
}

func (e RuntimeError) Unwrap() error {
	return e.Err
}

// Config holds configuration for running an agent in CLI mode
type Config struct {
	AppName        string
	AttachmentPath string
	AutoApprove    bool
	HideToolCalls  bool
	OutputJSON     bool
}

// Run executes an agent in non-TUI mode, handling user input and runtime events
func Run(ctx context.Context, out *Printer, cfg Config, rt runtime.Runtime, sess *session.Session, args []string) error {
	// Create a cancellable context for this agentic loop and wire Ctrl+C to cancel it
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Ensure telemetry is initialized and add to context so runtime can access it
	telemetry.EnsureGlobalTelemetryInitialized()
	if telemetryClient := telemetry.GetGlobalTelemetryClient(); telemetryClient != nil {
		ctx = telemetry.WithClient(ctx, telemetryClient)
	}

	sess.Title = "Running agent"
	// If the last received event was an error, return it. That way the exit code
	// will be non-zero if the agent failed.
	var lastErr error

	oneLoop := func(text string, rd io.Reader) error {
		userInput := strings.TrimSpace(text)
		if userInput == "" {
			return nil
		}

		sess.AddMessage(PrepareUserMessage(ctx, rt, userInput, cfg.AttachmentPath))

		if cfg.OutputJSON {
			for event := range rt.RunStream(ctx, sess) {
				switch e := event.(type) {
				case *runtime.ToolCallConfirmationEvent:
					if !cfg.AutoApprove {
						rt.Resume(ctx, runtime.ResumeReject(""))
					}
				case *runtime.ErrorEvent:
					return fmt.Errorf("%s", e.Error)
				}

				buf, err := json.Marshal(event)
				if err != nil {
					return err
				}
				out.Println(string(buf))
			}

			return nil
		}

		firstLoop := true
		lastAgent := rt.CurrentAgentName()
		var lastConfirmedToolCallID string
		for event := range rt.RunStream(ctx, sess) {
			agentName := event.GetAgentName()
			if agentName != "" && (firstLoop || lastAgent != agentName) {
				if !firstLoop {
					out.Println()
				}
				out.PrintAgentName(agentName)
				firstLoop = false
				lastAgent = agentName
			}
			switch e := event.(type) {
			case *runtime.AgentChoiceEvent:
				out.Print(e.Content)
			case *runtime.AgentChoiceReasoningEvent:
				out.Print(e.Content)
			case *runtime.ToolCallConfirmationEvent:
				result := out.PrintToolCallWithConfirmation(ctx, e.ToolCall, rd)
				// If interrupted, skip resuming; the runtime will notice context cancellation and stop
				if ctx.Err() != nil {
					continue
				}
				lastConfirmedToolCallID = e.ToolCall.ID // Store the ID to avoid duplicate printing
				switch result {
				case ConfirmationApprove:
					rt.Resume(ctx, runtime.ResumeApprove())
				case ConfirmationApproveSession:
					sess.ToolsApproved = true
					rt.Resume(ctx, runtime.ResumeApproveSession())
				case ConfirmationReject:
					rt.Resume(ctx, runtime.ResumeReject(""))
					lastConfirmedToolCallID = "" // Clear on reject since tool won't execute
				case ConfirmationAbort:
					// Stop the agent loop immediately
					cancel()
					continue
				}
			case *runtime.ToolCallEvent:
				if cfg.HideToolCalls {
					continue
				}
				// Only print if this wasn't already shown during confirmation
				if e.ToolCall.ID != lastConfirmedToolCallID {
					out.PrintToolCall(e.ToolCall)
				}
			case *runtime.ToolCallResponseEvent:
				if cfg.HideToolCalls {
					continue
				}
				out.PrintToolCallResponse(e.ToolCall, e.Response)
				// Clear the confirmed ID after the tool completes
				if e.ToolCall.ID == lastConfirmedToolCallID {
					lastConfirmedToolCallID = ""
				}
			case *runtime.ErrorEvent:
				lowerErr := strings.ToLower(e.Error)
				if strings.Contains(lowerErr, "context cancel") && ctx.Err() != nil { // treat Ctrl+C cancellations as non-errors
					lastErr = nil
				} else {
					lastErr = fmt.Errorf("%s", e.Error)
					out.PrintError(lastErr)
				}
			case *runtime.MaxIterationsReachedEvent:
				result := out.PromptMaxIterationsContinue(ctx, e.MaxIterations)
				switch result {
				case ConfirmationApprove:
					rt.Resume(ctx, runtime.ResumeApprove())
				case ConfirmationReject:
					rt.Resume(ctx, runtime.ResumeReject(""))
					return nil
				case ConfirmationAbort:
					rt.Resume(ctx, runtime.ResumeReject(""))
					return nil
				}
			case *runtime.ElicitationRequestEvent:
				serverURL := e.Meta["cagent/server_url"].(string)
				result := out.PromptOAuthAuthorization(ctx, serverURL)
				switch {
				case ctx.Err() != nil:
					return ctx.Err()
				case result == ConfirmationApprove:
					_ = rt.ResumeElicitation(ctx, "accept", nil)
				case result == ConfirmationReject:
					_ = rt.ResumeElicitation(ctx, "decline", nil)
					return fmt.Errorf("OAuth authorization rejected by user")
				}
			}
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

			if err := oneLoop(string(buf), os.Stdin); err != nil {
				return err
			}
		} else {
			if err := oneLoop(args[1], os.Stdin); err != nil {
				return err
			}
		}
	} else {
		out.PrintWelcomeMessage(cfg.AppName)
		firstQuestion := true
		for {
			if !firstQuestion {
				out.Println()
				out.Println()
			}
			out.Print("> ")
			firstQuestion = false

			line, err := input.ReadLine(ctx, os.Stdin)
			if err != nil {
				return err
			}

			if err := oneLoop(line, os.Stdin); err != nil {
				return err
			}
		}
	}

	// Wrap runtime errors to prevent duplicate error messages and usage display
	if lastErr != nil {
		return RuntimeError{Err: lastErr}
	}
	return nil
}

// PrepareUserMessage resolves commands, parses /attach directives, and creates
// a user message with optional image attachment. This is the common flow for
// both TUI and CLI modes.
//
// Parameters:
//   - ctx: context for command resolution
//   - rt: runtime for command resolution
//   - userInput: the raw user input (may contain /commands and /attach directives)
//   - globalAttachPath: attachment path from --attach flag (can be empty)
//
// Returns the prepared session.Message ready to be added to the session.
func PrepareUserMessage(ctx context.Context, rt runtime.Runtime, userInput, globalAttachPath string) *session.Message {
	// Resolve any /command to its prompt text
	resolvedContent := runtime.ResolveCommand(ctx, rt, userInput)

	// Parse for /attach commands in the message
	messageText, attachPath := ParseAttachCommand(resolvedContent)

	// Use either the per-message attachment or the global one
	finalAttachPath := cmp.Or(attachPath, globalAttachPath)

	return CreateUserMessageWithAttachment(messageText, finalAttachPath)
}

// ParseAttachCommand parses user input for /attach commands
// Returns the message text (with /attach commands removed) and the attachment path
func ParseAttachCommand(userInput string) (messageText, attachPath string) {
	lines := strings.Split(userInput, "\n")
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

// CreateUserMessageWithAttachment creates a user message with optional image attachment
func CreateUserMessageWithAttachment(userContent, attachmentPath string) *session.Message {
	if attachmentPath == "" {
		return session.UserMessage(userContent)
	}

	// Convert file to data URL
	dataURL, err := fileToDataURL(attachmentPath)
	if err != nil {
		slog.Warn("Failed to attach file", "path", attachmentPath, "error", err)
		return session.UserMessage(userContent)
	}

	// Ensure we have some text content when attaching a file
	textContent := cmp.Or(strings.TrimSpace(userContent), "Please analyze this attached file.")

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

	return session.UserMessage("", multiContent...)
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
