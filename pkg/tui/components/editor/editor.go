package editor

import (
	"github.com/charmbracelet/bubbles/v2/help"
	"github.com/charmbracelet/bubbles/v2/key"
	"github.com/charmbracelet/bubbles/v2/textarea"
	tea "github.com/charmbracelet/bubbletea/v2"

	"github.com/docker/cagent/pkg/tui/core"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/styles"
)

// SendMsg represents a message to send
type SendMsg struct {
	Content string
}

// Editor represents an input editor component
type Editor interface {
	layout.Model
	layout.Sizeable
	layout.Focusable
	layout.Help
	SetWorking(working bool) tea.Cmd
}

// editor implements Editor
type editor struct {
	textarea *textarea.Model
	width    int
	height   int
	working  bool
	history           []string // Stores sent messages
	historyIdx        int      // Current position in history (-1 when not navigating)
	navigatingHistory bool     // Whether we're navigating history
}

// New creates a new editor component
func New() Editor {
	ta := textarea.New()
	ta.SetStyles(styles.InputStyle)
	ta.Placeholder = "Type your message here..."
	ta.Prompt = "│ "
	ta.CharLimit = -1
	ta.SetWidth(50)
	ta.SetHeight(3) // Set minimum 3 lines for multi-line input
	ta.Focus()
	ta.ShowLineNumbers = false
	ta.KeyMap.InsertNewline.SetEnabled(true) // Enable newline insertion

	return &editor{
		textarea: ta,
		history:    make([]string, 0),
		historyIdx: -1,
	}
}

// Init initializes the component
func (e *editor) Init() tea.Cmd {
	return textarea.Blink
}

// Update handles messages and updates the component state
func (e *editor) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		e.textarea.SetWidth(msg.Width - 2)
		return e, nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "enter":
			if !e.textarea.Focused() {
				return e, nil
			}
			value := e.textarea.Value()
			if value != "" && !e.working {
				// Save to history
				e.addToHistory(value)
				e.navigatingHistory = false
				e.historyIdx = -1
				e.textarea.Reset()
				return e, core.CmdHandler(SendMsg{Content: value})
			}
			return e, nil
		case "up":
			if !e.textarea.Focused() {
				return e, nil
			}
			// Navigate history backwards
			if len(e.history) > 0 {
				if e.navigatingHistory {
					if e.historyIdx > 0 {
						e.historyIdx--
					}
				} else {
					// Start navigating from the most recent message
					e.historyIdx = len(e.history) - 1
					e.navigatingHistory = true
				}
				// Load history item into textarea
				e.textarea.SetValue(e.history[e.historyIdx])
				// Move cursor to end
				e.textarea.CursorEnd()
			}
			return e, nil
		case "down":
			if !e.textarea.Focused() {
				return e, nil
			}
			// Navigate history forwards - only if already navigating
			if e.navigatingHistory && len(e.history) > 0 && e.historyIdx >= 0 {
				if e.historyIdx < len(e.history)-1 {
					e.historyIdx++
					// Load history item into textarea
					e.textarea.SetValue(e.history[e.historyIdx])
					// Move cursor to end
					e.textarea.CursorEnd()
				} else {
					// Reached the end, reset to empty
					e.navigatingHistory = false
					e.historyIdx = -1
					e.textarea.Reset()
				}
			}
			return e, nil
		case "ctrl+c":
			return e, tea.Quit
		}
	}

	var cmd tea.Cmd
	e.textarea, cmd = e.textarea.Update(msg)

	// Detect when user starts editing a history item by checking if the value changed
	// This happens after the textarea processes the input event
	if e.navigatingHistory && e.historyIdx >= 0 && e.historyIdx < len(e.history) {
		currentValue := e.textarea.Value()
		if currentValue != e.history[e.historyIdx] {
			// User has edited the history item, exit navigation mode
			e.navigatingHistory = false
			e.historyIdx = -1
		}
	}

	return e, cmd
}

// View renders the component
func (e *editor) View() string {
	return styles.EditorStyle.Render(e.textarea.View())
}

// SetSize sets the dimensions of the component
func (e *editor) SetSize(width, height int) tea.Cmd {
	e.width = width
	e.height = height

	// Account for border and padding
	contentWidth := max(width, 10)
	contentHeight := max(height, 3) // Minimum 3 lines, but respect height parameter

	e.textarea.SetWidth(contentWidth)
	e.textarea.SetHeight(contentHeight)

	return nil
}

// GetSize returns the current dimensions
func (e *editor) GetSize() (width, height int) {
	return e.width, e.height
}

// Focus gives focus to the component
func (e *editor) Focus() tea.Cmd {
	return e.textarea.Focus()
}

// Blur removes focus from the component
func (e *editor) Blur() tea.Cmd {
	e.textarea.Blur()
	return nil
}

// Bindings returns key bindings for the component
func (e *editor) Bindings() []key.Binding {
	return []key.Binding{
		key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "send"),
		),
		key.NewBinding(
			key.WithKeys("up"),
			key.WithHelp("↑", "previous message"),
		),
		key.NewBinding(
			key.WithKeys("down"),
			key.WithHelp("↓", "next message"),
		),
	}
}

// Help returns the help information
func (e *editor) Help() help.KeyMap {
	return core.NewSimpleHelp(e.Bindings())
}

func (e *editor) SetWorking(working bool) tea.Cmd {
	e.working = working
	return nil
}

// addToHistory adds a message to the history, avoiding duplicates from consecutive messages
func (e *editor) addToHistory(value string) {
	// Don't add empty messages
	if value == "" {
		return
	}

	// Don't add if it's the same as the last message
	if len(e.history) > 0 && e.history[len(e.history)-1] == value {
		return
	}

	// Add to history
	e.history = append(e.history, value)
}
