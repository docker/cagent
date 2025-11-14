package commands

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/docker/cagent/pkg/app"
	"github.com/docker/cagent/pkg/feedback"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/tui/core"
)

// Session commands
type (
	NewSessionMsg             struct{}
	EvalSessionMsg            struct{}
	CompactSessionMsg         struct{}
	CopySessionToClipboardMsg struct{}
	LoadSessionMsg            struct {
		SessionID string
	}
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

// formatRelativeTime formats a time as a relative string (e.g., "2 hours ago")
func formatRelativeTime(t time.Time) string {
	duration := time.Since(t)

	switch {
	case duration < time.Minute:
		return "just now"
	case duration < time.Hour:
		minutes := int(duration.Minutes())
		if minutes == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", minutes)
	case duration < 24*time.Hour:
		hours := int(duration.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	case duration < 48*time.Hour:
		return "yesterday"
	case duration < 7*24*time.Hour:
		days := int(duration.Hours() / 24)
		return fmt.Sprintf("%d days ago", days)
	default:
		// For older sessions, just show the date
		return t.Format("Jan 2, 2006")
	}
}

// getSessionPreview generates a preview string from session messages
func getSessionPreview(sess *session.Session) string {
	// Find the first user message
	for _, item := range sess.Messages {
		if item.IsMessage() && item.Message.Message.Content != "" {
			// Get first line or first 60 characters
			content := strings.TrimSpace(item.Message.Message.Content)
			if idx := strings.Index(content, "\n"); idx > 0 {
				content = content[:idx]
			}
			if len(content) > 60 {
				content = content[:57] + "..."
			}
			return content
		}
	}
	return "Empty session"
}

// BuildSessionHistoryCommands creates command items for past sessions
func BuildSessionHistoryCommands(ctx context.Context, store session.Store, agentFilename string) []Item {
	if store == nil {
		return nil
	}

	// Get sessions for this agent
	sessions, err := store.GetSessionsByAgent(ctx, agentFilename)
	if err != nil {
		return nil
	}

	// Limit to 20 most recent sessions
	if len(sessions) > 20 {
		sessions = sessions[:20]
	}

	commands := make([]Item, 0, len(sessions))
	for _, sess := range sessions {
		sessionID := sess.ID
		relativeTime := formatRelativeTime(sess.CreatedAt)
		preview := getSessionPreview(sess)
		messageCount := len(sess.Messages)

		// Use title if available, otherwise use preview
		title := sess.Title
		if title == "" {
			title = preview
		}

		label := fmt.Sprintf("[%s] %s (%d messages)", relativeTime, title, messageCount)

		commands = append(commands, Item{
			ID:          "session.load." + sessionID,
			Label:       label,
			Description: preview,
			Category:    "Session History",
			Execute: func() tea.Cmd {
				// Capture sessionID in closure
				sid := sessionID
				return core.CmdHandler(LoadSessionMsg{SessionID: sid})
			},
		})
	}

	return commands
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

	// Add session history if session store is available
	if sessionStore := application.SessionStore(); sessionStore != nil {
		sessionHistoryCommands := BuildSessionHistoryCommands(ctx, sessionStore, application.AgentFilename())
		if len(sessionHistoryCommands) > 0 {
			categories = append(categories, Category{
				Name:     "Session History",
				Commands: sessionHistoryCommands,
			})
		}
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
