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
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/dialog"
	"github.com/docker/cagent/pkg/tui/messages"
	"github.com/docker/cagent/pkg/tui/page/chat"
	"github.com/docker/cagent/pkg/tui/service"
)

// mockChatPage implements chat.Page for testing.
type mockChatPage struct {
	cleanupCalled bool
}

func (m *mockChatPage) Init() tea.Cmd                            { return nil }
func (m *mockChatPage) Update(tea.Msg) (layout.Model, tea.Cmd)   { return m, nil }
func (m *mockChatPage) View() string                             { return "" }
func (m *mockChatPage) SetSize(int, int) tea.Cmd                 { return nil }
func (m *mockChatPage) CompactSession(string) tea.Cmd            { return nil }
func (m *mockChatPage) Cleanup()                                 { m.cleanupCalled = true }
func (m *mockChatPage) SetSessionStarred(bool)                   {}
func (m *mockChatPage) SetTitleRegenerating(bool) tea.Cmd        { return nil }
func (m *mockChatPage) ScrollToBottom() tea.Cmd                  { return nil }
func (m *mockChatPage) IsWorking() bool                          { return false }
func (m *mockChatPage) QueueLength() int                         { return 0 }
func (m *mockChatPage) FocusMessages() tea.Cmd                   { return nil }
func (m *mockChatPage) FocusMessageAt(int, int) tea.Cmd          { return nil }
func (m *mockChatPage) BlurMessages()                            {}
func (m *mockChatPage) GetSidebarSettings() chat.SidebarSettings { return chat.SidebarSettings{} }
func (m *mockChatPage) SetSidebarSettings(chat.SidebarSettings)  {}
func (m *mockChatPage) Bindings() []key.Binding                  { return nil }
func (m *mockChatPage) Help() help.KeyMap                        { return nil }

// mockEditor implements editor.Editor for testing.
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
func (m *mockEditor) SetRecording(bool) tea.Cmd                   { return nil }
func (m *mockEditor) IsRecording() bool                           { return false }
func (m *mockEditor) IsHistorySearchActive() bool                 { return false }
func (m *mockEditor) EnterHistorySearch() (layout.Model, tea.Cmd) { return m, nil }
func (m *mockEditor) SendContent() tea.Cmd                        { return nil }

// collectMsgs executes a command (or batch/sequence of commands) and collects all returned messages.
func collectMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}

	msg := cmd()
	if msg == nil {
		return nil
	}

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

func hasMsg[T any](msgs []tea.Msg) bool {
	for _, msg := range msgs {
		if _, ok := msg.(T); ok {
			return true
		}
	}
	return false
}

func newTestModel() (*appModel, *mockChatPage, *mockEditor) {
	page := &mockChatPage{}
	ed := &mockEditor{}

	m := &appModel{
		chatPages:               map[string]chat.Page{"test": page},
		sessionStates:           map[string]*service.SessionState{},
		editors:                 map[string]editor.Editor{"test": ed},
		pendingRestores:         map[string]string{},
		pendingSidebarCollapsed: map[string]bool{},
		chatPage:                page,
		editor:                  ed,
		notification:            notification.New(),
		dialogMgr:               dialog.New(),
		completions:             completion.New(),
	}
	return m, page, ed
}

func TestExitSessionMsg_ExitsImmediately(t *testing.T) {
	t.Parallel()

	m, page, ed := newTestModel()

	_, cmd := m.Update(messages.ExitSessionMsg{})

	assert.True(t, page.cleanupCalled, "Cleanup() should be called on chat page")
	assert.True(t, ed.cleanupCalled, "Cleanup() should be called on editor")
	require.NotNil(t, cmd, "cmd should not be nil")
	msgs := collectMsgs(cmd)
	assert.True(t, hasMsg[tea.QuitMsg](msgs), "should produce tea.QuitMsg for immediate exit")
}

func TestExitConfirmedMsg_ExitsImmediately(t *testing.T) {
	t.Parallel()

	m, page, ed := newTestModel()

	_, cmd := m.Update(dialog.ExitConfirmedMsg{})

	assert.True(t, page.cleanupCalled, "Cleanup() should be called on chat page")
	assert.True(t, ed.cleanupCalled, "Cleanup() should be called on editor")
	require.NotNil(t, cmd, "cmd should not be nil")
	msgs := collectMsgs(cmd)
	assert.True(t, hasMsg[tea.QuitMsg](msgs), "should produce tea.QuitMsg")
}
