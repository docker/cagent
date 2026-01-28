package chat

import (
	"context"
	"errors"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/docker/cagent/pkg/app"
	"github.com/docker/cagent/pkg/tui/components/editor"
	"github.com/docker/cagent/pkg/tui/components/messages"
	"github.com/docker/cagent/pkg/tui/components/notification"
	"github.com/docker/cagent/pkg/tui/components/sidebar"
	"github.com/docker/cagent/pkg/tui/components/tool/editfile"
	"github.com/docker/cagent/pkg/tui/core"
	"github.com/docker/cagent/pkg/tui/core/layout"
	msgtypes "github.com/docker/cagent/pkg/tui/messages"
	"github.com/docker/cagent/pkg/tui/styles"
)

// handleKeyPress handles keyboard input events for the chat page.
// Returns the updated model and command, plus a bool indicating if the event was handled.
func (p *chatPage) handleKeyPress(msg tea.KeyPressMsg) (layout.Model, tea.Cmd, bool) {
	// When editing title, route keypresses to the sidebar
	if p.sidebar.IsEditingTitle() {
		switch msg.Key().Code {
		case tea.KeyEnter:
			newTitle := p.sidebar.CommitTitleEdit()
			cmd := p.persistSessionTitle(newTitle)
			return p, cmd, true
		case tea.KeyEscape:
			p.sidebar.CancelTitleEdit()
			return p, nil, true
		default:
			cmd := p.sidebar.UpdateTitleInput(msg)
			return p, cmd, true
		}
	}

	switch {
	case key.Matches(msg, p.keyMap.Tab):
		if p.focusedPanel == PanelEditor {
			if cmd := p.editor.AcceptSuggestion(); cmd != nil {
				return p, cmd, true
			}
		}
		p.switchFocus()
		return p, nil, true

	case key.Matches(msg, p.keyMap.Cancel):
		cmd := p.cancelStream(true)
		return p, cmd, true

	case key.Matches(msg, p.keyMap.ExternalEditor):
		cmd := p.openExternalEditor()
		return p, cmd, true

	case key.Matches(msg, p.keyMap.ToggleSplitDiff):
		model, cmd := p.messages.Update(editfile.ToggleDiffViewMsg{})
		p.messages = model.(messages.Model)
		return p, cmd, true

	case key.Matches(msg, p.keyMap.ToggleSidebar):
		p.sidebar.ToggleCollapsed()
		cmd := p.SetSize(p.width, p.height)
		return p, cmd, true
	}

	// Route other keys to focused component
	switch p.focusedPanel {
	case PanelChat:
		model, cmd := p.messages.Update(msg)
		p.messages = model.(messages.Model)
		return p, cmd, true
	case PanelEditor:
		model, cmd := p.editor.Update(msg)
		p.editor = model.(editor.Editor)
		return p, cmd, true
	}

	return p, nil, false
}

// persistSessionTitle saves the new session title to the store
func (p *chatPage) persistSessionTitle(newTitle string) tea.Cmd {
	return func() tea.Msg {
		if err := p.app.UpdateSessionTitle(context.Background(), newTitle); err != nil {
			// Show warning if title generation is in progress
			if errors.Is(err, app.ErrTitleGenerating) {
				return notification.ShowMsg{Text: "Title is being generated, please wait", Type: notification.TypeWarning}
			}
			// Log other errors but don't show them
			return nil
		}
		return nil
	}
}

// handleMouseClick handles mouse click events.
func (p *chatPage) handleMouseClick(msg tea.MouseClickMsg) (layout.Model, tea.Cmd) {
	if p.isOnResizeHandle(msg.X, msg.Y) {
		p.isDragging = true
		return p, nil
	}

	// Handle sidebar toggle glyph click
	if msg.Button == tea.MouseLeft && p.isOnSidebarToggleGlyph(msg.X, msg.Y) {
		p.sidebar.ToggleCollapsed()
		cmd := p.SetSize(p.width, p.height)
		return p, cmd
	}

	// Handle sidebar resize handle click (starts potential drag)
	if msg.Button == tea.MouseLeft && p.isOnSidebarHandle(msg.X, msg.Y) {
		p.isDraggingSidebar = true
		p.sidebarDragStartX = msg.X
		p.sidebarDragStartWidth = p.sidebar.GetPreferredWidth()
		p.sidebarDragMoved = false
		return p, nil
	}

	// Check if click is on the star or title in sidebar
	if msg.Button == tea.MouseLeft {
		clickResult := p.handleSidebarClickType(msg.X, msg.Y)
		switch clickResult {
		case sidebar.ClickStar:
			sess := p.app.Session()
			if sess != nil {
				return p, core.CmdHandler(msgtypes.ToggleSessionStarMsg{SessionID: sess.ID})
			}
			return p, nil
		case sidebar.ClickPencil:
			p.sidebar.BeginTitleEdit()
			return p, nil
		}
	}

	cmd := p.routeMouseEvent(msg, msg.Y)
	return p, cmd
}

// handleMouseMotion handles mouse motion events.
func (p *chatPage) handleMouseMotion(msg tea.MouseMotionMsg) (layout.Model, tea.Cmd) {
	if p.isDragging {
		cmd := p.handleResize(msg.Y)
		return p, cmd
	}
	if p.isDraggingSidebar {
		delta := p.sidebarDragStartX - msg.X
		if max(delta, -delta) >= dragThreshold {
			p.sidebarDragMoved = true
		}
		if p.sidebarDragMoved {
			cmd := p.handleSidebarResize(msg.X)
			return p, cmd
		}
		return p, nil
	}
	p.isHoveringHandle = p.isOnResizeLine(msg.Y)
	cmd := p.routeMouseEvent(msg, msg.Y)
	return p, cmd
}

// handleMouseRelease handles mouse release events.
func (p *chatPage) handleMouseRelease(msg tea.MouseReleaseMsg) (layout.Model, tea.Cmd) {
	if p.isDragging {
		p.isDragging = false
		return p, nil
	}
	if p.isDraggingSidebar {
		p.isDraggingSidebar = false
		return p, nil
	}
	cmd := p.routeMouseEvent(msg, msg.Y)
	return p, cmd
}

// handleMouseWheel handles mouse wheel events.
func (p *chatPage) handleMouseWheel(msg tea.MouseWheelMsg) (layout.Model, tea.Cmd) {
	sl := p.computeSidebarLayout()

	if sl.mode == sidebarVertical && !p.sidebar.IsCollapsed() {
		adjustedX := msg.X - styles.AppPaddingLeft
		if sl.isInSidebar(adjustedX) {
			model, cmd := p.sidebar.Update(msg)
			p.sidebar = model.(sidebar.Model)
			return p, cmd
		}
	}

	cmd := p.routeMouseEvent(msg, msg.Y)
	return p, cmd
}

// handleSidebarResize adjusts sidebar width based on drag position.
func (p *chatPage) handleSidebarResize(x int) tea.Cmd {
	innerWidth := p.width - appPaddingHorizontal
	delta := p.sidebarDragStartX - x
	newWidth := p.sidebarDragStartWidth + delta

	// Auto-collapse if dragged below minimum
	if newWidth < sidebar.MinWidth {
		if !p.sidebar.IsCollapsed() {
			// Set preferredWidth to 0 so expanding resets to default
			p.sidebar.SetPreferredWidth(0)
			p.sidebar.SetCollapsed(true)
			return p.SetSize(p.width, p.height)
		}
		return nil
	}

	// Auto-expand if dragged back above minimum
	if p.sidebar.IsCollapsed() {
		p.sidebar.SetCollapsed(false)
	}

	newWidth = p.sidebar.ClampWidth(newWidth, innerWidth)
	if newWidth != p.sidebar.GetPreferredWidth() {
		p.sidebar.SetPreferredWidth(newWidth)
		return p.SetSize(p.width, p.height)
	}
	return nil
}
