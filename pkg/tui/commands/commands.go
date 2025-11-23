package commands

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/docker/cagent/pkg/app"
	"github.com/docker/cagent/pkg/feedback"
	"github.com/docker/cagent/pkg/tui/core"
	"github.com/docker/cagent/pkg/tui/messages"
)

type (
	NewSessionMsg             = messages.NewSessionMsg
	EvalSessionMsg            = messages.EvalSessionMsg
	CompactSessionMsg         = messages.CompactSessionMsg
	CopySessionToClipboardMsg = messages.CopySessionToClipboardMsg
	ToggleYoloMsg             = messages.ToggleYoloMsg
	AgentCommandMsg           = messages.AgentCommandMsg
	MCPPromptMsg              = messages.MCPPromptMsg
	OpenURLMsg                = messages.OpenURLMsg
)

// CommandCategory represents a category of commands
type Category struct {
	Name     string
	Commands []Item
}

// Command represents a single command in the palette
type Item struct {
	ID           string
	Label        string
	Description  string
	Category     string
	SlashCommand string
	Execute      func() tea.Cmd
}

func BuiltInSessionCommands() []Item {
	return []Item{
		{
			ID:           "session.new",
			Label:        "New",
			SlashCommand: "/new",
			Description:  "Start a new conversation",
			Category:     "Session",
			Execute: func() tea.Cmd {
				return core.CmdHandler(NewSessionMsg{})
			},
		},
		{
			ID:           "session.compact",
			Label:        "Compact",
			SlashCommand: "/compact",
			Description:  "Summarize the current conversation",
			Category:     "Session",
			Execute: func() tea.Cmd {
				return core.CmdHandler(CompactSessionMsg{})
			},
		},
		{
			ID:           "session.clipboard",
			Label:        "Copy",
			SlashCommand: "/copy",
			Description:  "Copy the current conversation to the clipboard",
			Category:     "Session",
			Execute: func() tea.Cmd {
				return core.CmdHandler(CopySessionToClipboardMsg{})
			},
		},
		{
			ID:           "session.eval",
			Label:        "Eval",
			SlashCommand: "/eval",
			Description:  "Create an evaluation report for the current conversation",
			Category:     "Session",
			Execute: func() tea.Cmd {
				return core.CmdHandler(EvalSessionMsg{})
			},
		},
		{
			ID:           "session.yolo",
			Label:        "Yolo",
			SlashCommand: "/yolo",
			Description:  "Toggle automatic approval of tool calls",
			Category:     "Session",
			Execute: func() tea.Cmd {
				return core.CmdHandler(ToggleYoloMsg{})
			},
		},
	}
}

func builtInFeedbackCommands() []Item {
	return []Item{
		{
			ID:          "feedback.bug",
			Label:       "Report Bug",
			Description: "Report a bug or issue",
			Category:    "Feedback",
			Execute: func() tea.Cmd {
				return core.CmdHandler(OpenURLMsg{URL: "https://github.com/docker/cagent/issues/new/choose"})
			},
		},
		{
			ID:          "feedback.feedback",
			Label:       "Give Feedback",
			Description: "Provide feedback about cagent",
			Category:    "Feedback",
			Execute: func() tea.Cmd {
				return core.CmdHandler(OpenURLMsg{URL: feedback.FeedbackLink})
			},
		},
	}
}

// BuildCommandCategories builds the list of command categories for the command palette
func BuildCommandCategories(ctx context.Context, application *app.App) []Category {
	categories := []Category{
		{
			Name:     "Session",
			Commands: BuiltInSessionCommands(),
		},
		{
			Name:     "Feedback",
			Commands: builtInFeedbackCommands(),
		},
	}

	agentCommands := application.CurrentAgentCommands(ctx)
	if len(agentCommands) > 0 {
		commands := make([]Item, 0, len(agentCommands))
		for name, prompt := range agentCommands {

			// Truncate long descriptions to fit on one line
			description := prompt
			if len(description) > 60 {
				description = description[:57] + "..."
			}

			commands = append(commands, Item{
				ID:          "agent.command." + name,
				Label:       name,
				Description: description,
				Category:    "Agent Commands",
				Execute: func() tea.Cmd {
					return core.CmdHandler(AgentCommandMsg{Command: "/" + name})
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
			if len(description) > 55 {
				description = description[:52] + "..."
			}

			// Create closure variables to capture current iteration values
			currentPromptName := promptName
			currentPromptInfo := promptInfo

			mcpCommands = append(mcpCommands, Item{
				ID:          "mcp.prompt." + promptName,
				Label:       promptName,
				Description: description,
				Category:    "MCP Prompts",
				Execute: func() tea.Cmd {
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
						return core.CmdHandler(MCPPromptMsg{
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
