package chat

import (
	tea "charm.land/bubbletea/v2"

	"github.com/docker/cagent/pkg/tui/components/sidebar"
	"github.com/docker/cagent/pkg/tui/styles"
)

// MouseTarget represents what the mouse is interacting with.
type MouseTarget int

const (
	TargetNone MouseTarget = iota
	TargetEditorResizeHandle
	TargetSidebarToggle
	TargetSidebarResizeHandle
	TargetSidebarStar
	TargetSidebarTitle
	TargetSidebarContent
	TargetMessages
	TargetEditorBanner
	TargetEditor
)

// HitTest determines what UI element is at the given coordinates.
// This centralizes all hit-testing logic in one place, making it easier
// to understand the clickable regions and their priorities.
type HitTest struct {
	page *chatPage
}

// NewHitTest creates a hit tester for the given chat page.
func NewHitTest(page *chatPage) *HitTest {
	return &HitTest{page: page}
}

// At determines what target is at the given coordinates.
// It checks regions in priority order (most specific first).
func (h *HitTest) At(x, y int) MouseTarget {
	p := h.page

	// Check editor resize handle first (highest priority for dragging)
	if h.isOnEditorResizeHandle(x, y) {
		return TargetEditorResizeHandle
	}

	// Check sidebar toggle glyph
	if h.isOnSidebarToggleGlyph(x, y) {
		return TargetSidebarToggle
	}

	// Check sidebar resize handle
	if h.isOnSidebarResizeHandle(x, y) {
		return TargetSidebarResizeHandle
	}

	// Check sidebar content areas
	sl := p.computeSidebarLayout()
	adjustedX := x - styles.AppPaddingLeft

	if sl.mode == sidebarVertical && sl.isInSidebar(adjustedX) {
		return h.sidebarClickTarget(x, y)
	}

	// Check if in collapsed sidebar area (top of screen)
	if sl.mode != sidebarVertical && y < sl.sidebarHeight {
		return h.sidebarClickTarget(x, y)
	}

	// Check editor area
	_, editorHeight := p.editor.GetSize()
	editorTop := p.height - editorHeight
	if y >= editorTop {
		// Check banner area
		editorTopPadding := styles.EditorStyle.GetPaddingTop()
		localY := y - editorTop - editorTopPadding
		if localY >= 0 && localY < p.editor.BannerHeight() {
			return TargetEditorBanner
		}
		return TargetEditor
	}

	return TargetMessages
}

// isOnEditorResizeHandle checks if (x, y) is on the draggable editor resize handle.
func (h *HitTest) isOnEditorResizeHandle(x, y int) bool {
	p := h.page
	_, editorHeight := p.editor.GetSize()
	resizeY := p.height - editorHeight - 2

	if y != resizeY {
		return false
	}

	// Only the center portion is draggable
	center := p.width / 2
	return x >= center-resizeHandleWidth/2 && x < center+resizeHandleWidth/2
}

// isOnSidebarToggleGlyph checks if (x, y) is on the sidebar toggle glyph.
func (h *HitTest) isOnSidebarToggleGlyph(x, y int) bool {
	p := h.page
	sl := p.computeSidebarLayout()

	if !sl.showToggle() {
		return false
	}

	if sl.mode == sidebarVertical {
		// Toggle is at y=0 on the handle column
		return y == 0 && h.isOnSidebarResizeHandle(x, y)
	}

	// Collapsed horizontal: toggle is at right edge of first line
	if y != 0 {
		return false
	}
	adjustedX := x - styles.AppPaddingLeft
	return adjustedX == sl.innerWidth-toggleColumnWidth
}

// isOnSidebarResizeHandle checks if (x, y) is on the sidebar resize handle column.
func (h *HitTest) isOnSidebarResizeHandle(x, y int) bool {
	p := h.page
	sl := p.computeSidebarLayout()

	if sl.mode != sidebarVertical {
		return false
	}
	if y < 0 || y >= sl.chatHeight {
		return false
	}
	adjustedX := x - styles.AppPaddingLeft
	return sl.isOnHandle(adjustedX)
}

// IsOnResizeLine checks if y is on the resize handle line.
func (h *HitTest) IsOnResizeLine(y int) bool {
	p := h.page
	_, editorHeight := p.editor.GetSize()
	return y == p.height-editorHeight-2
}

// ExtractCoords extracts x, y coordinates from a mouse message.
func ExtractCoords(msg tea.Msg) (x, y int, ok bool) {
	switch m := msg.(type) {
	case tea.MouseClickMsg:
		return m.X, m.Y, true
	case tea.MouseMotionMsg:
		return m.X, m.Y, true
	case tea.MouseReleaseMsg:
		return m.X, m.Y, true
	case tea.MouseWheelMsg:
		return m.X, m.Y, true
	default:
		return 0, 0, false
	}
}

// sidebarClickTarget determines the specific target within the sidebar area.
func (h *HitTest) sidebarClickTarget(x, y int) MouseTarget {
	clickResult := h.page.handleSidebarClickType(x, y)
	switch clickResult {
	case sidebar.ClickStar:
		return TargetSidebarStar
	case sidebar.ClickTitle:
		return TargetSidebarTitle
	default:
		return TargetSidebarContent
	}
}
