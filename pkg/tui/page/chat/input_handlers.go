package chat

import (
	"context"
	"errors"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/docker/cagent/pkg/app"
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
// Returns the updated model and command. All key presses are handled (forwarded to messages if no match).
func (p *chatPage) handleKeyPress(msg tea.KeyPressMsg) (layout.Model, tea.Cmd) {
	// When editing title, route keypresses to the sidebar
	if p.sidebar.IsEditingTitle() {
		switch msg.Key().Code {
		case tea.KeyEnter:
			newTitle := p.sidebar.CommitTitleEdit()
			cmd := p.persistSessionTitle(newTitle)
			return p, cmd
		case tea.KeyEscape:
			p.sidebar.CancelTitleEdit()
			return p, nil
		default:
			cmd := p.sidebar.UpdateTitleInput(msg)
			return p, cmd
		}
	}

	switch {
	case key.Matches(msg, p.keyMap.Cancel):
		// If inline editing is active, cancel the edit first
		if p.messages.IsInlineEditing() {
			cmd := p.messages.CancelInlineEdit()
			return p, cmd
		}
		// Otherwise cancel the stream (only if something is running)
		if p.working || p.msgCancel != nil {
			cmd := p.cancelStream(true)
			return p, cmd
		}
		// Forward to messages for other uses (e.g., clear selection)
		model, cmd := p.messages.Update(msg)
		p.messages = model.(messages.Model)
		return p, cmd

	case key.Matches(msg, p.keyMap.ToggleSplitDiff):
		model, cmd := p.messages.Update(editfile.ToggleDiffViewMsg{})
		p.messages = model.(messages.Model)
		return p, cmd

	case key.Matches(msg, p.keyMap.ToggleSidebar):
		p.sidebar.ToggleCollapsed()
		cmd := p.SetSize(p.width, p.height)
		return p, cmd
	}

	// Route keys to messages (for scrolling, etc.)
	model, cmd := p.messages.Update(msg)
	p.messages = model.(messages.Model)
	return p, cmd
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
	hit := NewHitTest(p)
	target := hit.At(msg.X, msg.Y)

	switch target {
	case TargetSidebarToggle:
		if msg.Button == tea.MouseLeft {
			p.sidebar.ToggleCollapsed()
			cmd := p.SetSize(p.width, p.height)
			return p, cmd
		}

	case TargetSidebarResizeHandle:
		if msg.Button == tea.MouseLeft {
			p.isDraggingSidebar = true
			p.sidebarDragStartX = msg.X
			p.sidebarDragStartWidth = p.sidebar.GetPreferredWidth()
			p.sidebarDragMoved = false
			return p, nil
		}

	case TargetSidebarStar:
		if msg.Button == tea.MouseLeft {
			sess := p.app.Session()
			if sess != nil {
				return p, core.CmdHandler(msgtypes.ToggleSessionStarMsg{SessionID: sess.ID})
			}
			return p, nil
		}

	case TargetSidebarTitle:
		// Double-click on title to edit
		if msg.Button == tea.MouseLeft {
			if p.sidebar.HandleTitleClick() {
				p.sidebar.BeginTitleEdit()
			}
			return p, nil
		}
	}

	// Default: route to appropriate component
	cmd := p.routeMouseEvent(msg, msg.Y)
	return p, cmd
}

// handleMouseMotion handles mouse motion events.
func (p *chatPage) handleMouseMotion(msg tea.MouseMotionMsg) (layout.Model, tea.Cmd) {
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

	cmd := p.routeMouseEvent(msg, msg.Y)
	return p, cmd
}

// handleMouseRelease handles mouse release events.
func (p *chatPage) handleMouseRelease(msg tea.MouseReleaseMsg) (layout.Model, tea.Cmd) {
	if p.isDraggingSidebar {
		p.isDraggingSidebar = false
		return p, nil
	}
	cmd := p.routeMouseEvent(msg, msg.Y)
	return p, cmd
}

// handleMouseWheel handles mouse wheel events.
func (p *chatPage) handleMouseWheel(msg tea.MouseWheelMsg) (layout.Model, tea.Cmd) {
	switch p.wheelTarget(msg.X, msg.Y) {
	case wheelTargetSidebar:
		model, cmd := p.sidebar.Update(msg)
		p.sidebar = model.(sidebar.Model)
		return p, cmd
	default:
		model, cmd := p.messages.Update(msg)
		p.messages = model.(messages.Model)
		return p, cmd
	}
}

func (p *chatPage) handleWheelCoalesced(msg msgtypes.WheelCoalescedMsg) (layout.Model, tea.Cmd) {
	if msg.Delta == 0 {
		return p, nil
	}
	switch p.wheelTarget(msg.X, msg.Y) {
	case wheelTargetSidebar:
		p.sidebar.ScrollByWheel(msg.Delta)
	default:
		p.messages.ScrollByWheel(msg.Delta)
	}
	return p, nil
}

type wheelTarget int

const (
	wheelTargetMessages wheelTarget = iota
	wheelTargetSidebar
)

func (p *chatPage) wheelTarget(x, _ int) wheelTarget {
	sl := p.computeSidebarLayout()
	if sl.mode == sidebarVertical && !p.sidebar.IsCollapsed() {
		adjustedX := x - styles.AppPaddingLeft
		if sl.isInSidebar(adjustedX) {
			return wheelTargetSidebar
		}
	}

	return wheelTargetMessages
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
