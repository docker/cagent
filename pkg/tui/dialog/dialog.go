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

// CloseDialogMsg is sent to close the current dialog
type CloseDialogMsg struct{}

// Dialog defines the interface that all dialogs must implement
type Dialog interface {
	layout.Model
	Position() (int, int) // Returns (row, col) for dialog placement
}

// Manager manages the dialog stack and rendering
type Manager interface {
	tea.Model

	GetLayer() *lipgloss.Layer
	HasDialog() bool
}

// manager implements Manager
type manager struct {
	width, height int
	currentDialog Dialog // Single active dialog
	keyMap        KeyMap // Global dialog key bindings
}

// KeyMap defines global dialog key bindings
type KeyMap struct {
	// Add any global dialog keys here if needed
}

// New creates a new dialog component manager
func New() Manager {
	return &manager{
		currentDialog: nil,
		keyMap:        KeyMap{},
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
		// Propagate resize to current dialog if it exists
		if d.currentDialog != nil {
			u, cmd := d.currentDialog.Update(msg)
			d.currentDialog = u.(Dialog)
			return d, cmd
		}
		return d, nil

	case OpenDialogMsg:
		return d.handleOpen(msg)

	case CloseDialogMsg:
		return d.handleClose()
	}

	// Forward messages to current dialog if it exists
	if d.currentDialog != nil {
		u, cmd := d.currentDialog.Update(msg)
		d.currentDialog = u.(Dialog)
		return d, cmd
	}
	return d, nil
}

// View renders the current dialog (used for debugging, actual rendering uses GetLayers)
func (d *manager) View() string {
	// This is mainly for debugging - actual rendering uses GetLayers
	if d.currentDialog == nil {
		return ""
	}
	return d.currentDialog.View()
}

// handleOpen processes dialog opening requests
func (d *manager) handleOpen(msg OpenDialogMsg) (tea.Model, tea.Cmd) {
	// Set the new dialog as current
	d.currentDialog = msg.Model

	// Initialize dialog
	var cmds []tea.Cmd
	cmd := msg.Model.Init()
	cmds = append(cmds, cmd)

	// Send initial window size
	_, cmd = msg.Model.Update(tea.WindowSizeMsg{
		Width:  d.width,
		Height: d.height,
	})
	cmds = append(cmds, cmd)

	return d, tea.Batch(cmds...)
}

// handleClose processes dialog closing requests
func (d *manager) handleClose() (tea.Model, tea.Cmd) {
	if d.currentDialog == nil {
		return d, nil
	}

	d.currentDialog = nil

	return d, nil
}

// HasDialog returns true if there is an active dialog
func (d *manager) HasDialog() bool {
	return d.currentDialog != nil
}

// GetLayer returns lipgloss layer for rendering the current dialog
func (d *manager) GetLayer() *lipgloss.Layer {
	if d.currentDialog == nil {
		return nil
	}
	dialogView := d.currentDialog.View()
	row, col := d.currentDialog.Position()
	return lipgloss.NewLayer(dialogView).X(col).Y(row)
}
