package layout

import (
	"github.com/charmbracelet/bubbles/v2/help"
	"github.com/charmbracelet/bubbles/v2/key"
	tea "github.com/charmbracelet/bubbletea/v2"
)

// Sizeable represents components that can be resized
type Sizeable interface {
	SetSize(width, height int) tea.Cmd
}

// Focusable represents components that can receive focus
type Focusable interface {
	Focus() tea.Cmd
	Blur() tea.Cmd
	IsFocused() bool
}

// Help represents components that provide help information
type Help interface {
	Bindings() []key.Binding
	Help() help.KeyMap
}

// Model is the base interface for all TUI models
type Model interface {
	tea.Model
	tea.ViewModel
	Sizeable
}
