// Package tabbar provides a horizontal tab bar for the TUI.
package tabbar

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/docker/cagent/pkg/tui/core"
	"github.com/docker/cagent/pkg/tui/messages"
	"github.com/docker/cagent/pkg/tui/styles"
)

const (
	// tabBarHeight is the number of terminal rows the tab bar occupies.
	tabBarHeight = 1
	// fallbackWidth is used when the terminal width is unknown or zero.
	fallbackWidth = 200
	// scrollArrowWidth is the visual width of a scroll indicator.
	scrollArrowWidth = 2
	// scrollLeftText is the left scroll arrow content.
	scrollLeftText = "◀ "
	// scrollRightText is the right scroll arrow content.
	scrollRightText = " ▶"
	// plusButtonWidth is the visual width of the "+" button.
	plusButtonWidth = 3
	// plusButtonText is the "+" button content.
	plusButtonText = " + "
	// noTab is the sentinel value for click zones that don't map to a tab.
	noTab = -1
)

// clickZone records where a clickable element is on the tab bar.
type clickZone struct {
	startX, endX  int
	tabIdx        int // index into tabs (noTab for non-tab zones)
	isPlus        bool
	isClose       bool
	isScrollLeft  bool
	isScrollRight bool
}

// TabBar renders a horizontal bar of session tabs with click and keyboard support.
type TabBar struct {
	tabs      []messages.TabInfo
	activeIdx int
	width     int
	keyMap    KeyMap

	scrollOffset int
	zones        []clickZone

	// maxTitleLen is the maximum display length for tab titles.
	// Configurable via user settings; defaults to the constant in tab.go.
	maxTitleLen int

	// lastEnsuredIdx tracks which active tab was last scrolled-to by
	// ensureActiveVisible. This prevents View() from overriding manual
	// scroll actions — ensureActiveVisible only runs when the active tab
	// actually changes.
	lastEnsuredIdx int

	// animFrame is the current animation frame from the global coordinator,
	// used to cycle the running indicator on active streaming tabs.
	animFrame int
}

// KeyMap defines key bindings for the tab bar.
type KeyMap struct {
	NewTab   key.Binding
	NextTab  key.Binding
	PrevTab  key.Binding
	CloseTab key.Binding
}

// DefaultKeyMap returns the default tab bar key bindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		NewTab: key.NewBinding(
			key.WithKeys("ctrl+t"),
			key.WithHelp("Ctrl+t", "new tab"),
		),
		NextTab: key.NewBinding(
			key.WithKeys("ctrl+n"),
			key.WithHelp("Ctrl+n", "next tab"),
		),
		PrevTab: key.NewBinding(
			key.WithKeys("ctrl+p"),
			key.WithHelp("Ctrl+p", "prev tab"),
		),
		CloseTab: key.NewBinding(
			key.WithKeys("ctrl+w"),
			key.WithHelp("Ctrl+W", "close tab"),
		),
	}
}

// New creates a new tab bar with the given max title length.
// If maxTitleLen is <= 0, the default (20) is used.
func New(maxTitleLen int) *TabBar {
	if maxTitleLen <= 0 {
		maxTitleLen = defaultMaxTitleLen
	}
	return &TabBar{
		keyMap:         DefaultKeyMap(),
		maxTitleLen:    maxTitleLen,
		lastEnsuredIdx: noTab,
	}
}

// SetWidth sets the available width for the tab bar.
func (t *TabBar) SetWidth(width int) {
	t.width = width
}

// SetTabs updates the list of tabs and active index.
func (t *TabBar) SetTabs(tabs []messages.TabInfo, activeIdx int) {
	if activeIdx != t.activeIdx {
		// Active tab changed — force ensureActiveVisible on next View.
		t.lastEnsuredIdx = noTab
	}
	t.tabs = tabs
	t.activeIdx = activeIdx
	t.clampScroll()
}

// SetAnimFrame updates the animation frame for the running indicator.
func (t *TabBar) SetAnimFrame(frame int) {
	t.animFrame = frame
}

// Height returns the height of the tab bar.
// Returns 0 when there is a single tab (no bar needed).
func (t *TabBar) Height() int {
	if len(t.tabs) <= 1 {
		return 0
	}
	return tabBarHeight
}

// Bindings returns consolidated key bindings for the help bar.
func (t *TabBar) Bindings() []key.Binding {
	return []key.Binding{
		key.NewBinding(
			key.WithKeys("ctrl+t", "ctrl+w"),
			key.WithHelp("Ctrl+t/w", "new/close tab"),
		),
		key.NewBinding(
			key.WithKeys("ctrl+p", "ctrl+n"),
			key.WithHelp("Ctrl+p/n", "prev/next tab"),
		),
	}
}

// Update handles messages and returns commands.
func (t *TabBar) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, t.keyMap.NewTab):
			return core.CmdHandler(messages.SpawnSessionMsg{})

		case key.Matches(msg, t.keyMap.NextTab):
			if len(t.tabs) <= 1 {
				return nil
			}
			nextIdx := (t.activeIdx + 1) % len(t.tabs)
			return core.CmdHandler(messages.SwitchTabMsg{SessionID: t.tabs[nextIdx].SessionID})

		case key.Matches(msg, t.keyMap.PrevTab):
			if len(t.tabs) <= 1 {
				return nil
			}
			prevIdx := t.activeIdx - 1
			if prevIdx < 0 {
				prevIdx = len(t.tabs) - 1
			}
			return core.CmdHandler(messages.SwitchTabMsg{SessionID: t.tabs[prevIdx].SessionID})

		case key.Matches(msg, t.keyMap.CloseTab):
			if len(t.tabs) == 0 {
				return nil
			}
			return core.CmdHandler(messages.CloseTabMsg{SessionID: t.tabs[t.activeIdx].SessionID})
		}

	case tea.MouseClickMsg:
		if msg.Y == 0 {
			if msg.Button == tea.MouseMiddle {
				return t.handleMiddleClick(msg.X)
			}
			return t.handleClick(msg.X)
		}
	}

	return nil
}

// handleMiddleClick closes the tab under the cursor on middle-click.
func (t *TabBar) handleMiddleClick(x int) tea.Cmd {
	for _, z := range t.zones {
		if x < z.startX || x >= z.endX {
			continue
		}
		if z.tabIdx >= 0 && z.tabIdx < len(t.tabs) {
			return core.CmdHandler(messages.CloseTabMsg{SessionID: t.tabs[z.tabIdx].SessionID})
		}
		return nil
	}
	return nil
}

// handleClick uses the click zones computed during the last View() call.
func (t *TabBar) handleClick(x int) tea.Cmd {
	for _, z := range t.zones {
		if x < z.startX || x >= z.endX {
			continue
		}

		switch {
		case z.isScrollLeft:
			t.scrollOffset = max(0, t.scrollOffset-1)
			return nil
		case z.isScrollRight:
			t.scrollOffset = min(len(t.tabs)-1, t.scrollOffset+1)
			return nil
		case z.isPlus:
			return core.CmdHandler(messages.SpawnSessionMsg{})
		case z.isClose && z.tabIdx >= 0 && z.tabIdx < len(t.tabs):
			return core.CmdHandler(messages.CloseTabMsg{SessionID: t.tabs[z.tabIdx].SessionID})
		case z.tabIdx >= 0 && z.tabIdx < len(t.tabs) && z.tabIdx != t.activeIdx:
			return core.CmdHandler(messages.SwitchTabMsg{SessionID: t.tabs[z.tabIdx].SessionID})
		}
		return nil
	}
	return nil
}

// View renders the tab bar as a single line: tab tab tab  +
// Returns empty string when there is a single tab.
func (t *TabBar) View() string {
	if len(t.tabs) <= 1 {
		return ""
	}

	// Reset zones (reuse backing array if available).
	if t.zones != nil {
		t.zones = t.zones[:0]
	}

	availWidth := t.width
	if availWidth <= 0 {
		availWidth = fallbackWidth
	}

	// Pre-render all tabs.
	allTabs := make([]Tab, len(t.tabs))
	totalWidth := 0
	for i, info := range t.tabs {
		allTabs[i] = renderTab(info, t.maxTitleLen, t.animFrame)
		totalWidth += allTabs[i].Width()
	}
	totalWidth += plusButtonWidth

	needsScroll := totalWidth > availWidth

	if !needsScroll {
		t.scrollOffset = 0
	} else if t.activeIdx != t.lastEnsuredIdx {
		// Only auto-scroll when the active tab changes (e.g. tab switch),
		// not on every render — otherwise manual scroll via arrows is undone.
		t.ensureActiveVisible(allTabs, availWidth)
		t.lastEnsuredIdx = t.activeIdx
	}

	// Compute "+" and arrow colors dynamically from the terminal background.
	chromeFg := mutedContrastFg(styles.Background)
	plusStyle := lipgloss.NewStyle().Foreground(chromeFg)
	arrowStyle := lipgloss.NewStyle().Foreground(chromeFg)
	// Attention arrow style: warning-colored and bold so off-screen attention tabs are obvious.
	attnArrowStyle := lipgloss.NewStyle().Foreground(ensureContrast(styles.Warning, styles.Background)).Bold(true)

	var line string
	var cursor int

	// Left scroll arrow — highlight if any off-screen tab to the left needs attention.
	if needsScroll && t.scrollOffset > 0 {
		style := arrowStyle
		if t.hasAttentionInRange(0, t.scrollOffset) {
			style = attnArrowStyle
		}
		arrow := style.Render(scrollLeftText)
		line += arrow
		t.zones = append(t.zones, clickZone{
			startX: cursor, endX: cursor + scrollArrowWidth,
			tabIdx: noTab, isScrollLeft: true,
		})
		cursor += scrollArrowWidth
	}

	// Visible tabs — each tab starts with its own accent bar, no divider needed.
	lastVisibleIdx := t.scrollOffset - 1
	for i := t.scrollOffset; i < len(allTabs); i++ {
		tab := allTabs[i]
		tabW := tab.Width()

		// Reserve space for possible right arrow and "+" button.
		rightReserve := plusButtonWidth
		if needsScroll && i < len(allTabs)-1 {
			rightReserve += scrollArrowWidth
		}

		if cursor+tabW+rightReserve > availWidth && i > t.scrollOffset {
			break
		}

		// Register click zones: main area + close area.
		mainEnd := cursor + tab.MainZoneEnd()
		t.zones = append(t.zones,
			clickZone{startX: cursor, endX: mainEnd, tabIdx: i},
			clickZone{startX: mainEnd, endX: cursor + tabW, tabIdx: i, isClose: true},
		)

		line += tab.View()
		cursor += tabW
		lastVisibleIdx = i
	}

	// Right scroll arrow — highlight if any off-screen tab to the right needs attention.
	if needsScroll && lastVisibleIdx < len(allTabs)-1 {
		style := arrowStyle
		if t.hasAttentionInRange(lastVisibleIdx+1, len(t.tabs)) {
			style = attnArrowStyle
		}
		arrow := style.Render(scrollRightText)
		line += arrow
		t.zones = append(t.zones, clickZone{
			startX: cursor, endX: cursor + scrollArrowWidth,
			tabIdx: noTab, isScrollRight: true,
		})
		cursor += scrollArrowWidth
	}

	// "+" button.
	plus := plusStyle.Render(plusButtonText)
	line += plus
	t.zones = append(t.zones, clickZone{
		startX: cursor, endX: cursor + plusButtonWidth,
		tabIdx: noTab, isPlus: true,
	})

	return line
}

// ensureActiveVisible adjusts scrollOffset so the active tab is visible.
func (t *TabBar) ensureActiveVisible(tabs []Tab, availWidth int) {
	if t.activeIdx < t.scrollOffset {
		t.scrollOffset = t.activeIdx
	}

	for t.scrollOffset < t.activeIdx {
		usedWidth := 0
		if t.scrollOffset > 0 {
			usedWidth += scrollArrowWidth
		}

		fits := false
		for i := t.scrollOffset; i < len(tabs); i++ {
			usedWidth += tabs[i].Width()

			if i == t.activeIdx {
				rightReserve := plusButtonWidth
				if i < len(tabs)-1 {
					rightReserve += scrollArrowWidth
				}
				if usedWidth+rightReserve <= availWidth {
					fits = true
				}
				break
			}
		}

		if fits {
			break
		}
		t.scrollOffset++
	}
}

// hasAttentionInRange returns true if any tab in [start, end) needs attention.
func (t *TabBar) hasAttentionInRange(start, end int) bool {
	for i := start; i < end && i < len(t.tabs); i++ {
		if t.tabs[i].NeedsAttention {
			return true
		}
	}
	return false
}

// clampScroll ensures scrollOffset is within valid bounds.
func (t *TabBar) clampScroll() {
	if t.scrollOffset >= len(t.tabs) {
		t.scrollOffset = max(0, len(t.tabs)-1)
	}
	if t.activeIdx < t.scrollOffset {
		t.scrollOffset = t.activeIdx
	}
}
