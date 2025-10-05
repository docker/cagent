package root

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
	"golang.org/x/term"

	"github.com/docker/cagent/pkg/tools"
)

var (
	// Let's disable the colors in non TUI mode.
	// (dga): I kept those functions in case we find a proper way to use them in both dark and light modes.
	blue   = fmt.Sprintf
	yellow = fmt.Sprintf
	red    = fmt.Sprintf
	white  = fmt.Sprintf
	green  = fmt.Sprintf

	bold = color.New(color.Bold).SprintfFunc()
)

// confirmation result types
type ConfirmationResult string

const (
	ConfirmationApprove        ConfirmationResult = "approve"
	ConfirmationApproveSession ConfirmationResult = "approve_session"
	ConfirmationReject         ConfirmationResult = "reject"
	ConfirmationAbort          ConfirmationResult = "abort"
)

// text utility functions

func printWelcomeMessage() {
	fmt.Printf("\n%s\n%s\n\n", blue("------- Welcome to %s! -------", bold(AppName)), white("(Ctrl+C to stop the agent or exit)"))
}

func printError(err error) {
	fmt.Println(red("❌ %s", err))
}

func printAgentName(agentName string) {
	fmt.Printf("\n%s\n", blue("--- Agent: %s ---", bold(agentName)))
}

func printToolCall(toolCall tools.ToolCall, colorFunc ...func(format string, a ...any) string) {
	c := white
	if len(colorFunc) > 0 && colorFunc[0] != nil {
		c = colorFunc[0]
	}
	fmt.Printf("\nCalling %s\n", c("%s%s", bold(toolCall.Function.Name), formatToolCallArguments(toolCall.Function.Arguments)))
}

func printToolCallWithConfirmation(toolCall tools.ToolCall, scanner *bufio.Scanner) ConfirmationResult {
	fmt.Printf("\n%s\n", bold(yellow("🛠️ Tool call requires confirmation 🛠️")))
	printToolCall(toolCall, color.New(color.FgWhite).SprintfFunc())
	fmt.Printf("\n%s", bold(yellow("Can I run this tool? ([y]es/[a]ll/[n]o): ")))

	// Try single-character input from stdin in raw mode (no Enter required)
	fd := int(os.Stdin.Fd())
	if oldState, err := term.MakeRaw(fd); err == nil {
		defer func() {
			if err := term.Restore(fd, oldState); err != nil {
				fmt.Printf("\n%s\n", yellow("Failed to restore terminal state: %v", err))
			}
		}()
		buf := make([]byte, 1)
		for {
			if _, err := os.Stdin.Read(buf); err != nil {
				break
			}
			switch buf[0] {
			case 'y', 'Y':
				fmt.Print(bold("Yes 👍"))
				return ConfirmationApprove
			case 'a', 'A':
				fmt.Print(bold("Yes to all 👍"))
				return ConfirmationApproveSession
			case 'n', 'N':
				fmt.Print(bold("No 👎"))
				return ConfirmationReject
			case 3: // Ctrl+C
				return ConfirmationAbort
			case '\r', '\n':
				// ignore
			default:
				// ignore other keys
			}
		}
	}

	// Fallback: line-based scanner (requires Enter)
	if !scanner.Scan() {
		return ConfirmationReject
	}
	text := scanner.Text()
	switch text {
	case "y":
		return ConfirmationApprove
	case "a":
		return ConfirmationApproveSession
	case "n":
		return ConfirmationReject
	default:
		// Default to reject for invalid input
		return ConfirmationReject
	}
}

func printToolCallResponse(toolCall tools.ToolCall, response string) {
	fmt.Printf("\n%s\n", white("%s response%s", bold(toolCall.Function.Name), formatToolCallResponse(response)))
}

func promptMaxIterationsContinue(maxIterations int) ConfirmationResult {
	fmt.Printf("\n%s\n", yellow("⚠️  Maximum iterations (%d) reached. The agent may be stuck in a loop.", maxIterations))
	fmt.Printf("%s\n", white("This can happen with smaller or less capable models."))
	fmt.Printf("\n%s (y/n): ", blue("Do you want to continue for 10 more iterations?"))

	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		fmt.Printf("\n%s\n", red("Failed to read input, exiting..."))
		return ConfirmationAbort
	}

	response = strings.TrimSpace(strings.ToLower(response))
	if response == "y" || response == "yes" {
		fmt.Printf("%s\n\n", green("✓ Continuing..."))
		return ConfirmationApprove
	} else {
		fmt.Printf("%s\n\n", white("Exiting..."))
		return ConfirmationReject
	}
}

func promptOAuthAuthorization(serverURL string) ConfirmationResult {
	fmt.Printf("\n%s\n", yellow("🔐 OAuth Authorization Required"))
	fmt.Printf("%s %s (remote)\n", white("Server:"), blue(serverURL))
	fmt.Printf("%s\n", white("This server requires OAuth authentication to access its tools."))
	fmt.Printf("%s\n", white("Your browser will open automatically to complete the authorization."))
	fmt.Printf("\n%s (y/n): ", blue("Do you want to authorize access?"))

	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		fmt.Printf("\n%s\n", red("Failed to read input, aborting authorization..."))
		return ConfirmationAbort
	}

	response = strings.TrimSpace(strings.ToLower(response))
	if response == "y" || response == "yes" {
		fmt.Printf("%s\n", green("✓ Starting OAuth authorization..."))
		fmt.Printf("%s\n", white("Please complete the authorization in your browser."))
		fmt.Printf("%s\n\n", white("Once completed, the agent will continue automatically."))
		return ConfirmationApprove
	} else {
		fmt.Printf("%s\n\n", white("Authorization declined. Exiting..."))
		return ConfirmationReject
	}
}

func formatToolCallArguments(arguments string) string {
	if arguments == "" {
		return "()"
	}

	// Parse JSON to validate it and reformat
	var parsed any
	if err := json.Unmarshal([]byte(arguments), &parsed); err != nil {
		// If JSON parsing fails, return the original string
		return fmt.Sprintf("(%s)", arguments)
	}

	// Custom format that handles multiline strings better
	return formatParsedJSON(parsed)
}

func formatToolCallResponse(response string) string {
	if response == "" {
		return " → ()"
	}

	// For responses, we want to show them as readable text, not JSON
	// Check if it looks like JSON first
	var parsed any
	if err := json.Unmarshal([]byte(response), &parsed); err == nil {
		// It's valid JSON, format it nicely
		return " → " + formatParsedJSON(parsed)
	}

	// It's plain text, handle multiline content
	if strings.Contains(response, "\n") {
		// Trim whitespace and split into lines
		trimmed := strings.TrimSpace(response)
		lines := strings.Split(trimmed, "\n")

		if len(lines) <= 3 {
			// Short multiline, show inline
			return fmt.Sprintf(" → %q", response)
		}

		// Long multiline, format with line breaks
		// Process each line individually and collapse consecutive empty lines
		var formatted []string
		lastWasEmpty := false

		for _, line := range lines {
			trimmedLine := strings.TrimSpace(line)
			if trimmedLine == "" {
				// Empty line - only add one if the last line wasn't empty
				if !lastWasEmpty {
					formatted = append(formatted, "")
					lastWasEmpty = true
				}
			} else {
				formatted = append(formatted, line)
				lastWasEmpty = false
			}
		}
		return fmt.Sprintf(" → (\n%s\n)", strings.Join(formatted, "\n"))
	}

	// Single line text response
	return fmt.Sprintf(" → %q", response)
}

func formatParsedJSON(data any) string {
	switch v := data.(type) {
	case map[string]any:
		if len(v) == 0 {
			return "()"
		}

		parts := make([]string, 0, len(v))
		hasMultilineContent := false

		for key, value := range v {
			formatted := formatJSONValue(key, value)
			parts = append(parts, formatted)
			if strings.Contains(formatted, "\n") {
				hasMultilineContent = true
			}
		}

		if len(parts) == 1 && !hasMultilineContent {
			return fmt.Sprintf("(%s)", parts[0])
		}

		return fmt.Sprintf("(\n  %s\n)", strings.Join(parts, "\n  "))

	default:
		// For non-object types, use standard JSON formatting
		formatted, _ := json.MarshalIndent(data, "", "  ")
		return fmt.Sprintf("(%s)", string(formatted))
	}
}

func formatJSONValue(key string, value any) string {
	switch v := value.(type) {
	case string:
		// Handle multiline strings by displaying with actual newlines
		if strings.Contains(v, "\n") {
			// Format as: key: "content with
			// actual line breaks"
			return fmt.Sprintf("%s: %q", bold(key), v)
		}
		// Regular string with proper escaping
		return fmt.Sprintf("%s: %q", bold(key), v)

	case []any:
		if len(v) == 0 {
			return fmt.Sprintf("%s: []", bold(key))
		}
		// Show full array contents
		jsonBytes, _ := json.MarshalIndent(v, "", "  ")
		return fmt.Sprintf("%s: %s", bold(key), string(jsonBytes))

	case map[string]any:
		jsonBytes, _ := json.MarshalIndent(v, "", "  ")
		return fmt.Sprintf("%s: %s", bold(key), string(jsonBytes))

	default:
		jsonBytes, _ := json.Marshal(v)
		return fmt.Sprintf("%s: %s", bold(key), string(jsonBytes))
	}
}
