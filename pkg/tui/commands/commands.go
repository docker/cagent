package commands

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/docker/cagent/pkg/app"
	"github.com/docker/cagent/pkg/feedback"
	"github.com/docker/cagent/pkg/modelsdev"
	"github.com/docker/cagent/pkg/tui/components/toolcommon"
	"github.com/docker/cagent/pkg/tui/core"
	"github.com/docker/cagent/pkg/tui/messages"
)

// ExecuteFunc is a function that executes a command with an optional argument.
type ExecuteFunc func(arg string) tea.Cmd

// Category represents a category of commands
type Category struct {
	Name     string
	Commands []Item
}

// Item represents a single command in the palette
type Item struct {
	ID           string
	Label        string
	Description  string
	Category     string
	SlashCommand string
	Execute      ExecuteFunc
}

func builtInSessionCommands() []Item {
	cmds := []Item{
		{
			ID:           "session.exit",
			Label:        "Exit",
			SlashCommand: "/exit",
			Description:  "Exit the application",
			Category:     "Session",
			Execute: func(string) tea.Cmd {
				return core.CmdHandler(messages.ExitSessionMsg{})
			},
		},
		{
			ID:           "session.new",
			Label:        "New",
			SlashCommand: "/new",
			Description:  "Start a new conversation",
			Category:     "Session",
			Execute: func(string) tea.Cmd {
				return core.CmdHandler(messages.NewSessionMsg{})
			},
		},
		{
			ID:           "session.history",
			Label:        "Sessions",
			SlashCommand: "/sessions",
			Description:  "Browse and load past sessions",
			Category:     "Session",
			Execute: func(string) tea.Cmd {
				return core.CmdHandler(messages.OpenSessionBrowserMsg{})
			},
		},
		{
			ID:           "session.star",
			Label:        "Star",
			SlashCommand: "/star",
			Description:  "Toggle star on current session",
			Category:     "Session",
			Execute: func(string) tea.Cmd {
				return core.CmdHandler(messages.ToggleSessionStarMsg{})
			},
		},
		{
			ID:           "session.title",
			Label:        "Title",
			SlashCommand: "/title",
			Description:  "Set or regenerate session title (usage: /title [new title])",
			Category:     "Session",
			Execute: func(arg string) tea.Cmd {
				arg = strings.TrimSpace(arg)
				if arg == "" {
					// No argument: regenerate title
					return core.CmdHandler(messages.RegenerateTitleMsg{})
				}
				// With argument: set title
				return core.CmdHandler(messages.SetSessionTitleMsg{Title: arg})
			},
		},
		{
			ID:           "session.model",
			Label:        "Model",
			SlashCommand: "/model",
			Description:  "Change the model for the current agent",
			Category:     "Session",
			Execute: func(string) tea.Cmd {
				return core.CmdHandler(messages.OpenModelPickerMsg{})
			},
		},
		{
			ID:           "session.compact",
			Label:        "Compact",
			SlashCommand: "/compact",
			Description:  "Summarize the current conversation (usage: /compact [additional instructions])",
			Category:     "Session",
			Execute: func(arg string) tea.Cmd {
				return core.CmdHandler(messages.CompactSessionMsg{AdditionalPrompt: arg})
			},
		},
		{
			ID:           "session.clipboard",
			Label:        "Copy",
			SlashCommand: "/copy",
			Description:  "Copy the current conversation to the clipboard",
			Category:     "Session",
			Execute: func(string) tea.Cmd {
				return core.CmdHandler(messages.CopySessionToClipboardMsg{})
			},
		},
		{
			ID:           "session.copy_last_response",
			Label:        "Copy Last Response",
			SlashCommand: "/copy-last",
			Description:  "Copy the last assistant message to the clipboard",
			Category:     "Session",
			Execute: func(string) tea.Cmd {
				return core.CmdHandler(messages.CopyLastResponseToClipboardMsg{})
			},
		},
		{
			ID:           "session.eval",
			Label:        "Eval",
			SlashCommand: "/eval",
			Description:  "Create an evaluation report (usage: /eval [filename])",
			Category:     "Session",
			Execute: func(arg string) tea.Cmd {
				return core.CmdHandler(messages.EvalSessionMsg{Filename: arg})
			},
		},
		{
			ID:           "session.export",
			Label:        "Export",
			SlashCommand: "/export",
			Description:  "Export the session as HTML (usage: /export [filename])",
			Category:     "Session",
			Execute: func(arg string) tea.Cmd {
				return core.CmdHandler(messages.ExportSessionMsg{Filename: arg})
			},
		},
		{
			ID:           "session.yolo",
			Label:        "Yolo",
			SlashCommand: "/yolo",
			Description:  "Toggle automatic approval of tool calls",
			Category:     "Session",
			Execute: func(string) tea.Cmd {
				return core.CmdHandler(messages.ToggleYoloMsg{})
			},
		},
		{
			ID:           "session.think",
			Label:        "Think",
			SlashCommand: "/think",
			Description:  "Toggle thinking/reasoning mode",
			Category:     "Session",
			Execute: func(string) tea.Cmd {
				return core.CmdHandler(messages.ToggleThinkingMsg{})
			},
		},
		{
			ID:           "session.shell",
			Label:        "Shell",
			SlashCommand: "/shell",
			Description:  "Start a shell",
			Category:     "Session",
			Execute: func(string) tea.Cmd {
				return core.CmdHandler(messages.StartShellMsg{})
			},
		},
		{
			ID:           "session.cost",
			Label:        "Cost",
			SlashCommand: "/cost",
			Description:  "Show detailed cost breakdown for this session",
			Category:     "Session",
			Execute: func(string) tea.Cmd {
				return core.CmdHandler(messages.ShowCostDialogMsg{})
			},
		},
		{
			ID:           "session.permissions",
			Label:        "Permissions",
			SlashCommand: "/permissions",
			Description:  "Show tool permission rules for this session",
			Category:     "Session",
			Execute: func(string) tea.Cmd {
				return core.CmdHandler(messages.ShowPermissionsDialogMsg{})
			},
		},
		{
			ID:           "session.attach",
			Label:        "Attach",
			SlashCommand: "/attach",
			Description:  "Attach a file to your message (usage: /attach [path])",
			Category:     "Session",
			Execute: func(arg string) tea.Cmd {
				return core.CmdHandler(messages.AttachFileMsg{FilePath: arg})
			},
		},
		{
			ID:           "settings.theme",
			Label:        "Theme",
			SlashCommand: "/theme",
			Description:  "Change the color theme",
			Category:     "Settings",
			Execute: func(string) tea.Cmd {
				return core.CmdHandler(messages.OpenThemePickerMsg{})
			},
		},
	}

	// Add speak command on supported platforms (macOS only)
	if speak := speakCommand(); speak != nil {
		cmds = append(cmds, *speak)
	}

	return cmds
}

func builtInFeedbackCommands() []Item {
	return []Item{
		{
			ID:          "feedback.bug",
			Label:       "Report Bug",
			Description: "Report a bug or issue",
			Category:    "Feedback",
			Execute: func(string) tea.Cmd {
				return core.CmdHandler(messages.OpenURLMsg{URL: "https://github.com/docker/cagent/issues/new/choose"})
			},
		},
		{
			ID:          "feedback.feedback",
			Label:       "Give Feedback",
			Description: "Provide feedback about cagent",
			Category:    "Feedback",
			Execute: func(string) tea.Cmd {
				return core.CmdHandler(messages.OpenURLMsg{URL: feedback.Link})
			},
		},
	}
}

// BuildCommandCategories builds the list of command categories for the command palette
func BuildCommandCategories(ctx context.Context, application *app.App) []Category {
	// Get session commands and filter based on model capabilities
	sessionCommands := builtInSessionCommands()

	// Check if the current model supports reasoning; hide /think if not
	currentModel := application.CurrentAgentModel()
	if !modelsdev.ModelSupportsReasoning(ctx, currentModel) {
		filtered := make([]Item, 0, len(sessionCommands))
		for _, cmd := range sessionCommands {
			if cmd.ID != "session.think" {
				filtered = append(filtered, cmd)
			}
		}
		sessionCommands = filtered
	}

	// Hide /permissions if no permissions are configured
	if !application.HasPermissions() {
		filtered := make([]Item, 0, len(sessionCommands))
		for _, cmd := range sessionCommands {
			if cmd.ID != "session.permissions" {
				filtered = append(filtered, cmd)
			}
		}
		sessionCommands = filtered
	}

	categories := []Category{
		{
			Name:     "Session",
			Commands: sessionCommands,
		},
		{
			Name:     "Feedback",
			Commands: builtInFeedbackCommands(),
		},
	}

	agentCommands := application.CurrentAgentCommands(ctx)
	if len(agentCommands) > 0 {
		var commands []Item
		for name, cmd := range agentCommands {
			commands = append(commands, Item{
				ID:           "agent.command." + name,
				Label:        name,
				Description:  toolcommon.TruncateText(cmd.DisplayText(), 60),
				Category:     "Agent Commands",
				SlashCommand: "/" + name,
				Execute: func(string) tea.Cmd {
					return core.CmdHandler(messages.AgentCommandMsg{Command: "/" + name})
				},
			})
		}

		categories = append(categories, Category{
			Name:     "Agent Commands",
			Commands: commands,
		})
	}

	mcpPrompts := application.CurrentMCPPrompts(ctx)
	if len(mcpPrompts) > 0 {
		mcpCommands := make([]Item, 0, len(mcpPrompts))
		for promptName, promptInfo := range mcpPrompts {
			// Build description with argument info
			description := promptInfo.Description
			if len(promptInfo.Arguments) > 0 {
				// Count required arguments
				requiredCount := 0
				for _, arg := range promptInfo.Arguments {
					if arg.Required {
						requiredCount++
					}
				}

				if requiredCount > 0 {
					if description != "" {
						description += " "
					}
					if requiredCount == 1 {
						description += "(1 required arg)"
					} else {
						description += fmt.Sprintf("(%d required args)", requiredCount)
					}
				}
			}

			// Truncate long descriptions to fit on one line
			description = toolcommon.TruncateText(description, 55)

			// Create closure variables to capture current iteration values
			currentPromptName := promptName
			currentPromptInfo := promptInfo

			mcpCommands = append(mcpCommands, Item{
				ID:          "mcp.prompt." + promptName,
				Label:       promptName,
				Description: description,
				Category:    "MCP Prompts",
				Execute: func(string) tea.Cmd {
					// If prompt has no required arguments, execute immediately
					hasRequiredArgs := false
					for _, arg := range currentPromptInfo.Arguments {
						if arg.Required {
							hasRequiredArgs = true
							break
						}
					}

					if !hasRequiredArgs {
						// Execute prompt with empty arguments
						return core.CmdHandler(messages.MCPPromptMsg{
							PromptName: currentPromptName,
							Arguments:  make(map[string]string),
						})
					} else {
						// Show parameter input dialog for prompts with required arguments
						return core.CmdHandler(messages.ShowMCPPromptInputMsg{
							PromptName: currentPromptName,
							PromptInfo: currentPromptInfo,
						})
					}
				},
			})
		}

		categories = append(categories, Category{
			Name:     "MCP Prompts",
			Commands: mcpCommands,
		})
	}

	return categories
}

// ParseSlashCommand checks if the input matches a known slash command and returns
// the tea.Cmd to execute it. Returns nil if not a slash command or not recognized.
// This function only handles built-in session commands, not agent commands or MCP prompts.
func ParseSlashCommand(input string) tea.Cmd {
	if input == "" || input[0] != '/' {
		return nil
	}

	// Split into command and argument
	cmd, arg, _ := strings.Cut(input, " ")

	// Search through built-in commands
	for _, item := range builtInSessionCommands() {
		if item.SlashCommand == cmd {
			return item.Execute(arg)
		}
	}

	return nil
}
