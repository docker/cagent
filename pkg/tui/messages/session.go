// Package messages defines all TUI message types organized by domain.
package messages

import "github.com/docker/cagent/pkg/session"

// Session lifecycle messages control session state and persistence.
type (
	// NewSessionMsg requests creation of a new session.
	NewSessionMsg struct{}

	// ExitSessionMsg requests exiting the current session.
	ExitSessionMsg struct{}

	// ExitAfterFirstResponseMsg exits TUI after first assistant response completes.
	ExitAfterFirstResponseMsg struct{}

	// EvalSessionMsg saves evaluation data to the specified file.
	EvalSessionMsg struct{ Filename string }

	// CompactSessionMsg generates a summary and compacts session history.
	CompactSessionMsg struct{ AdditionalPrompt string }

	// CopySessionToClipboardMsg copies the entire conversation to clipboard.
	CopySessionToClipboardMsg struct{}

	// CopyLastResponseToClipboardMsg copies the last assistant response to clipboard.
	CopyLastResponseToClipboardMsg struct{}

	// ExportSessionMsg exports the session to the specified file.
	ExportSessionMsg struct{ Filename string }

	// OpenSessionBrowserMsg opens the session browser dialog.
	OpenSessionBrowserMsg struct{}

	// LoadSessionMsg loads a session by ID.
	LoadSessionMsg struct{ SessionID string }

	// ToggleSessionStarMsg toggles star on a session; empty ID means current session.
	ToggleSessionStarMsg struct{ SessionID string }

	// SetSessionTitleMsg sets the session title to specified value.
	SetSessionTitleMsg struct{ Title string }

	// RegenerateTitleMsg regenerates the session title using the AI.
	RegenerateTitleMsg struct{}

	// StreamCancelledMsg notifies components that the stream has been cancelled.
	StreamCancelledMsg struct{ ShowMessage bool }

	// ClearQueueMsg clears all queued messages.
	ClearQueueMsg struct{}

	// SendMsg contains the content sent to the agent.
	SendMsg struct {
		Content     string            // Full content sent to the agent (with file contents expanded)
		Attachments map[string]string // Map of filename to content for attachments
	}

	// SendAttachmentMsg is a message for the first message with an attachment.
	SendAttachmentMsg struct{ Content *session.Message }
)
