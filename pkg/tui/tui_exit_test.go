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
	"github.com/docker/cagent/pkg/tui/components/notification"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/dialog"
	"github.com/docker/cagent/pkg/tui/messages"
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
func (m *mockChatPage) GetInputHeight() int                    { return 0 }
func (m *mockChatPage) SetSessionStarred(bool)                 {}
func (m *mockChatPage) SetTitleRegenerating(bool) tea.Cmd      { return nil }
func (m *mockChatPage) InsertText(string)                      {}
func (m *mockChatPage) SetRecording(bool) tea.Cmd              { return nil }
func (m *mockChatPage) SendEditorContent() tea.Cmd             { return nil }
func (m *mockChatPage) Bindings() []key.Binding                { return nil }
func (m *mockChatPage) Help() help.KeyMap                      { return nil }

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

	// Create minimal appModel with the mock chat page
	model := &appModel{
		keyMap:       DefaultKeyMap(),
		dialog:       dialog.New(),
		notification: notification.New(),
		completions:  completion.New(),
		chatPage:     mockPage,
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
		keyMap:       DefaultKeyMap(),
		dialog:       dialog.New(),
		notification: notification.New(),
		completions:  completion.New(),
		chatPage:     mockPage,
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
