package dialog

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/docker/cagent/pkg/tui/components/scrollbar"
	"github.com/docker/cagent/pkg/tui/core"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/messages"
	"github.com/docker/cagent/pkg/tui/service/tuistate"
	"github.com/docker/cagent/pkg/tui/styles"
)

// dirEntryKind distinguishes the different types of entries in the picker.
type dirEntryKind int

const (
	entryUseThisDir dirEntryKind = iota
	entryParentDir
	entryFavoriteDir
	entryRecentDir
	entryDir
	// entrySeparator is a non-selectable visual separator between sections.
	entrySeparator
	// entryBlankLine is a non-selectable empty line for visual spacing.
	entryBlankLine
)

type dirEntry struct {
	name string
	path string
	kind dirEntryKind
}

// isNonSelectable returns true for entry kinds that cannot be selected (separators, blank lines).
func (e dirEntry) isNonSelectable() bool {
	return e.kind == entrySeparator || e.kind == entryBlankLine
}

// Working directory picker dialog dimension constants
const (
	// dirPickerWidthPercent is the percentage of screen width to use for the dialog
	dirPickerWidthPercent = 80
	// dirPickerMinWidth is the minimum width of the dialog
	dirPickerMinWidth = 50
	// dirPickerMaxWidth is the maximum width of the dialog
	dirPickerMaxWidth = 100
	// dirPickerHeightPercent is the percentage of screen height to use for the dialog
	dirPickerHeightPercent = 70
	// dirPickerMaxHeight is the maximum height of the dialog
	dirPickerMaxHeight = 150

	// dirPickerDialogPadding is the horizontal padding inside the dialog border (2 on each side + border)
	dirPickerDialogPadding = 6

	// dirPickerListVerticalOverhead is the number of rows used by dialog chrome:
	// title(1) + space(1) + input(1) + separator(1) + space at bottom(1) + help keys(1) + borders/padding(2) = 8
	dirPickerListVerticalOverhead = 8

	// dirPickerListStartOffset is the Y offset from dialog top to where the directory list starts:
	// border(1) + padding(1) + title(1) + space(1) + input(1) + separator(1) = 6
	dirPickerListStartOffset = 6

	// dirPickerScrollbarYOffset is the Y offset from dialog top to where the scrollbar starts.
	// Must match dirPickerListStartOffset since they are joined horizontally.
	dirPickerScrollbarYOffset = dirPickerListStartOffset

	// dirPickerScrollbarXInset is the inset from dialog right edge for the scrollbar
	dirPickerScrollbarXInset = 3

	// dirPickerScrollbarGap is the space between content and the scrollbar
	dirPickerScrollbarGap = 1

	// dirPickerMaxRecentDirs is the maximum number of recent directories to show
	dirPickerMaxRecentDirs = 5

	// pinnedSeparatorLabel is the text for the pinned directories section separator
	pinnedSeparatorLabel = "── Pinned "
	// recentSeparatorLabel is the text for the recent directories section separator
	recentSeparatorLabel = "── Recent "
	// browseSeparatorLabel is the text for the directory browser section separator
	browseSeparatorLabel = "── Browse "
)

type workingDirPickerDialog struct {
	BaseDialog
	textInput        textinput.Model
	currentDir       string
	entries          []dirEntry
	filtered         []dirEntry
	selected         int
	keyMap           commandPaletteKeyMap
	recentDirs       []string
	favoriteDirs     []string
	favoriteSet      map[string]bool // fast lookup for favorite status
	tuiStore         *tuistate.Store
	err              error
	scrollbar        *scrollbar.Model
	needsScrollToSel bool // true when keyboard nav requires scrolling to selection

	// Double-click detection
	lastClickTime  time.Time
	lastClickIndex int
}

// NewWorkingDirPickerDialog creates a new working directory picker dialog.
// recentDirs provides a list of recently used directories to show.
// favoriteDirs provides a list of pinned directories to show.
// store is used for persisting favorite directory changes (may be nil).
func NewWorkingDirPickerDialog(recentDirs, favoriteDirs []string, store *tuistate.Store) Dialog {
	ti := textinput.New()
	ti.Placeholder = "Type to filter directories…"
	ti.Focus()
	ti.CharLimit = 256
	ti.SetWidth(50)

	cwd, err := os.Getwd()
	if err != nil {
		cwd = "/"
	}

	// Build favorite set for O(1) lookup
	favSet := make(map[string]bool, len(favoriteDirs))
	for _, d := range favoriteDirs {
		favSet[d] = true
	}

	// Remove favorites, current dir, and empty paths from recent dirs to avoid duplicates and blanks
	var filteredRecent []string
	for _, d := range recentDirs {
		if d != "" && !favSet[d] && d != cwd {
			filteredRecent = append(filteredRecent, d)
		}
	}

	d := &workingDirPickerDialog{
		textInput:    ti,
		currentDir:   cwd,
		recentDirs:   filteredRecent,
		favoriteDirs: favoriteDirs,
		favoriteSet:  favSet,
		tuiStore:     store,
		keyMap:       defaultCommandPaletteKeyMap(),
		scrollbar:    scrollbar.New(),
	}

	d.loadDirectory()

	return d
}

func (d *workingDirPickerDialog) loadDirectory() {
	d.entries = nil
	d.filtered = nil
	d.selected = 0
	d.err = nil
	d.scrollbar.SetScrollOffset(0)

	// Add recent directories section first (on top, skip empty paths and current dir)
	var recentEntries []dirEntry
	for i, dir := range d.recentDirs {
		if i >= dirPickerMaxRecentDirs {
			break
		}
		if dir == "" || dir == d.currentDir {
			continue
		}
		recentEntries = append(recentEntries, dirEntry{
			name: dir,
			path: dir,
			kind: entryRecentDir,
		})
	}
	if len(recentEntries) > 0 {
		d.entries = append(d.entries, dirEntry{kind: entrySeparator, name: recentSeparatorLabel})
		d.entries = append(d.entries, recentEntries...)
	}

	// Add pinned directories section (skip empty paths)
	var pinnedEntries []dirEntry
	for _, dir := range d.favoriteDirs {
		if dir == "" {
			continue
		}
		pinnedEntries = append(pinnedEntries, dirEntry{
			name: dir,
			path: dir,
			kind: entryFavoriteDir,
		})
	}
	if len(pinnedEntries) > 0 {
		d.entries = append(d.entries, dirEntry{kind: entrySeparator, name: pinnedSeparatorLabel})
		d.entries = append(d.entries, pinnedEntries...)
	}

	// Add browse separator
	d.entries = append(d.entries, dirEntry{kind: entrySeparator, name: browseSeparatorLabel})

	// Add current directory above ".." (default selection)
	useThisDirIndex := len(d.entries)
	d.entries = append(d.entries, dirEntry{
		name: d.currentDir,
		path: d.currentDir,
		kind: entryUseThisDir,
	})

	// Set default selection to the current directory entry
	d.selected = useThisDirIndex

	// Add blank line between current dir and the rest of the browse entries
	d.entries = append(d.entries, dirEntry{kind: entryBlankLine})

	// Add parent directory entry if not at root
	if d.currentDir != "/" {
		d.entries = append(d.entries, dirEntry{
			name: "..",
			path: filepath.Dir(d.currentDir),
			kind: entryParentDir,
		})
	}

	dirEntries, err := os.ReadDir(d.currentDir)
	if err != nil {
		d.err = err
		d.filtered = d.entries
		return
	}

	// Only add directories
	for _, entry := range dirEntries {
		// Skip hidden directories
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		if entry.IsDir() {
			fullPath := filepath.Join(d.currentDir, entry.Name())
			d.entries = append(d.entries, dirEntry{
				name: entry.Name() + "/",
				path: fullPath,
				kind: entryDir,
			})
		}
	}

	d.filterEntries()
}

func (d *workingDirPickerDialog) Init() tea.Cmd {
	return textinput.Blink
}

func (d *workingDirPickerDialog) Update(msg tea.Msg) (layout.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		cmd := d.SetSize(msg.Width, msg.Height)
		return d, cmd

	case tea.PasteMsg:
		var cmd tea.Cmd
		d.textInput, cmd = d.textInput.Update(msg)
		d.filterEntries()
		return d, cmd

	case tea.MouseClickMsg:
		return d.handleMouseClick(msg)

	case tea.MouseMotionMsg:
		return d.handleMouseMotion(msg)

	case tea.MouseReleaseMsg:
		return d.handleMouseRelease(msg)

	case tea.MouseWheelMsg:
		return d.handleMouseWheel(msg)

	case messages.WheelCoalescedMsg:
		return d.handleWheelCoalesced(msg)

	case tea.KeyPressMsg:
		if cmd := HandleQuit(msg); cmd != nil {
			return d, cmd
		}

		switch {
		case key.Matches(msg, d.keyMap.Escape):
			return d, core.CmdHandler(CloseDialogMsg{})

		case key.Matches(msg, d.keyMap.Up):
			d.movePrev()
			return d, nil

		case key.Matches(msg, d.keyMap.Down):
			d.moveNext()
			return d, nil

		case key.Matches(msg, d.keyMap.PageUp):
			for range d.pageSize() {
				d.movePrev()
			}
			return d, nil

		case key.Matches(msg, d.keyMap.PageDown):
			for range d.pageSize() {
				d.moveNext()
			}
			return d, nil

		case key.Matches(msg, d.keyMap.Enter):
			cmd := d.handleSelection()
			return d, cmd

		case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+s"))):
			d.toggleFavorite()
			return d, nil

		default:
			var cmd tea.Cmd
			d.textInput, cmd = d.textInput.Update(msg)
			d.filterEntries()
			return d, cmd
		}
	}

	return d, nil
}

// movePrev moves selection to the previous selectable entry.
func (d *workingDirPickerDialog) movePrev() {
	for i := d.selected - 1; i >= 0; i-- {
		if !d.filtered[i].isNonSelectable() {
			d.selected = i
			d.needsScrollToSel = true
			return
		}
	}
}

// moveNext moves selection to the next selectable entry.
func (d *workingDirPickerDialog) moveNext() {
	for i := d.selected + 1; i < len(d.filtered); i++ {
		if !d.filtered[i].isNonSelectable() {
			d.selected = i
			d.needsScrollToSel = true
			return
		}
	}
}

// toggleFavorite toggles the favorite status of the currently selected entry's path.
func (d *workingDirPickerDialog) toggleFavorite() {
	if d.tuiStore == nil {
		return
	}

	// Determine the path to toggle
	var togglePath string
	if d.selected >= 0 && d.selected < len(d.filtered) {
		entry := d.filtered[d.selected]
		switch entry.kind {
		case entryUseThisDir:
			togglePath = d.currentDir
		case entryFavoriteDir, entryRecentDir, entryDir, entryParentDir:
			togglePath = entry.path
		default:
			return
		}
	} else {
		return
	}

	ctx := context.Background()
	isFav, err := d.tuiStore.ToggleFavoriteDir(ctx, togglePath)
	if err != nil {
		return
	}

	// Update local state
	if isFav {
		d.favoriteSet[togglePath] = true
		d.favoriteDirs = append(d.favoriteDirs, togglePath)
		// Remove from recent if present
		d.recentDirs = removeFromSlice(d.recentDirs, togglePath)
	} else {
		delete(d.favoriteSet, togglePath)
		d.favoriteDirs = removeFromSlice(d.favoriteDirs, togglePath)
	}

	// Rebuild entries, then restore selection to the same path.
	// The index shifts when pinning/unpinning because entries are added/removed
	// in the Pinned and Recent sections above the Browse section.
	selectedPath := togglePath
	savedScroll := d.scrollbar.GetScrollOffset()
	d.loadDirectory()

	// Find the same path in the rebuilt list
	found := false
	for i, e := range d.filtered {
		if !e.isNonSelectable() && e.path == selectedPath {
			d.selected = i
			found = true
			break
		}
	}
	if !found {
		d.ensureSelectableSelected()
	}

	d.scrollbar.SetScrollOffset(savedScroll)
	d.needsScrollToSel = true
}

// removeFromSlice removes all occurrences of val from s.
func removeFromSlice(s []string, val string) []string {
	var result []string
	for _, v := range s {
		if v != val {
			result = append(result, v)
		}
	}
	return result
}

// starClickWidth is the clickable area width for the star indicator (matches sidebar).
const starClickWidth = 2

// handleMouseClick handles mouse click events on the dialog.
func (d *workingDirPickerDialog) handleMouseClick(msg tea.MouseClickMsg) (layout.Model, tea.Cmd) {
	// Check if click is on the scrollbar
	if d.isMouseOnScrollbar(msg.X, msg.Y) {
		sb, cmd := d.scrollbar.Update(msg)
		d.scrollbar = sb
		return d, cmd
	}

	// Check if click is on an entry in the list
	if msg.Button == tea.MouseLeft {
		if entryIdx := d.mouseYToEntryIndex(msg.Y); entryIdx >= 0 {
			// Check if click is on the star icon (first 2 chars of the entry content area).
			// Content starts after: dialog border(1) + padding(2) = 3 chars from dialog left edge.
			_, dialogCol := d.Position()
			contentStartX := dialogCol + 3 // border + padding
			clickXInContent := msg.X - contentStartX

			if clickXInContent >= 0 && clickXInContent < starClickWidth {
				// Star click: only toggle for entry kinds that show a star indicator
				entry := d.filtered[entryIdx]
				if entry.kind != entryParentDir {
					d.selected = entryIdx
					d.toggleFavorite()
					return d, nil
				}
			}

			now := time.Now()

			// Check for double-click: same index within threshold
			if entryIdx == d.lastClickIndex && now.Sub(d.lastClickTime) < styles.DoubleClickThreshold {
				// Double-click: confirm selection
				d.selected = entryIdx
				d.lastClickTime = time.Time{} // Reset to prevent triple-click
				cmd := d.handleSelection()
				return d, cmd
			}

			// Single click: just highlight
			d.selected = entryIdx
			d.lastClickTime = now
			d.lastClickIndex = entryIdx
		}
	}
	return d, nil
}

// handleMouseMotion handles mouse drag events (for scrollbar dragging).
func (d *workingDirPickerDialog) handleMouseMotion(msg tea.MouseMotionMsg) (layout.Model, tea.Cmd) {
	if d.scrollbar.IsDragging() {
		sb, cmd := d.scrollbar.Update(msg)
		d.scrollbar = sb
		return d, cmd
	}
	return d, nil
}

// handleMouseRelease handles mouse button release events.
func (d *workingDirPickerDialog) handleMouseRelease(msg tea.MouseReleaseMsg) (layout.Model, tea.Cmd) {
	if d.scrollbar.IsDragging() {
		sb, cmd := d.scrollbar.Update(msg)
		d.scrollbar = sb
		return d, cmd
	}
	return d, nil
}

// handleMouseWheel handles mouse wheel scrolling inside the dialog.
func (d *workingDirPickerDialog) handleMouseWheel(msg tea.MouseWheelMsg) (layout.Model, tea.Cmd) {
	// Only scroll if mouse is inside the dialog
	if !d.isMouseInDialog(msg.X, msg.Y) {
		return d, nil
	}

	switch msg.Button {
	case tea.MouseWheelUp:
		d.scrollbar.ScrollUp()
		d.scrollbar.ScrollUp() // Scroll 2 lines at a time
	case tea.MouseWheelDown:
		d.scrollbar.ScrollDown()
		d.scrollbar.ScrollDown() // Scroll 2 lines at a time
	}
	return d, nil
}

// handleWheelCoalesced handles coalesced wheel events (aggregated deltas).
// The background model forwards WheelCoalescedMsg to dialogs, so we convert
// the delta back into individual scroll operations.
func (d *workingDirPickerDialog) handleWheelCoalesced(msg messages.WheelCoalescedMsg) (layout.Model, tea.Cmd) {
	if !d.isMouseInDialog(msg.X, msg.Y) {
		return d, nil
	}

	steps := msg.Delta
	if steps < 0 {
		steps = -steps
		for range steps {
			d.scrollbar.ScrollUp()
		}
	} else {
		for range steps {
			d.scrollbar.ScrollDown()
		}
	}
	return d, nil
}

// isMouseInDialog checks if the mouse position is inside the dialog bounds.
func (d *workingDirPickerDialog) isMouseInDialog(x, y int) bool {
	dialogRow, dialogCol := d.Position()
	dialogWidth, maxHeight, _ := d.dialogSize()

	return x >= dialogCol && x < dialogCol+dialogWidth &&
		y >= dialogRow && y < dialogRow+maxHeight
}

// isMouseOnScrollbar checks if the mouse position is on the scrollbar.
func (d *workingDirPickerDialog) isMouseOnScrollbar(x, y int) bool {
	dialogWidth, maxHeight, _ := d.dialogSize()
	maxItems := maxHeight - dirPickerListVerticalOverhead

	if len(d.filtered) <= maxItems {
		return false // No scrollbar when content fits
	}

	dialogRow, dialogCol := d.Position()
	scrollbarX := dialogCol + dialogWidth - dirPickerScrollbarXInset - scrollbar.Width
	scrollbarY := dialogRow + dirPickerScrollbarYOffset

	return x >= scrollbarX && x < scrollbarX+scrollbar.Width &&
		y >= scrollbarY && y < scrollbarY+maxItems
}

// mouseYToEntryIndex converts a mouse Y position to an entry index in `filtered`.
// Returns -1 if the position is not on a selectable entry.
func (d *workingDirPickerDialog) mouseYToEntryIndex(y int) int {
	dialogRow, _ := d.Position()
	_, maxHeight, _ := d.dialogSize()
	maxItems := maxHeight - dirPickerListVerticalOverhead

	listStartY := dialogRow + dirPickerListStartOffset
	listEndY := listStartY + maxItems

	// Check if Y is within the directory list area
	if y < listStartY || y >= listEndY {
		return -1
	}

	// Calculate which line in the visible area was clicked
	lineInView := y - listStartY
	scrollOffset := d.scrollbar.GetScrollOffset()
	entryIdx := scrollOffset + lineInView

	if entryIdx < 0 || entryIdx >= len(d.filtered) {
		return -1
	}

	// Don't select non-selectable entries (separators, blank lines)
	if d.filtered[entryIdx].isNonSelectable() {
		return -1
	}

	return entryIdx
}

func (d *workingDirPickerDialog) handleSelection() tea.Cmd {
	if d.selected < 0 || d.selected >= len(d.filtered) {
		return nil
	}

	entry := d.filtered[d.selected]

	switch entry.kind {
	case entryUseThisDir, entryFavoriteDir, entryRecentDir:
		// Select this path as the working directory
		return tea.Sequence(
			core.CmdHandler(CloseDialogMsg{}),
			core.CmdHandler(messages.SpawnSessionMsg{WorkingDir: entry.path}),
		)
	case entryParentDir, entryDir:
		// Navigate into directory
		d.currentDir = entry.path
		d.textInput.SetValue("")
		d.loadDirectory()
		return nil
	case entrySeparator, entryBlankLine:
		return nil
	}

	return nil
}

func (d *workingDirPickerDialog) filterEntries() {
	query := strings.ToLower(strings.TrimSpace(d.textInput.Value()))
	if query == "" {
		d.filtered = d.entries
		d.selectUseThisDir()
		d.scrollbar.SetScrollOffset(0)
		return
	}

	d.filtered = nil
	for _, entry := range d.entries {
		switch entry.kind {
		case entryUseThisDir:
			// Always include
			d.filtered = append(d.filtered, entry)
		case entrySeparator, entryBlankLine:
			// Skip separators and blank lines when filtering
			continue
		case entryParentDir:
			// Always include
			d.filtered = append(d.filtered, entry)
		default:
			// Match against name or path
			if strings.Contains(strings.ToLower(entry.name), query) ||
				strings.Contains(strings.ToLower(entry.path), query) {
				d.filtered = append(d.filtered, entry)
			}
		}
	}

	d.ensureSelectableSelected()
	d.scrollbar.SetScrollOffset(0)
}

// selectUseThisDir selects the entryUseThisDir entry (current directory) if present,
// otherwise falls back to the first selectable entry.
func (d *workingDirPickerDialog) selectUseThisDir() {
	for i, e := range d.filtered {
		if e.kind == entryUseThisDir {
			d.selected = i
			return
		}
	}
	d.ensureSelectableSelected()
}

// ensureSelectableSelected ensures the selection is on a selectable entry.
func (d *workingDirPickerDialog) ensureSelectableSelected() {
	if d.selected >= len(d.filtered) {
		d.selected = max(0, len(d.filtered)-1)
	}
	// Skip forward past non-selectable entries
	for d.selected < len(d.filtered) && d.filtered[d.selected].isNonSelectable() {
		d.selected++
	}
	if d.selected >= len(d.filtered) {
		// Try backward
		d.selected = max(0, len(d.filtered)-1)
		for d.selected > 0 && d.filtered[d.selected].isNonSelectable() {
			d.selected--
		}
	}
}

func (d *workingDirPickerDialog) dialogSize() (dialogWidth, maxHeight, contentWidth int) {
	dialogWidth = max(min(d.Width()*dirPickerWidthPercent/100, dirPickerMaxWidth), dirPickerMinWidth)
	maxHeight = min(d.Height()*dirPickerHeightPercent/100, dirPickerMaxHeight)
	contentWidth = dialogWidth - dirPickerDialogPadding - scrollbar.Width - dirPickerScrollbarGap
	return dialogWidth, maxHeight, contentWidth
}

func (d *workingDirPickerDialog) View() string {
	dialogWidth, maxHeight, contentWidth := d.dialogSize()

	d.textInput.SetWidth(contentWidth)

	maxItems := maxHeight - dirPickerListVerticalOverhead

	// Build all entry lines (1:1 with d.filtered)
	var allEntryLines []string
	for i, entry := range d.filtered {
		allEntryLines = append(allEntryLines, d.renderEntry(entry, i == d.selected, contentWidth))
	}

	totalLines := len(allEntryLines)
	visibleLines := maxItems

	// Update scrollbar dimensions
	d.scrollbar.SetDimensions(visibleLines, totalLines)

	// Only auto-scroll to selection when keyboard navigation occurred
	if d.needsScrollToSel {
		scrollOffset := d.scrollbar.GetScrollOffset()
		if d.selected < scrollOffset {
			d.scrollbar.SetScrollOffset(d.selected)
		} else if d.selected >= scrollOffset+visibleLines {
			d.scrollbar.SetScrollOffset(d.selected - visibleLines + 1)
		}
		d.needsScrollToSel = false
	}

	// Slice visible lines based on scroll offset
	scrollOffset := d.scrollbar.GetScrollOffset()
	visibleEnd := min(scrollOffset+visibleLines, totalLines)
	visibleEntryLines := allEntryLines[scrollOffset:visibleEnd]

	// Pad with empty lines if content is shorter than visible area
	for len(visibleEntryLines) < visibleLines {
		visibleEntryLines = append(visibleEntryLines, "")
	}

	// Handle empty state
	if len(d.filtered) == 0 {
		visibleEntryLines = []string{"", styles.DialogContentStyle.
			Italic(true).
			Align(lipgloss.Center).
			Width(contentWidth).
			Render("No directories found")}
		for len(visibleEntryLines) < visibleLines {
			visibleEntryLines = append(visibleEntryLines, "")
		}
	}

	if d.err != nil {
		visibleEntryLines = append(visibleEntryLines, "", styles.ErrorStyle.
			Align(lipgloss.Center).
			Width(contentWidth).
			Render(d.err.Error()))
	}

	// Build entry list with fixed width to keep scrollbar position stable
	entryListStyle := lipgloss.NewStyle().Width(contentWidth)
	var fixedWidthLines []string
	for _, line := range visibleEntryLines {
		fixedWidthLines = append(fixedWidthLines, entryListStyle.Render(line))
	}
	entryListContent := strings.Join(fixedWidthLines, "\n")

	// Set scrollbar position for mouse hit testing
	dialogRow, dialogCol := d.Position()
	scrollbarX := dialogCol + dialogWidth - dirPickerScrollbarXInset - scrollbar.Width
	scrollbarY := dialogRow + dirPickerScrollbarYOffset
	d.scrollbar.SetPosition(scrollbarX, scrollbarY)

	// Get scrollbar view
	scrollbarView := d.scrollbar.View()

	// Combine content with scrollbar (gap between content and scrollbar)
	// Always include the gap and scrollbar space to maintain consistent layout
	var scrollableContent string
	gap := strings.Repeat(" ", dirPickerScrollbarGap)
	if scrollbarView != "" {
		scrollableContent = lipgloss.JoinHorizontal(lipgloss.Top, entryListContent, gap, scrollbarView)
	} else {
		// No scrollbar needed, but still pad to maintain consistent width
		scrollbarPlaceholder := strings.Repeat(" ", scrollbar.Width)
		scrollableContent = lipgloss.JoinHorizontal(lipgloss.Top, entryListContent, gap, scrollbarPlaceholder)
	}

	content := NewContent(contentWidth+dirPickerScrollbarGap+scrollbar.Width).
		AddTitle("New Session: Select Working Directory").
		AddSpace().
		AddContent(d.textInput.View()).
		AddSeparator().
		AddContent(scrollableContent).
		AddSpace().
		AddHelpKeys("↑/↓", "navigate", "enter", "select", "ctrl+s", "star", "esc", "cancel").
		Build()

	return styles.DialogStyle.Width(dialogWidth).Render(content)
}

func (d *workingDirPickerDialog) pageSize() int {
	_, maxHeight, _ := d.dialogSize()
	return max(1, maxHeight-dirPickerListVerticalOverhead)
}

func (d *workingDirPickerDialog) renderEntry(entry dirEntry, selected bool, maxWidth int) string {
	// Blank lines
	if entry.kind == entryBlankLine {
		return ""
	}

	// Separator lines
	if entry.kind == entrySeparator {
		label := entry.name
		remaining := max(0, maxWidth-len(label)-2)
		return styles.MutedStyle.Render(label + strings.Repeat("─", remaining))
	}

	nameStyle := styles.PaletteUnselectedActionStyle
	if selected {
		nameStyle = styles.PaletteSelectedActionStyle
	}

	var prefix string
	var name string

	switch entry.kind {
	case entryUseThisDir:
		isFav := d.favoriteSet[entry.path]
		prefix = styles.StarIndicator(isFav)
		suffix := "  (current)"
		displayPath := entry.path
		maxPathLen := maxWidth - 6 - len(suffix)
		if len(displayPath) > maxPathLen {
			displayPath = "…" + displayPath[len(displayPath)-(maxPathLen-1):]
		}
		name = displayPath + styles.MutedStyle.Render(suffix)
	case entryFavoriteDir:
		prefix = styles.StarIndicator(true)
		displayPath := entry.path
		maxPathLen := maxWidth - 6
		if len(displayPath) > maxPathLen {
			displayPath = "…" + displayPath[len(displayPath)-(maxPathLen-1):]
		}
		name = displayPath
	case entryRecentDir:
		prefix = styles.StarIndicator(false)
		displayPath := entry.path
		maxPathLen := maxWidth - 6
		if len(displayPath) > maxPathLen {
			displayPath = "…" + displayPath[len(displayPath)-(maxPathLen-1):]
		}
		name = displayPath
	case entryParentDir:
		prefix = "  "
		name = ".."
	case entryDir:
		isFav := d.favoriteSet[entry.path]
		prefix = styles.StarIndicator(isFav)
		name = entry.name
	default:
		prefix = "  "
		name = entry.name
	}

	maxNameLen := maxWidth - 6
	if len(name) > maxNameLen {
		name = name[:maxNameLen-1] + "…"
	}

	return prefix + nameStyle.Render(name)
}

func (d *workingDirPickerDialog) Position() (row, col int) {
	dialogWidth, maxHeight, _ := d.dialogSize()
	return CenterPosition(d.Width(), d.Height(), dialogWidth, maxHeight)
}
