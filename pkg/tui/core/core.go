package core

import (
	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// KeyMapHelp interface for components that provide help
type KeyMapHelp interface {
	Help() help.KeyMap
}

// simpleHelp implements help.KeyMap with simple key bindings
type simpleHelp struct {
	list []key.Binding
}

// NewSimpleHelp creates a new simple help system
func NewSimpleHelp(list []key.Binding) help.KeyMap {
	return &simpleHelp{
		list,
	}
}

// ShortHelp implements help.KeyMap
func (s *simpleHelp) ShortHelp() []key.Binding {
	return s.list
}

// FullHelp implements help.KeyMap
func (s *simpleHelp) FullHelp() [][]key.Binding {
	return nil
}

// CmdHandler creates a command that returns the given message
func CmdHandler(msg tea.Msg) tea.Cmd {
	return func() tea.Msg {
		return msg
	}
}
