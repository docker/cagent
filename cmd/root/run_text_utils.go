package root

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/docker/cagent/pkg/tools"
	"github.com/fatih/color"
	"golang.org/x/term"
)

// text colors
var (
	blue   = color.New(color.FgBlue).SprintfFunc()
	yellow = color.New(color.FgYellow).SprintfFunc()
	red    = color.New(color.FgRed).SprintfFunc()
	gray   = color.New(color.FgHiBlack).SprintfFunc()
)

// text styles
var bold = color.New(color.Bold).SprintfFunc()

// HideOutputOption represents valid options for hiding tool output
type HideOutputOption string

const (
	HideAll      HideOutputOption = "all"
	HideFileOps  HideOutputOption = "file-ops"
	HideShell    HideOutputOption = "shell"
	HideThink    HideOutputOption = "think"
	HideMemory   HideOutputOption = "memory"
	HideTodo     HideOutputOption = "todo"
	HideTransfer HideOutputOption = "transfer"
	// Individual filesystem tools
	HideCreateDirectory        HideOutputOption = "create_directory"
	HideDirectoryTree          HideOutputOption = "directory_tree"
	HideEditFile               HideOutputOption = "edit_file"
	HideGetFileInfo            HideOutputOption = "get_file_info"
	HideListAllowedDirs        HideOutputOption = "list_allowed_directories"
	HideAddAllowedDir          HideOutputOption = "add_allowed_directory"
	HideListDirectory          HideOutputOption = "list_directory"
	HideListDirectoryWithSizes HideOutputOption = "list_directory_with_sizes"
	HideMoveFile               HideOutputOption = "move_file"
	HideReadFile               HideOutputOption = "read_file"
	HideReadMultipleFiles      HideOutputOption = "read_multiple_files"
	HideSearchFiles            HideOutputOption = "search_files"
	HideSearchFilesContent     HideOutputOption = "search_files_content"
	HideWriteFile              HideOutputOption = "write_file"
	// Individual todo tools
	HideCreateTodo  HideOutputOption = "create_todo"
	HideCreateTodos HideOutputOption = "create_todos"
	HideUpdateTodo  HideOutputOption = "update_todo"
	HideListTodos   HideOutputOption = "list_todos"
	// Individual memory tools
	HideAddMemory    HideOutputOption = "add_memory"
	HideGetMemories  HideOutputOption = "get_memories"
	HideDeleteMemory HideOutputOption = "delete_memory"
	// Transfer task tool
	HideTransferTask HideOutputOption = "transfer_task"
)

// GetAllHideOutputOptions returns all valid hide output options for help text
func GetAllHideOutputOptions() []string {
	return []string{
		string(HideAll),
		string(HideFileOps),
		string(HideShell),
		string(HideThink),
		string(HideMemory),
		string(HideTodo),
		string(HideTransfer),
		string(HideCreateDirectory),
		string(HideDirectoryTree),
		string(HideEditFile),
		string(HideGetFileInfo),
		string(HideListAllowedDirs),
		string(HideAddAllowedDir),
		string(HideListDirectory),
		string(HideListDirectoryWithSizes),
		string(HideMoveFile),
		string(HideReadFile),
		string(HideReadMultipleFiles),
		string(HideSearchFiles),
		string(HideSearchFilesContent),
		string(HideWriteFile),
		string(HideCreateTodo),
		string(HideCreateTodos),
		string(HideUpdateTodo),
		string(HideListTodos),
		string(HideAddMemory),
		string(HideGetMemories),
		string(HideDeleteMemory),
		string(HideTransferTask),
	}
}

// ValidateHideOutputOptions validates that all provided options are valid
func ValidateHideOutputOptions(hideOutputFor string) error {
	if hideOutputFor == "" {
		return nil
	}

	validOptions := make(map[string]bool)
	for _, option := range GetAllHideOutputOptions() {
		validOptions[option] = true
	}

	hideList := strings.Split(hideOutputFor, ",")
	for _, item := range hideList {
		item = strings.TrimSpace(item)
		if item != "" && !validOptions[item] {
			return fmt.Errorf("invalid hide-output-for option: '%s'. Valid options: %s",
				item, strings.Join(GetAllHideOutputOptions(), ", "))
		}
	}
	return nil
}

// shouldHideOutput checks if output should be hidden for a given tool
func shouldHideOutput(toolName, hideOutputFor string) bool {
	if hideOutputFor == "" {
		return false
	}

	hideList := strings.Split(hideOutputFor, ",")
	for _, item := range hideList {
		item = strings.TrimSpace(item)
		option := HideOutputOption(item)

		switch option {
		case HideAll:
			return true
		case HideFileOps:
			if isFileOperation(toolName) {
				return true
			}
		case HideShell:
			if toolName == "shell" {
				return true
			}
		case HideThink:
			if toolName == "think" {
				return true
			}
		case HideMemory:
			if isMemoryOperation(toolName) {
				return true
			}
		case HideTodo:
			if isTodoOperation(toolName) {
				return true
			}
		case HideTransfer:
			if toolName == "transfer_task" {
				return true
			}
		default:
			// Check if it's a specific tool name
			if toolName == string(option) {
				return true
			}
		}
	}
	return false
}

// isFileOperation checks if a tool is a file operation
func isFileOperation(toolName string) bool {
	fileOps := []string{
		"create_directory", "directory_tree", "edit_file", "get_file_info",
		"list_allowed_directories", "add_allowed_directory", "list_directory",
		"list_directory_with_sizes", "move_file", "read_file", "read_multiple_files",
		"search_files", "search_files_content", "write_file",
	}
	for _, op := range fileOps {
		if toolName == op {
			return true
		}
	}
	return false
}

// isMemoryOperation checks if a tool is a memory operation
func isMemoryOperation(toolName string) bool {
	memoryOps := []string{"add_memory", "get_memories", "delete_memory"}
	for _, op := range memoryOps {
		if toolName == op {
			return true
		}
	}
	return false
}

// isTodoOperation checks if a tool is a todo operation
func isTodoOperation(toolName string) bool {
	todoOps := []string{"create_todo", "create_todos", "update_todo", "list_todos"}
	for _, op := range todoOps {
		if toolName == op {
			return true
		}
	}
	return false
}

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
	fmt.Printf("\n%s\n%s\n\n", blue("------- Welcome to %s! -------", bold(APP_NAME)), gray("(Ctrl+C to stop the agent or exit)"))
}

func printError(err error) {
	fmt.Println(red("❌ %s", err))
}

func printAgentName(agentName string) {
	fmt.Printf("\n%s\n", blue("--- Agent: %s ---", bold(agentName)))
}

func printToolCall(toolCall tools.ToolCall, colorFunc ...func(format string, a ...any) string) {
	c := gray
	if len(colorFunc) > 0 && colorFunc[0] != nil {
		c = colorFunc[0]
	}
	fmt.Printf("\n%s\n", c("%s%s", bold(toolCall.Function.Name), formatToolCallArguments(toolCall.Function.Arguments)))
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

func printToolCallResponse(toolCall tools.ToolCall, response string, hideOutputFor ...string) {
	hideOutput := ""
	if len(hideOutputFor) > 0 {
		hideOutput = hideOutputFor[0]
	}

	if shouldHideOutput(toolCall.Function.Name, hideOutput) {
		fmt.Printf("\n%s\n", gray("%s response → (output hidden)", bold(toolCall.Function.Name)))
		return
	}

	fmt.Printf("\n%s\n", gray("%s response%s", bold(toolCall.Function.Name), formatToolCallResponse(response)))
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
