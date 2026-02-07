package tui

import (
	"reflect"
	"testing"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/tui/components/completion"
	"github.com/docker/cagent/pkg/tui/components/editor"
	"github.com/docker/cagent/pkg/tui/components/notification"
	"github.com/docker/cagent/pkg/tui/components/spinner"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/dialog"
	"github.com/docker/cagent/pkg/tui/messages"
	"github.com/docker/cagent/pkg/tui/page/chat"
	"github.com/docker/cagent/pkg/tui/styles"
)

// mockChatPage implements chat.Page for testing
type mockChatPage struct {
	cleanupCalled bool
}

func (m *mockChatPage) Init() tea.Cmd                          { return nil }
func (m *mockChatPage) Update(tea.Msg) (layout.Model, tea.Cmd) { return m, nil }
func (m *mockChatPage) View() string                           { return "" }
func (m *mockChatPage) SetSize(int, int) tea.Cmd               { return nil }
func (m *mockChatPage) CompactSession(string) tea.Cmd          { return nil }
func (m *mockChatPage) Cleanup()                               { m.cleanupCalled = true }
func (m *mockChatPage) SetSessionStarred(bool)                 {}
func (m *mockChatPage) SetTitleRegenerating(bool) tea.Cmd      { return nil }
func (m *mockChatPage) ScrollToBottom() tea.Cmd                { return nil }
func (m *mockChatPage) IsWorking() bool                        { return false }
func (m *mockChatPage) QueueLength() int                       { return 0 }
func (m *mockChatPage) FocusMessages() tea.Cmd                 { return nil }
func (m *mockChatPage) BlurMessages()                          {}
func (m *mockChatPage) GetSidebarSettings() chat.SidebarSettings {
	return chat.SidebarSettings{}
}
func (m *mockChatPage) SetSidebarSettings(chat.SidebarSettings) {}
func (m *mockChatPage) Bindings() []key.Binding                 { return nil }
func (m *mockChatPage) Help() help.KeyMap                       { return nil }

// mockEditor implements editor.Editor for testing
type mockEditor struct {
	cleanupCalled bool
}

func (m *mockEditor) Init() tea.Cmd                          { return nil }
func (m *mockEditor) Update(tea.Msg) (layout.Model, tea.Cmd) { return m, nil }
func (m *mockEditor) View() string                           { return "" }
func (m *mockEditor) SetSize(int, int) tea.Cmd               { return nil }
func (m *mockEditor) Focus() tea.Cmd                         { return nil }
func (m *mockEditor) Blur() tea.Cmd                          { return nil }
func (m *mockEditor) SetWorking(bool) tea.Cmd                { return nil }
func (m *mockEditor) AcceptSuggestion() tea.Cmd              { return nil }
func (m *mockEditor) ScrollByWheel(int)                      {}
func (m *mockEditor) Value() string                          { return "" }
func (m *mockEditor) SetValue(string)                        {}
func (m *mockEditor) InsertText(string)                      {}
func (m *mockEditor) AttachFile(string)                      {}
func (m *mockEditor) Cleanup()                               { m.cleanupCalled = true }
func (m *mockEditor) GetSize() (int, int)                    { return 0, 0 }
func (m *mockEditor) BannerHeight() int                      { return 0 }
func (m *mockEditor) AttachmentAt(int) (editor.AttachmentPreview, bool) {
	return editor.AttachmentPreview{}, false
}
func (m *mockEditor) SetRecording(bool) tea.Cmd { return nil }
func (m *mockEditor) IsRecording() bool         { return false }
func (m *mockEditor) SendContent() tea.Cmd      { return nil }

// collectMsgs executes a command (or batch/sequence of commands) and collects all returned messages.
func collectMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}

	msg := cmd()
	if msg == nil {
		return nil
	}

	// Handle BatchMsg
	if batchMsg, ok := msg.(tea.BatchMsg); ok {
		var msgs []tea.Msg
		for _, innerCmd := range batchMsg {
			if innerCmd != nil {
				msgs = append(msgs, collectMsgs(innerCmd)...)
			}
		}
		return msgs
	}

	// Handle Sequence (unexported type, use reflection)
	msgValue := reflect.ValueOf(msg)
	if msgValue.Kind() == reflect.Slice {
		var msgs []tea.Msg
		for i := range msgValue.Len() {
			elem := msgValue.Index(i)
			if elem.CanInterface() {
				if innerCmd, ok := elem.Interface().(tea.Cmd); ok && innerCmd != nil {
					msgs = append(msgs, collectMsgs(innerCmd)...)
				}
			}
		}
		if len(msgs) > 0 {
			return msgs
		}
	}

	return []tea.Msg{msg}
}

// hasMsg checks if a message of the specified type exists in the collected messages.
func hasMsg[T any](msgs []tea.Msg) bool {
	for _, msg := range msgs {
		if _, ok := msg.(T); ok {
			return true
		}
	}
	return false
}

func TestExitSessionMsg_ExitsImmediately(t *testing.T) {
	t.Parallel()

	mockPage := &mockChatPage{}

	// Create minimal appModel with the mock chat page and editor
	model := &appModel{
		keyMap:         DefaultKeyMap(),
		dialog:         dialog.New(),
		notification:   notification.New(),
		completions:    completion.New(),
		chatPage:       mockPage,
		editor:         &mockEditor{},
		workingSpinner: spinner.New(spinner.ModeSpinnerOnly, styles.SpinnerDotsHighlightStyle),
		focusedPanel:   panelEditor,
	}

	// Send ExitSessionMsg
	_, cmd := model.Update(messages.ExitSessionMsg{})

	// Verify Cleanup was called
	assert.True(t, mockPage.cleanupCalled, "Cleanup() should be called on /exit")

	// Verify the command produces a quit message
	require.NotNil(t, cmd, "cmd should not be nil")
	msgs := collectMsgs(cmd)
	assert.True(t, hasMsg[tea.QuitMsg](msgs), "should produce tea.QuitMsg for immediate exit")
}

func TestExitConfirmedMsg_ExitsImmediately(t *testing.T) {
	t.Parallel()

	mockPage := &mockChatPage{}

	model := &appModel{
		keyMap:         DefaultKeyMap(),
		dialog:         dialog.New(),
		notification:   notification.New(),
		completions:    completion.New(),
		chatPage:       mockPage,
		editor:         &mockEditor{},
		workingSpinner: spinner.New(spinner.ModeSpinnerOnly, styles.SpinnerDotsHighlightStyle),
		focusedPanel:   panelEditor,
	}

	// Send ExitConfirmedMsg (from dialog confirmation)
	_, cmd := model.Update(dialog.ExitConfirmedMsg{})

	// Verify Cleanup was called
	assert.True(t, mockPage.cleanupCalled, "Cleanup() should be called on exit confirmation")

	// Verify the command produces a quit message
	require.NotNil(t, cmd, "cmd should not be nil")
	msgs := collectMsgs(cmd)
	assert.True(t, hasMsg[tea.QuitMsg](msgs), "should produce tea.QuitMsg")
}
