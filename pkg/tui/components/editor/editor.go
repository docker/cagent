package editor

import (
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/docker/cagent/pkg/app"
	"github.com/docker/cagent/pkg/history"
	"github.com/docker/cagent/pkg/tui/components/completion"
	"github.com/docker/cagent/pkg/tui/components/editor/completions"
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
	SetWorking(working bool) tea.Cmd
}

// editor implements [Editor]
type editor struct {
	textarea *textarea.Model
	hist     *history.History
	width    int
	height   int
	working  bool
	// completions are the available completions
	completions []completions.Completion

	// completionWord stores the word being completed
	completionWord    string
	currentCompletion completions.Completion
}

// New creates a new editor component
func New(a *app.App, hist *history.History) Editor {
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
		textarea:    ta,
		hist:        hist,
		completions: completions.Completions(a),
	}
}

// Init initializes the component
func (e *editor) Init() tea.Cmd {
	return textarea.Blink
}

// Update handles messages and updates the component state
func (e *editor) Update(msg tea.Msg) (layout.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		e.textarea.SetWidth(msg.Width - 2)
		return e, nil
	case completion.SelectedMsg:
		currentValue := e.textarea.Value()
		lastIdx := strings.LastIndex(currentValue, e.completionWord)
		if e.currentCompletion.AutoSubmit() {
			if lastIdx >= 0 {
				newValue := currentValue[:lastIdx-1]
				e.textarea.SetValue(newValue)
				e.textarea.MoveToEnd()
			}
			if msg.Execute != nil {
				return e, msg.Execute()
			}
		} else {
			if lastIdx >= 0 {
				newValue := currentValue[:lastIdx-1] + msg.Value + currentValue[lastIdx+len(e.completionWord):]
				e.textarea.SetValue(newValue)
				e.textarea.MoveToEnd()
			}
			return e, nil
		}
		return e, nil
	case completion.ClosedMsg:
		e.completionWord = ""
		return e, nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "enter":
			if !e.textarea.Focused() {
				return e, nil
			}
			value := e.textarea.Value()
			if value != "" && !e.working {
				e.textarea.Reset()
				return e, core.CmdHandler(SendMsg{Content: value})
			}
			return e, nil
		case "ctrl+c":
			return e, tea.Quit
		case "up":
			e.textarea.SetValue(e.hist.Previous())
			e.textarea.MoveToEnd()
			return e, nil
		case "down":
			e.textarea.SetValue(e.hist.Next())
			e.textarea.MoveToEnd()
			return e, nil
		default:
			for _, completion := range e.completions {
				if msg.String() == completion.Trigger() {
					if completion.RequiresEmptyEditor() && e.textarea.Value() != "" {
						continue
					}
					cmds = append(cmds, e.startCompletion(completion))
				}
			}
		}
	}

	var cmd tea.Cmd
	e.textarea, cmd = e.textarea.Update(msg)
	cmds = append(cmds, cmd)

	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		if keyMsg.String() == "space" {
			e.completionWord = ""
			e.currentCompletion = nil
			cmds = append(cmds, core.CmdHandler(completion.CloseMsg{}))
		}

		currentWord := e.textarea.Word()
		if e.currentCompletion != nil && strings.HasPrefix(currentWord, e.currentCompletion.Trigger()) {
			e.completionWord = currentWord[1:]
			cmds = append(cmds, core.CmdHandler(completion.QueryMsg{Query: e.completionWord}))
		} else {
			e.completionWord = ""
			cmds = append(cmds, core.CmdHandler(completion.CloseMsg{}))
		}
	}

	return e, tea.Batch(cmds...)
}

func (e *editor) startCompletion(c completions.Completion) tea.Cmd {
	e.currentCompletion = c
	return core.CmdHandler(completion.OpenMsg{
		Items: c.Items(),
	})
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

func (e *editor) SetWorking(working bool) tea.Cmd {
	e.working = working
	return nil
}
