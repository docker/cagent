package dialog

import (
	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"

	"github.com/docker/cagent/pkg/tui/core/layout"
)

// OpenDialogMsg is sent to open a new dialog
type OpenDialogMsg struct {
	Model Dialog
}

// CloseDialogMsg is sent to close the current (topmost) dialog
type CloseDialogMsg struct{}

// CloseAllDialogsMsg is sent to close all dialogs in the stack
type CloseAllDialogsMsg struct{}

// Dialog defines the interface that all dialogs must implement
type Dialog interface {
	layout.Model
	Position() (int, int) // Returns (row, col) for dialog placement
}

// Manager manages the dialog stack and rendering
type Manager interface {
	tea.Model

	GetLayers() []*lipgloss.Layer
	HasDialog() bool
}

// manager implements Manager
type manager struct {
	width, height int
	dialogStack   []Dialog
}

// New creates a new dialog component manager
func New() Manager {
	return &manager{
		dialogStack: make([]Dialog, 0),
	}
}

// Init initializes the dialog component
func (d *manager) Init() tea.Cmd {
	return nil
}

// Update handles messages and updates dialog state
func (d *manager) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		d.width = msg.Width
		d.height = msg.Height
		// Propagate resize to all dialogs in the stack
		var cmds []tea.Cmd
		for i := range d.dialogStack {
			u, cmd := d.dialogStack[i].Update(msg)
			d.dialogStack[i] = u.(Dialog)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		return d, tea.Batch(cmds...)

	case OpenDialogMsg:
		return d.handleOpen(msg)

	case CloseDialogMsg:
		return d.handleClose()

	case CloseAllDialogsMsg:
		return d.handleCloseAll()
	}

	// Forward messages to top dialog if it exists
	// Only the topmost dialog receives input to prevent conflicts
	if len(d.dialogStack) > 0 {
		topIndex := len(d.dialogStack) - 1
		u, cmd := d.dialogStack[topIndex].Update(msg)
		d.dialogStack[topIndex] = u.(Dialog)
		return d, cmd
	}
	return d, nil
}

// View renders all dialogs (used for debugging, actual rendering uses GetLayers)
func (d *manager) View() string {
	// This is mainly for debugging - actual rendering uses GetLayers
	if len(d.dialogStack) == 0 {
		return ""
	}
	// Return view of top dialog for debugging
	return d.dialogStack[len(d.dialogStack)-1].View()
}

// handleOpen processes dialog opening requests and adds to stack
func (d *manager) handleOpen(msg OpenDialogMsg) (tea.Model, tea.Cmd) {
	d.dialogStack = append(d.dialogStack, msg.Model)

	var cmds []tea.Cmd
	cmd := msg.Model.Init()
	cmds = append(cmds, cmd)

	_, cmd = msg.Model.Update(tea.WindowSizeMsg{
		Width:  d.width,
		Height: d.height,
	})
	cmds = append(cmds, cmd)

	return d, tea.Batch(cmds...)
}

// handleClose processes dialog closing requests (pops top dialog from stack)
func (d *manager) handleClose() (tea.Model, tea.Cmd) {
	if len(d.dialogStack) != 0 {
		d.dialogStack = d.dialogStack[:len(d.dialogStack)-1]
	}

	return d, nil
}

// handleCloseAll closes all dialogs in the stack
func (d *manager) handleCloseAll() (tea.Model, tea.Cmd) {
	d.dialogStack = make([]Dialog, 0)
	return d, nil
}

// HasDialog returns true if there is at least one active dialog
func (d *manager) HasDialog() bool {
	return len(d.dialogStack) > 0
}

// GetLayers returns lipgloss layers for rendering all dialogs in the stack
// Dialogs are returned in order from bottom to top (index 0 is bottom-most)
func (d *manager) GetLayers() []*lipgloss.Layer {
	if len(d.dialogStack) == 0 {
		return nil
	}

	layers := make([]*lipgloss.Layer, 0, len(d.dialogStack))
	for _, dialog := range d.dialogStack {
		dialogView := dialog.View()
		row, col := dialog.Position()
		layers = append(layers, lipgloss.NewLayer(dialogView).X(col).Y(row))
	}

	return layers
}
