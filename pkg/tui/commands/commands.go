package commands

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/docker/cagent/pkg/app"
	"github.com/docker/cagent/pkg/feedback"
	"github.com/docker/cagent/pkg/tui/core"
)

// Session commands
type (
	NewSessionMsg             struct{}
	EvalSessionMsg            struct{}
	CompactSessionMsg         struct{}
	CopySessionToClipboardMsg struct{}
)

// Agent commands
type AgentCommandMsg struct {
	Command string
}

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

type OpenURLMsg struct {
	URL string
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
	if len(agentCommands) == 0 {
		return categories
	}

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

	return categories
}
