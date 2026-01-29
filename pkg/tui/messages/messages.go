package messages

import (
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/tools"
)

// Session command messages
type (
	NewSessionMsg                  struct{}
	ExitSessionMsg                 struct{}
	ExitAfterFirstResponseMsg      struct{} // Exit TUI after first assistant response completes
	EvalSessionMsg                 struct{ Filename string }
	CompactSessionMsg              struct{ AdditionalPrompt string }
	CopySessionToClipboardMsg      struct{}
	CopyLastResponseToClipboardMsg struct{}
	ExportSessionMsg               struct{ Filename string }
	ShowCostDialogMsg              struct{}
	ToggleYoloMsg                  struct{}
	ToggleThinkingMsg              struct{}
	ToggleHideToolResultsMsg       struct{}
	ToggleSidebarMsg               struct{} // Toggle sidebar visibility
	StartShellMsg                  struct{}
	SwitchAgentMsg                 struct{ AgentName string }
	OpenSessionBrowserMsg          struct{}
	LoadSessionMsg                 struct{ SessionID string }
	ToggleSessionStarMsg           struct{ SessionID string }         // Toggle star on a session; empty ID means current session
	AttachFileMsg                  struct{ FilePath string }          // Attach a file directly or open file picker if empty/directory
	InsertFileRefMsg               struct{ FilePath string }          // Insert @filepath reference into editor
	OpenModelPickerMsg             struct{}                           // Open the model picker dialog
	ChangeModelMsg                 struct{ ModelRef string }          // Change the model for the current agent
	StartSpeakMsg                  struct{}                           // Start speech-to-text transcription
	StopSpeakMsg                   struct{}                           // Stop speech-to-text transcription
	SpeakTranscriptMsg             struct{ Delta string }             // Transcription delta from speech-to-text
	ClearQueueMsg                  struct{}                           // Clear all queued messages
	AgentCommandMsg                struct{ Command string }           // AgentCommandMsg command message
	OpenURLMsg                     struct{ URL string }               // OpenURLMsg is a url for opening message
	StreamCancelledMsg             struct{ ShowMessage bool }         // StreamCancelledMsg notifies components that the stream has been cancelled
	SendAttachmentMsg              struct{ Content *session.Message } // Message for the first message with an attachment

	MCPPromptMsg struct {
		PromptName string
		Arguments  map[string]string
	}

	ShowMCPPromptInputMsg struct {
		PromptName string
		PromptInfo any // mcptools.PromptInfo but avoiding import cycles
	}

	ElicitationResponseMsg struct {
		Action  tools.ElicitationAction
		Content map[string]any
	}

	SendMsg struct {
		Content     string            // Full content sent to the agent (with file contents expanded)
		Attachments map[string]string // Map of filename to content for attachments
	}

	OpenThemePickerMsg struct{} // Open the theme picker dialog
	ChangeThemeMsg     struct {
		ThemeRef string // Theme reference to apply
	}
	ThemePreviewMsg struct {
		ThemeRef    string // Theme reference to preview
		OriginalRef string // Original theme to restore on cancel
	}
	ThemeCancelPreviewMsg struct {
		OriginalRef string // Theme reference to restore
	}
	ThemeChangedMsg struct{} // Notifies components that the theme has changed (for cache invalidation)
	// ThemeFileChangedMsg notifies TUI that the theme file was modified on disk (hot reload).
	// The TUI should load and apply the theme on the main goroutine to avoid race conditions.
	ThemeFileChangedMsg struct {
		ThemeRef string // The theme ref that was modified
	}
)
