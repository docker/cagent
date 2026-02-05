package dialog

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/tui/components/scrollbar"
	"github.com/docker/cagent/pkg/tui/core"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/messages"
	"github.com/docker/cagent/pkg/tui/styles"
)

const (
	agentPickerMinWidth   = 60
	agentPickerMaxWidth   = 130
	agentDoubleClickDelay = 400 * time.Millisecond

	agentPickerHeightPercent = 70
	agentPickerMaxHeight     = 200

	// agentPickerListOverhead is the number of rows used by dialog chrome:
	// title(1) + separator(1) + space(1) + help(1) + borders/padding(4) = 8
	agentPickerListOverhead = 8

	// agentPickerListStartOffset is the Y offset from dialog top to where the agent list starts:
	// border(1) + padding(1) + title(1) + separator(1) = 4
	agentPickerListStartOffset = 4

	// agentPickerScrollbarXInset is the inset from dialog right edge for the scrollbar.
	// This matches border(1) + horizontal padding(2) = 3.
	agentPickerScrollbarXInset = 3

	agentPickerScrollbarGap = 1

	// minLengthForEllipsis is the minimum string length required before truncating with ellipsis.
	minLengthForEllipsis = 3

	// pageJumpSize is the number of items to skip when using PgUp/PgDown.
	pageJumpSize = 5

	// minCardWidth is the minimum width for agent cards to prevent overly narrow rendering.
	minCardWidth = 10

	// descPreviewLength is the maximum description length used for dialog width calculation.
	descPreviewLength = 60

	// Layout constants for agent card rendering
	cardPrefixWidth    = 4 // Radio selector + spaces
	cardNameModelGap   = 2 // Space between name and model
	cardShortcutWidth  = 6 // Shortcut display width + padding
	cardIndent         = 4 // Indent for description, model, tools lines
	cardToolsetIndent  = 2 // Additional indent for toolset names
	cardToolNameIndent = 4 // Additional indent for individual tool names

	// screenMargin is the margin to keep from screen edges when sizing dialog.
	screenMargin = 8

	// scrollAdjustment is the offset adjustment when scrolling to keep selection visible.
	scrollAdjustment = 1

	// contentWidthPadding is the padding parameter for content width calculation.
	contentWidthPadding = 2

	// maxKeyboardShortcuts is the maximum number of Ctrl+N shortcuts (Ctrl+1 through Ctrl+9).
	maxKeyboardShortcuts = 9

	// indexDisplayOffset converts 0-based index to 1-based display number.
	indexDisplayOffset = 1

	// minVisibleLines is the minimum number of lines to display in the agent list.
	minVisibleLines = 1

	// minGapBetweenNameAndShortcut is the minimum space between agent name and shortcut.
	minGapBetweenNameAndShortcut = 1

	// maxDescriptionLines is the maximum number of lines to show for wrapped descriptions.
	maxDescriptionLines = 2

	// ellipsisReservedSpace is the number of characters to reserve for ellipsis when truncating.
	ellipsisReservedSpace = 1
)

type agentPickerDialog struct {
	BaseDialog
	agents        []runtime.AgentDetails
	currentAgent  string
	selected      int
	keyMap        agentPickerKeyMap
	lastClickTime time.Time
	lastClickIdx  int

	scrollbar        *scrollbar.Model
	needsScrollToSel bool
	lineToAgent      []int // maps rendered line index -> agent index (-1 for separators)

	expandedTools map[int]bool // tracks which agents have their tools section expanded
	toolsLineY    map[int]int  // maps agent index -> Y offset of "Tools:" line within card (for click detection)
}

type agentPickerKeyMap struct {
	Enter  key.Binding
	Escape key.Binding
	Up     key.Binding
	Down   key.Binding
	PgUp   key.Binding
	PgDown key.Binding
	Home   key.Binding
	End    key.Binding
}

func defaultAgentPickerKeyMap() agentPickerKeyMap {
	return agentPickerKeyMap{
		Enter:  key.NewBinding(key.WithKeys("enter")),
		Escape: key.NewBinding(key.WithKeys("esc")),
		Up:     key.NewBinding(key.WithKeys("up", "k")),
		Down:   key.NewBinding(key.WithKeys("down", "j")),
		PgUp:   key.NewBinding(key.WithKeys("pgup")),
		PgDown: key.NewBinding(key.WithKeys("pgdown")),
		Home:   key.NewBinding(key.WithKeys("home")),
		End:    key.NewBinding(key.WithKeys("end")),
	}
}

// NewAgentPickerDialog creates a dialog for selecting an agent.
func NewAgentPickerDialog(agents []runtime.AgentDetails, currentAgent string) Dialog {
	d := &agentPickerDialog{
		agents:        agents,
		currentAgent:  currentAgent,
		selected:      0,
		keyMap:        defaultAgentPickerKeyMap(),
		lastClickIdx:  -1,
		scrollbar:     scrollbar.New(),
		expandedTools: make(map[int]bool),
		toolsLineY:    make(map[int]int),
	}
	// Pre-select the current agent if found
	for i, agent := range agents {
		if agent.Name == currentAgent {
			d.selected = i
			d.needsScrollToSel = true // Ensure current agent is visible on open
			break
		}
	}
	return d
}

func (d *agentPickerDialog) Init() tea.Cmd {
	return nil
}

func (d *agentPickerDialog) Update(msg tea.Msg) (layout.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		cmd := d.SetSize(msg.Width, msg.Height)
		return d, cmd

	case *runtime.TeamInfoEvent:
		// Update agent list with new data (including updated tool loading status)
		d.updateAgents(msg.AvailableAgents)
		return d, nil

	case tea.KeyPressMsg:
		if cmd := HandleQuit(msg); cmd != nil {
			return d, cmd
		}
		return d.handleKeyPress(msg)

	case tea.MouseClickMsg:
		return d.handleMouseClick(msg)

	case tea.MouseMotionMsg:
		return d.handleMouseMotion(msg)

	case tea.MouseReleaseMsg:
		return d.handleMouseRelease(msg)

	case tea.MouseWheelMsg:
		return d.handleMouseWheel(msg)
	}

	return d, nil
}

// updateAgents updates the agent list while preserving UI state (selection, expanded tools, etc.)
func (d *agentPickerDialog) updateAgents(agents []runtime.AgentDetails) {
	// Preserve the selected agent name to re-select after update
	var selectedAgentName string
	if d.selected >= 0 && d.selected < len(d.agents) {
		selectedAgentName = d.agents[d.selected].Name
	}

	// Update agents
	d.agents = agents

	// Restore selection by name
	d.selected = 0
	for i, agent := range d.agents {
		if agent.Name == selectedAgentName {
			d.selected = i
			break
		}
	}

	// Clear line mapping so it gets rebuilt on next render
	d.lineToAgent = nil
}

func (d *agentPickerDialog) handleKeyPress(msg tea.KeyPressMsg) (layout.Model, tea.Cmd) {
	// Check for Ctrl+number shortcuts
	if msg.Mod == tea.ModCtrl && msg.Code >= '1' && msg.Code <= '9' {
		index := int(msg.Code - '1')
		if index >= 0 && index < len(d.agents) {
			return d.selectAgent(index)
		}
		return d, nil
	}

	switch {
	case key.Matches(msg, d.keyMap.Escape):
		return d, core.CmdHandler(CloseDialogMsg{})

	case key.Matches(msg, d.keyMap.Enter):
		if d.selected >= 0 && d.selected < len(d.agents) {
			return d.selectAgent(d.selected)
		}
		return d, nil

	case key.Matches(msg, d.keyMap.Up):
		if d.selected > 0 {
			d.selected--
			d.needsScrollToSel = true
		}
		return d, nil

	case key.Matches(msg, d.keyMap.Down):
		if d.selected < len(d.agents)-1 {
			d.selected++
			d.needsScrollToSel = true
		}
		return d, nil

	case key.Matches(msg, d.keyMap.PgUp):
		d.selected = max(0, d.selected-pageJumpSize)
		d.needsScrollToSel = true
		return d, nil

	case key.Matches(msg, d.keyMap.PgDown):
		d.selected = min(len(d.agents)-1, d.selected+pageJumpSize)
		d.needsScrollToSel = true
		return d, nil

	case key.Matches(msg, d.keyMap.Home):
		d.selected = 0
		d.needsScrollToSel = true
		return d, nil

	case key.Matches(msg, d.keyMap.End):
		d.selected = len(d.agents) - 1
		d.needsScrollToSel = true
		return d, nil
	}

	return d, nil
}

func (d *agentPickerDialog) selectAgent(index int) (layout.Model, tea.Cmd) {
	if index < 0 || index >= len(d.agents) {
		return d, nil
	}

	agentName := d.agents[index].Name
	if agentName == d.currentAgent {
		// Already selected, just close
		return d, core.CmdHandler(CloseDialogMsg{})
	}

	return d, tea.Sequence(
		core.CmdHandler(CloseDialogMsg{}),
		core.CmdHandler(messages.SwitchAgentMsg{AgentName: agentName}),
	)
}

func (d *agentPickerDialog) handleMouseClick(msg tea.MouseClickMsg) (layout.Model, tea.Cmd) {
	// Check if click is on the scrollbar
	if d.isMouseOnScrollbar(msg.X, msg.Y) {
		sb, cmd := d.scrollbar.Update(msg)
		d.scrollbar = sb
		return d, cmd
	}

	// Only respond to left clicks inside the dialog
	if msg.Button != tea.MouseLeft || !d.isMouseInDialog(msg.X, msg.Y) {
		return d, nil
	}

	agentIdx := d.mouseYToAgentIndex(msg.Y)
	if agentIdx < 0 || agentIdx >= len(d.agents) {
		return d, nil
	}

	// Check if click is on the "Tools:" line to toggle expansion
	if d.isClickOnToolsLine(msg.Y, agentIdx) {
		d.expandedTools[agentIdx] = !d.expandedTools[agentIdx]
		return d, nil
	}

	now := time.Now()
	if agentIdx == d.lastClickIdx && now.Sub(d.lastClickTime) < agentDoubleClickDelay {
		// Double-click - select immediately
		d.lastClickTime = time.Time{}
		d.lastClickIdx = -1
		return d.selectAgent(agentIdx)
	}

	// Single click - highlight
	d.selected = agentIdx
	d.needsScrollToSel = true
	d.lastClickTime = now
	d.lastClickIdx = agentIdx

	return d, nil
}

func (d *agentPickerDialog) handleMouseMotion(msg tea.MouseMotionMsg) (layout.Model, tea.Cmd) {
	if d.scrollbar.IsDragging() {
		sb, cmd := d.scrollbar.Update(msg)
		d.scrollbar = sb
		return d, cmd
	}
	return d, nil
}

func (d *agentPickerDialog) handleMouseRelease(msg tea.MouseReleaseMsg) (layout.Model, tea.Cmd) {
	if d.scrollbar.IsDragging() {
		sb, cmd := d.scrollbar.Update(msg)
		d.scrollbar = sb
		return d, cmd
	}
	return d, nil
}

func (d *agentPickerDialog) handleMouseWheel(msg tea.MouseWheelMsg) (layout.Model, tea.Cmd) {
	// Only scroll if mouse is inside the dialog
	if !d.isMouseInDialog(msg.X, msg.Y) {
		return d, nil
	}

	switch msg.Button.String() {
	case "wheelup":
		d.scrollbar.ScrollUp()
		d.scrollbar.ScrollUp()
	case "wheeldown":
		d.scrollbar.ScrollDown()
		d.scrollbar.ScrollDown()
	}
	return d, nil
}

func (d *agentPickerDialog) isMouseInDialog(x, y int) bool {
	dialogRow, dialogCol := d.Position()
	dialogWidth, maxHeight, _ := d.dialogSize()
	return x >= dialogCol && x < dialogCol+dialogWidth &&
		y >= dialogRow && y < dialogRow+maxHeight
}

func (d *agentPickerDialog) isMouseOnScrollbar(x, y int) bool {
	dialogWidth, maxHeight, contentWidth := d.dialogSize()
	visibleLines := d.visibleLines(maxHeight)
	cardWidth := max(minCardWidth, contentWidth-agentPickerScrollbarGap-scrollbar.Width)

	// Ensure we have up-to-date line mapping for correct total line count
	if d.lineToAgent == nil {
		_, d.lineToAgent = d.buildLines(cardWidth)
	}
	if len(d.lineToAgent) <= visibleLines {
		return false // No scrollbar when content fits
	}

	dialogRow, dialogCol := d.Position()
	scrollbarX := dialogCol + dialogWidth - agentPickerScrollbarXInset - scrollbar.Width
	scrollbarY := dialogRow + agentPickerListStartOffset

	return x >= scrollbarX && x < scrollbarX+scrollbar.Width &&
		y >= scrollbarY && y < scrollbarY+visibleLines
}

// mouseYToAgentIndex converts a mouse Y position to an agent index.
// Returns -1 if the position is outside the list.
func (d *agentPickerDialog) mouseYToAgentIndex(y int) int {
	dialogRow, _ := d.Position()
	_, maxHeight, contentWidth := d.dialogSize()
	visibleLines := d.visibleLines(maxHeight)

	listStartY := dialogRow + agentPickerListStartOffset
	if y < listStartY || y >= listStartY+visibleLines {
		return -1
	}

	// Ensure lineToAgent is initialized (may be nil before first View call)
	if d.lineToAgent == nil {
		cardWidth := max(minCardWidth, contentWidth-agentPickerScrollbarGap-scrollbar.Width)
		_, d.lineToAgent = d.buildLines(cardWidth)
	}

	lineInView := y - listStartY
	actualLine := d.scrollbar.GetScrollOffset() + lineInView
	if actualLine < 0 || actualLine >= len(d.lineToAgent) {
		return -1
	}

	return d.lineToAgent[actualLine]
}

// isClickOnToolsLine checks if the click is on the "Tools:" line for the given agent.
func (d *agentPickerDialog) isClickOnToolsLine(y, agentIdx int) bool {
	dialogRow, _ := d.Position()
	listStartY := dialogRow + agentPickerListStartOffset
	scrollOffset := d.scrollbar.GetScrollOffset()

	// Get the line index within the full content
	lineInView := y - listStartY
	actualLine := scrollOffset + lineInView

	// Check if this agent has a toolsLineY entry and if it matches
	if toolsLine, ok := d.toolsLineY[agentIdx]; ok {
		// toolsLineY stores the absolute line index within the content
		return actualLine == toolsLine
	}
	return false
}

func (d *agentPickerDialog) computeDialogWidthInternal() int {
	// Calculate width needed to fit content without truncation
	maxNameLen := 0
	maxModelLen := 0
	maxDescLen := 0

	for _, agent := range d.agents {
		if len(agent.Name) > maxNameLen {
			maxNameLen = len(agent.Name)
		}
		if len(agent.Model) > maxModelLen {
			maxModelLen = len(agent.Model)
		}
		// Take first descPreviewLength chars of description for width calculation
		desc := agent.Description
		if len(desc) > descPreviewLength {
			desc = desc[:descPreviewLength]
		}
		if len(desc) > maxDescLen {
			maxDescLen = len(desc)
		}
	}

	// Calculate required width: prefix + name + gap + model + shortcut width
	contentWidth := cardPrefixWidth + maxNameLen + cardNameModelGap + maxModelLen + cardShortcutWidth
	// Also consider description width
	descWidth := cardIndent + maxDescLen

	neededWidth := max(contentWidth, descWidth)

	// Add dialog frame size
	frameWidth := styles.DialogStyle.GetHorizontalFrameSize()
	dialogWidth := neededWidth + frameWidth + agentPickerScrollbarGap + scrollbar.Width

	// Apply bounds
	dialogWidth = max(agentPickerMinWidth, dialogWidth)

	// Don't exceed screen or max width
	maxWidth := min(agentPickerMaxWidth, d.Width()-screenMargin)
	dialogWidth = min(dialogWidth, maxWidth)

	return dialogWidth
}

func (d *agentPickerDialog) View() string {
	dialogWidth, maxHeight, contentWidth := d.dialogSize()
	visibleLines := d.visibleLines(maxHeight)

	cardWidth := max(minCardWidth, contentWidth-agentPickerScrollbarGap-scrollbar.Width)

	// Build all rendered lines and mapping
	allLines, lineToAgent := d.buildLines(cardWidth)
	d.lineToAgent = lineToAgent

	totalLines := len(allLines)
	d.scrollbar.SetDimensions(visibleLines, totalLines)

	// Auto-scroll to selection when keyboard navigation occurred
	if d.needsScrollToSel {
		selectedLine := d.findSelectedLine(lineToAgent)
		scrollOffset := d.scrollbar.GetScrollOffset()
		if selectedLine < scrollOffset {
			d.scrollbar.SetScrollOffset(selectedLine)
		} else if selectedLine >= scrollOffset+visibleLines {
			d.scrollbar.SetScrollOffset(selectedLine - visibleLines + scrollAdjustment)
		}
		d.needsScrollToSel = false
	}

	// Slice visible lines based on scroll offset
	scrollOffset := d.scrollbar.GetScrollOffset()
	visibleEnd := min(scrollOffset+visibleLines, totalLines)
	var visibleAgentLines []string
	if totalLines > 0 {
		visibleAgentLines = allLines[scrollOffset:visibleEnd]
	}

	// Pad with empty lines if content is shorter than visible area
	for len(visibleAgentLines) < visibleLines {
		visibleAgentLines = append(visibleAgentLines, "")
	}

	// Handle empty state
	if len(d.agents) == 0 {
		visibleAgentLines = []string{"", styles.DialogContentStyle.
			Italic(true).
			Align(lipgloss.Center).
			Width(cardWidth).
			Render("No agents found")}
		for len(visibleAgentLines) < visibleLines {
			visibleAgentLines = append(visibleAgentLines, "")
		}
	}

	// Build list with fixed width to keep scrollbar position stable
	listLineStyle := lipgloss.NewStyle().Width(cardWidth)
	fixedWidthLines := make([]string, len(visibleAgentLines))
	for i, line := range visibleAgentLines {
		fixedWidthLines[i] = listLineStyle.Render(line)
	}
	listContent := strings.Join(fixedWidthLines, "\n")

	// Set scrollbar position for mouse hit testing
	dialogRow, dialogCol := d.Position()
	scrollbarX := dialogCol + dialogWidth - agentPickerScrollbarXInset - scrollbar.Width
	scrollbarY := dialogRow + agentPickerListStartOffset
	d.scrollbar.SetPosition(scrollbarX, scrollbarY)

	// Get scrollbar view (or placeholder for consistent layout)
	scrollbarView := d.scrollbar.View()
	if scrollbarView == "" {
		scrollbarView = strings.Repeat(" ", scrollbar.Width)
	}

	scrollableContent := lipgloss.JoinHorizontal(
		lipgloss.Top,
		listContent,
		strings.Repeat(" ", agentPickerScrollbarGap),
		scrollbarView,
	)

	content := NewContent(cardWidth+agentPickerScrollbarGap+scrollbar.Width).
		AddTitle("Agents").
		AddSeparator().
		AddContent(scrollableContent).
		AddSpace().
		AddHelpKeys("↑/↓", "navigate", "enter", "switch", "esc", "cancel").
		Build()

	return styles.DialogStyle.Width(dialogWidth).Render(content)
}

func (d *agentPickerDialog) dialogSize() (dialogWidth, maxHeight, contentWidth int) {
	dialogWidth = d.computeDialogWidthInternal()
	maxHeight = min(d.Height()*agentPickerHeightPercent/100, agentPickerMaxHeight)
	contentWidth = d.ContentWidth(dialogWidth, contentWidthPadding)
	return dialogWidth, maxHeight, contentWidth
}

func (d *agentPickerDialog) visibleLines(maxHeight int) int {
	return max(minVisibleLines, maxHeight-agentPickerListOverhead)
}

func (d *agentPickerDialog) buildLines(cardWidth int) (lines []string, lineToAgent []int) {
	d.toolsLineY = make(map[int]int)

	lastIdx := len(d.agents) - 1
	for i, agent := range d.agents {
		cardStartLine := len(lines)
		card, toolsLineOffset := d.renderAgentCard(agent, i, cardWidth)
		cardLines := strings.Split(card, "\n")

		// for click detection
		if toolsLineOffset >= 0 {
			d.toolsLineY[i] = cardStartLine + toolsLineOffset
		}

		for _, l := range cardLines {
			lines = append(lines, l)
			lineToAgent = append(lineToAgent, i)
		}
		// Add separator between cards, or trailing blank for last card
		// Map separator lines so clicking near an agent selects it:
		// - blank line after card → belongs to card above
		// - separator line → belongs to card above
		// - blank line before next card → belongs to card below
		if i < lastIdx {
			// Blank line, subtle separator, blank line
			separator := styles.MutedStyle.Render(strings.Repeat("─", cardWidth))
			lines = append(lines, "", separator, "")
			lineToAgent = append(lineToAgent, i, i, i+1)
		} else {
			// Trailing blank line for last card so clicking below it still selects it
			lines = append(lines, "")
			lineToAgent = append(lineToAgent, i)
		}
	}
	return lines, lineToAgent
}

func (d *agentPickerDialog) findSelectedLine(lineToAgent []int) int {
	if d.selected < 0 || d.selected >= len(d.agents) {
		return 0
	}
	for i, idx := range lineToAgent {
		if idx == d.selected {
			return i
		}
	}
	return 0
}

func (d *agentPickerDialog) renderAgentCard(agent runtime.AgentDetails, index, contentWidth int) (string, int) {
	isSelected := index == d.selected
	indent := strings.Repeat(" ", cardIndent)

	// Build shortcut hint
	shortcut := ""
	if index < maxKeyboardShortcuts {
		shortcut = fmt.Sprintf("^%d", index+indexDisplayOffset)
	}

	// Line 1: Radio selector + Name + Shortcut (right-aligned)
	var selector string
	var nameStyle lipgloss.Style

	if isSelected {
		selector = styles.RadioSelectedStyle.Render("●") + " "
		nameStyle = styles.AgentNameSelectedStyle
	} else {
		selector = styles.RadioUnselectedStyle.Render("○") + " "
		nameStyle = styles.AgentNameUnselectedStyle
	}

	namePart := selector + nameStyle.Render(agent.Name)
	nameWidth := lipgloss.Width(namePart)
	shortcutWidth := lipgloss.Width(shortcut)

	gap := contentWidth - nameWidth - shortcutWidth
	if gap < minGapBetweenNameAndShortcut {
		gap = minGapBetweenNameAndShortcut
	}
	line1 := namePart + strings.Repeat(" ", gap) + styles.MutedStyle.Render(shortcut)

	var lines []string
	lines = append(lines, line1)

	// Desc: line (if description present)
	if agent.Description != "" {
		descLabel := "Desc:  "
		descLabelWidth := len(descLabel)
		maxDescWidth := contentWidth - cardIndent - descLabelWidth
		descLines := wrapText(agent.Description, maxDescWidth, maxDescriptionLines)
		for i, dl := range descLines {
			if i == 0 {
				lines = append(lines, indent+styles.AgentDescLabelStyle.Render(descLabel)+styles.AgentDescValueStyle.Render(dl))
			} else {
				// Indent continuation lines by label width
				lines = append(lines, indent+strings.Repeat(" ", descLabelWidth)+styles.AgentDescValueStyle.Render(dl))
			}
		}
	}

	// Model: line
	var modelValue string
	switch {
	case agent.Provider != "" && agent.Model != "":
		modelValue = agent.Provider + "/" + agent.Model
	case agent.Model != "":
		modelValue = agent.Model
	case agent.Provider != "":
		modelValue = agent.Provider
	}
	if modelValue != "" {
		modelLabel := "Model: "
		lines = append(lines, indent+styles.AgentModelLabelStyle.Render(modelLabel)+styles.AgentModelValueStyle.Render(modelValue))
	}

	// Tools: line with expand indicator or loading indicator
	toolsLineOffset := -1
	if agent.ToolsLoading {
		// Tools are still being loaded - show loading indicator
		toolsLabel := "Tools: "
		lines = append(lines, indent+styles.AgentToolsLabelStyle.Render(toolsLabel)+styles.MutedStyle.Italic(true).Render("loading..."))
	} else if agent.TotalTools > 0 || len(agent.Toolsets) > 0 {
		toolsLabel := "Tools: "
		toolsValue := fmt.Sprintf("%d", agent.TotalTools)

		expandIndicator := ""
		if d.expandedTools[index] {
			expandIndicator = " " + styles.AgentToolsExpanderStyle.Render("[-]")
		} else {
			expandIndicator = " " + styles.AgentToolsExpanderStyle.Render("[+]")
		}

		toolsLineOffset = len(lines)
		lines = append(lines, indent+styles.AgentToolsLabelStyle.Render(toolsLabel)+styles.AgentToolsValueStyle.Render(toolsValue)+expandIndicator)

		// If expanded, show toolsets and their tools directly under Tools:
		if d.expandedTools[index] {
			if len(agent.Toolsets) > 0 {
				for _, ts := range agent.Toolsets {
					// Toolset name with count
					tsHeader := fmt.Sprintf("%s [%d]:", ts.Name, ts.ToolCount)
					lines = append(lines, indent+strings.Repeat(" ", cardToolsetIndent)+styles.AgentToolsetNameStyle.Render(tsHeader))
					// Individual tool names
					for _, toolName := range ts.ToolNames {
						lines = append(lines, indent+strings.Repeat(" ", cardToolNameIndent)+styles.AgentToolNameStyle.Render("- "+toolName))
					}
				}
			} else {
				lines = append(lines, indent+styles.MutedStyle.Render(strings.Repeat(" ", cardToolsetIndent)+"(toolset details loading...)"))
			}
		}
	}

	return strings.Join(lines, "\n"), toolsLineOffset
}

// wrapText wraps text to fit within maxWidth, returning at most maxLines lines.
func wrapText(text string, maxWidth, maxLines int) []string {
	if maxWidth <= 0 || text == "" {
		return nil
	}

	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}

	var lines []string
	var currentLine strings.Builder

	for _, word := range words {
		switch {
		case currentLine.Len() == 0:
			if len(word) > maxWidth {
				// Word too long, truncate it
				lines = append(lines, word[:maxWidth-ellipsisReservedSpace]+"…")
				if len(lines) >= maxLines {
					return lines
				}
				continue
			}
			currentLine.WriteString(word)
		case currentLine.Len()+1+len(word) <= maxWidth:
			// Word fits on current line
			currentLine.WriteString(" ")
			currentLine.WriteString(word)
		default:
			// Word doesn't fit, start new line
			lines = append(lines, currentLine.String())
			if len(lines) >= maxLines {
				// We're at max lines, add ellipsis to last line if there's more content
				last := lines[len(lines)-1]
				if len(last) > minLengthForEllipsis {
					lines[len(lines)-1] = last[:len(last)-ellipsisReservedSpace] + "…"
				}
				return lines
			}
			currentLine.Reset()
			if len(word) > maxWidth {
				lines = append(lines, word[:maxWidth-ellipsisReservedSpace]+"…")
				if len(lines) >= maxLines {
					return lines
				}
			} else {
				currentLine.WriteString(word)
			}
		}
	}

	// Don't forget the last line
	if currentLine.Len() > 0 && len(lines) < maxLines {
		lines = append(lines, currentLine.String())
	}

	return lines
}

func (d *agentPickerDialog) Position() (row, col int) {
	dialogWidth, maxHeight, _ := d.dialogSize()
	return CenterPosition(d.Width(), d.Height(), dialogWidth, maxHeight)
}
