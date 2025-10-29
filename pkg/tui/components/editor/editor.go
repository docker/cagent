package editor

import (
	"github.com/charmbracelet/bubbles/v2/help"
	"github.com/charmbracelet/bubbles/v2/key"
	"github.com/charmbracelet/bubbles/v2/textarea"
	tea "github.com/charmbracelet/bubbletea/v2"

	"github.com/docker/cagent/pkg/history"
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
	textarea          *textarea.Model
	width             int
	height            int
	working           bool
	history           *history.History // Persistent message history
	navigatingHistory bool             // Whether we're navigating history
}

// New creates a new editor component
func New(resolver func(string) string) Editor {
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

	h, err := history.New()
	if err != nil {
		// If history initialization fails, we'll use nil and skip persistence
		// This allows the editor to still work without history
		h = nil
	}

	return &editor{
		textarea: ta,
		history:  h,
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
				// Save to history (automatically persists)
				// Avoid adding consecutive duplicate messages
				if e.history != nil {
					shouldAdd := true
					if len(e.history.Messages) > 0 {
						// Don't add if it's the same as the last message
						if e.history.Messages[len(e.history.Messages)-1] == value {
							shouldAdd = false
						}
					}
					if shouldAdd {
						_ = e.history.Add(value) // Ignore errors, history is optional
					}
				}
				e.navigatingHistory = false
				e.textarea.Reset()
				// Resolve command before sending
				if e.resolver != nil {
					value = e.resolver(value)
				}
				return e, core.CmdHandler(SendMsg{Content: value})
			}
			return e, nil
		case "up":
			if !e.textarea.Focused() {
				return e, nil
			}
			// Navigate history backwards using persistent history
			if e.history != nil && len(e.history.Messages) > 0 {
				if !e.navigatingHistory {
					e.navigatingHistory = true
				}
				// Use history.Previous() which handles navigation internally
				prevMsg := e.history.Previous()
				if prevMsg != "" {
					e.textarea.SetValue(prevMsg)
					// Move cursor to end
					e.textarea.CursorEnd()
				}
			}
			return e, nil
		case "down":
			if !e.textarea.Focused() {
				return e, nil
			}
			// Navigate history forwards - only if already navigating
			if e.navigatingHistory && e.history != nil && len(e.history.Messages) > 0 {
				nextMsg := e.history.Next()
				if nextMsg != "" {
					e.textarea.SetValue(nextMsg)
					// Move cursor to end
					e.textarea.CursorEnd()
				} else {
					// Reached the end, reset to empty
					e.navigatingHistory = false
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
