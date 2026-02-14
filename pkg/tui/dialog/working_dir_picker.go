package dialog

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/docker/cagent/pkg/fsx"
	"github.com/docker/cagent/pkg/tui/components/scrollview"
	"github.com/docker/cagent/pkg/tui/core"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/messages"
	"github.com/docker/cagent/pkg/tui/service/tuistate"
	"github.com/docker/cagent/pkg/tui/styles"
)

// dirSection identifies which section of the picker is active.
type dirSection int

const (
	sectionBrowse dirSection = iota
	sectionRecent
	sectionPinned
)

// dirEntryKind distinguishes the different types of entries in the picker.
type dirEntryKind int

const (
	entryPinnedDir dirEntryKind = iota
	entryRecentDir
	entryUseThisDir
	entryParentDir
	entryDir
)

type dirEntry struct {
	name string
	path string
	kind dirEntryKind
}

// truncatePath shortens a path to fit within maxLen, prefixing with "…" when truncated.
func truncatePath(path string, maxLen int) string {
	if len(path) <= maxLen {
		return path
	}
	return "…" + path[len(path)-(maxLen-1):]
}

const (
	// Dialog sizing
	dirPickerWidthPercent  = 80
	dirPickerMinWidth      = 50
	dirPickerMaxWidth      = 100
	dirPickerHeightPercent = 70
	dirPickerMaxHeight     = 150

	// Dialog chrome dimensions (from styles.DialogStyle: Border + Padding(1,2))
	dirPickerBorderWidth    = 1 // lipgloss.RoundedBorder() is 1 cell per side
	dirPickerHorizPadding   = 2 // DialogStyle Padding(1, 2) horizontal
	dirPickerVertPadding    = 1 // DialogStyle Padding(1, 2) vertical
	dirPickerHorizChrome    = (dirPickerBorderWidth + dirPickerHorizPadding) * 2
	dirPickerContentOffsetX = dirPickerBorderWidth + dirPickerHorizPadding
	dirPickerContentOffsetY = dirPickerBorderWidth + dirPickerVertPadding

	// Content rows above the list (pinned/recent sections):
	// title(1) + titleGap(1) + tabs(1) + space(1) = 4
	dirPickerHeaderRows = 4

	// Additional content rows for browse section:
	// filterInput(1) + space(1) = 2
	dirPickerBrowseFilterRows = 2

	// Content rows below the list: space(1) + helpKeys(1) = 2
	dirPickerFooterRows = 2

	// Total vertical overhead = chrome + header + footer + section-specific rows
	dirPickerOverheadSimple = dirPickerContentOffsetY*2 + dirPickerHeaderRows + dirPickerFooterRows
	dirPickerOverheadBrowse = dirPickerOverheadSimple + dirPickerBrowseFilterRows

	// Y offset from dialog top to the list area
	dirPickerListStartSimple = dirPickerContentOffsetY + dirPickerHeaderRows
	dirPickerListStartBrowse = dirPickerListStartSimple + dirPickerBrowseFilterRows

	dirPickerMaxRecentDirs = 5

	// Entry rendering
	dirPickerStarPrefixWidth   = 2 // "★ " or "☆ " — star + space
	dirPickerIndentPrefixWidth = 2 // "  " — two-space indent for non-starred entries
	dirPickerFolderIconWidth   = 3 // "📁 " — emoji + space (occupies 2 cells + 1 space, but measured as 3)
	dirPickerTabGap            = 4 // spaces between tab labels

	dirPickerFilterCharLimit = 256
)

// tabRegion stores the X range of a rendered tab for mouse hit-testing.
type tabRegion struct {
	xStart, xEnd int
	section      dirSection
}

type workingDirPickerDialog struct {
	BaseDialog
	textInput textinput.Model
	section   dirSection

	// Pinned section state
	pinnedEntries  []dirEntry
	pinnedSelected int
	pinnedScroll   *scrollview.Model

	// Recent section state
	recentEntries  []dirEntry
	recentSelected int
	recentScroll   *scrollview.Model

	// Browse section state
	currentDir     string
	browseEntries  []dirEntry
	browseFiltered []dirEntry
	browseSelected int
	browseScroll   *scrollview.Model
	browseErr      error

	// Shared state
	recentDirs   []string
	favoriteDirs []string
	favoriteSet  map[string]bool
	tuiStore     *tuistate.Store
	keyMap       commandPaletteKeyMap

	// Tab click regions (recomputed each render)
	tabRegions []tabRegion

	// Double-click detection
	lastClickTime  time.Time
	lastClickIndex int
}

// NewWorkingDirPickerDialog creates a new working directory picker dialog.
// recentDirs provides a list of recently used directories to show.
// favoriteDirs provides a list of pinned directories to show.
// store is used for persisting favorite directory changes (may be nil).
// sessionWorkingDir is the working directory of the active session; when non-empty
// it is used as the initial browse directory instead of the process working directory.
func NewWorkingDirPickerDialog(recentDirs, favoriteDirs []string, store *tuistate.Store, sessionWorkingDir string) Dialog {
	ti := textinput.New()
	ti.Placeholder = "Type to filter directories…"
	ti.Focus()
	ti.CharLimit = dirPickerFilterCharLimit
	ti.SetWidth(dirPickerMinWidth)

	cwd := sessionWorkingDir
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			cwd = "/"
		}
	}

	favSet := make(map[string]bool, len(favoriteDirs))
	for _, d := range favoriteDirs {
		favSet[d] = true
	}

	// Remove favorites, current dir, and empty paths from recent dirs
	var filteredRecent []string
	for _, d := range recentDirs {
		if d != "" && !favSet[d] && d != cwd {
			filteredRecent = append(filteredRecent, d)
		}
	}

	d := &workingDirPickerDialog{
		textInput:    ti,
		section:      sectionBrowse,
		currentDir:   cwd,
		recentDirs:   filteredRecent,
		favoriteDirs: favoriteDirs,
		favoriteSet:  favSet,
		tuiStore:     store,
		keyMap:       defaultCommandPaletteKeyMap(),
		pinnedScroll: scrollview.New(scrollview.WithReserveScrollbarSpace(true)),
		recentScroll: scrollview.New(scrollview.WithReserveScrollbarSpace(true)),
		browseScroll: scrollview.New(scrollview.WithReserveScrollbarSpace(true)),
	}

	d.rebuildPinnedEntries()
	d.rebuildRecentEntries()
	d.loadBrowseDirectory()

	return d
}

func (d *workingDirPickerDialog) rebuildPinnedEntries() {
	d.pinnedEntries = nil

	for _, dir := range d.favoriteDirs {
		if dir == "" {
			continue
		}
		d.pinnedEntries = append(d.pinnedEntries, dirEntry{name: dir, path: dir, kind: entryPinnedDir})
	}

	if d.pinnedSelected >= len(d.pinnedEntries) {
		d.pinnedSelected = max(0, len(d.pinnedEntries)-1)
	}
}

func (d *workingDirPickerDialog) rebuildRecentEntries() {
	d.recentEntries = nil

	for i, dir := range d.recentDirs {
		if i >= dirPickerMaxRecentDirs {
			break
		}
		if dir == "" || dir == d.currentDir {
			continue
		}
		d.recentEntries = append(d.recentEntries, dirEntry{name: dir, path: dir, kind: entryRecentDir})
	}

	slices.SortFunc(d.recentEntries, func(a, b dirEntry) int {
		return strings.Compare(a.path, b.path)
	})

	if d.recentSelected >= len(d.recentEntries) {
		d.recentSelected = max(0, len(d.recentEntries)-1)
	}
}

func (d *workingDirPickerDialog) loadBrowseDirectory() {
	d.browseEntries = nil
	d.browseFiltered = nil
	d.browseSelected = 0
	d.browseErr = nil
	d.browseScroll.SetScrollOffset(0)

	// Current directory entry (select to use)
	d.browseEntries = append(d.browseEntries, dirEntry{
		name: d.currentDir,
		path: d.currentDir,
		kind: entryUseThisDir,
	})

	if d.currentDir != "/" {
		d.browseEntries = append(d.browseEntries, dirEntry{
			name: "..",
			path: filepath.Dir(d.currentDir),
			kind: entryParentDir,
		})
	}

	var shouldIgnore func(string) bool
	if vcsMatcher, err := fsx.NewVCSMatcher(d.currentDir); err == nil && vcsMatcher != nil {
		shouldIgnore = vcsMatcher.ShouldIgnore
	}

	dirEntries, err := os.ReadDir(d.currentDir)
	if err != nil {
		d.browseErr = err
		d.browseFiltered = d.browseEntries
		return
	}

	for _, entry := range dirEntries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if !entry.IsDir() {
			continue
		}
		fullPath := filepath.Join(d.currentDir, entry.Name())
		if shouldIgnore != nil && shouldIgnore(fullPath) {
			continue
		}
		d.browseEntries = append(d.browseEntries, dirEntry{
			name: entry.Name() + "/",
			path: fullPath,
			kind: entryDir,
		})
	}

	d.browseFiltered = d.browseEntries
}

func (d *workingDirPickerDialog) Init() tea.Cmd {
	return textinput.Blink
}

func (d *workingDirPickerDialog) Update(msg tea.Msg) (layout.Model, tea.Cmd) {
	activeScroll := d.activeScrollview()
	if handled, cmd := activeScroll.Update(msg); handled {
		return d, cmd
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		cmd := d.SetSize(msg.Width, msg.Height)
		return d, cmd

	case tea.PasteMsg:
		if d.section == sectionBrowse {
			var cmd tea.Cmd
			d.textInput, cmd = d.textInput.Update(msg)
			d.filterBrowseEntries()
			return d, cmd
		}
		return d, nil

	case tea.MouseClickMsg:
		return d.handleMouseClick(msg)

	case tea.KeyPressMsg:
		if cmd := HandleQuit(msg); cmd != nil {
			return d, cmd
		}

		switch {
		case key.Matches(msg, d.keyMap.Escape):
			return d, core.CmdHandler(CloseDialogMsg{})

		case key.Matches(msg, key.NewBinding(key.WithKeys("tab"))):
			d.cycleSectionForward()
			return d, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("shift+tab"))):
			d.cycleSectionBackward()
			return d, nil

		case key.Matches(msg, d.keyMap.Up):
			d.moveUp()
			return d, nil

		case key.Matches(msg, d.keyMap.Down):
			d.moveDown()
			return d, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("pgup"))):
			for range d.pageSize() {
				d.moveUp()
			}
			return d, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("pgdown"))):
			for range d.pageSize() {
				d.moveDown()
			}
			return d, nil

		case key.Matches(msg, d.keyMap.Enter):
			cmd := d.handleSelection()
			return d, cmd

		case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+p"))):
			if d.pinHelpLabel() != "" {
				d.toggleFavorite()
			}
			return d, nil

		default:
			if d.section == sectionBrowse {
				var cmd tea.Cmd
				d.textInput, cmd = d.textInput.Update(msg)
				d.filterBrowseEntries()
				return d, cmd
			}
			return d, nil
		}
	}

	return d, nil
}

func (d *workingDirPickerDialog) activeScrollview() *scrollview.Model {
	switch d.section {
	case sectionPinned:
		return d.pinnedScroll
	case sectionRecent:
		return d.recentScroll
	default:
		return d.browseScroll
	}
}

func (d *workingDirPickerDialog) cycleSectionForward() {
	switch d.section {
	case sectionBrowse:
		d.section = sectionRecent
	case sectionRecent:
		d.section = sectionPinned
	case sectionPinned:
		d.section = sectionBrowse
	}
	d.updateSectionFocus()
}

func (d *workingDirPickerDialog) cycleSectionBackward() {
	switch d.section {
	case sectionBrowse:
		d.section = sectionPinned
	case sectionPinned:
		d.section = sectionRecent
	case sectionRecent:
		d.section = sectionBrowse
	}
	d.updateSectionFocus()
}

func (d *workingDirPickerDialog) updateSectionFocus() {
	if d.section == sectionBrowse {
		d.textInput.Focus()
	} else {
		d.textInput.Blur()
	}
}

func (d *workingDirPickerDialog) moveUp() {
	switch d.section {
	case sectionPinned:
		if d.pinnedSelected > 0 {
			d.pinnedSelected--
			d.pinnedScroll.EnsureLineVisible(d.pinnedSelected)
		}
	case sectionRecent:
		if d.recentSelected > 0 {
			d.recentSelected--
			d.recentScroll.EnsureLineVisible(d.recentSelected)
		}
	case sectionBrowse:
		if d.browseSelected > 0 {
			d.browseSelected--
			d.browseScroll.EnsureLineVisible(d.browseSelected)
		}
	}
}

func (d *workingDirPickerDialog) moveDown() {
	switch d.section {
	case sectionPinned:
		if d.pinnedSelected < len(d.pinnedEntries)-1 {
			d.pinnedSelected++
			d.pinnedScroll.EnsureLineVisible(d.pinnedSelected)
		}
	case sectionRecent:
		if d.recentSelected < len(d.recentEntries)-1 {
			d.recentSelected++
			d.recentScroll.EnsureLineVisible(d.recentSelected)
		}
	case sectionBrowse:
		if d.browseSelected < len(d.browseFiltered)-1 {
			d.browseSelected++
			d.browseScroll.EnsureLineVisible(d.browseSelected)
		}
	}
}

func (d *workingDirPickerDialog) handleSelection() tea.Cmd {
	switch d.section {
	case sectionPinned:
		if d.pinnedSelected < 0 || d.pinnedSelected >= len(d.pinnedEntries) {
			return nil
		}
		entry := d.pinnedEntries[d.pinnedSelected]
		return tea.Sequence(
			core.CmdHandler(CloseDialogMsg{}),
			core.CmdHandler(messages.SpawnSessionMsg{WorkingDir: entry.path}),
		)
	case sectionRecent:
		if d.recentSelected < 0 || d.recentSelected >= len(d.recentEntries) {
			return nil
		}
		entry := d.recentEntries[d.recentSelected]
		return tea.Sequence(
			core.CmdHandler(CloseDialogMsg{}),
			core.CmdHandler(messages.SpawnSessionMsg{WorkingDir: entry.path}),
		)
	default:
		// sectionBrowse
	}

	if d.browseSelected < 0 || d.browseSelected >= len(d.browseFiltered) {
		return nil
	}
	entry := d.browseFiltered[d.browseSelected]

	switch entry.kind {
	case entryUseThisDir:
		return tea.Sequence(
			core.CmdHandler(CloseDialogMsg{}),
			core.CmdHandler(messages.SpawnSessionMsg{WorkingDir: entry.path}),
		)
	case entryParentDir, entryDir:
		d.currentDir = entry.path
		d.textInput.SetValue("")
		d.loadBrowseDirectory()
		return nil
	}

	return nil
}

func (d *workingDirPickerDialog) toggleFavorite() {
	if d.tuiStore == nil {
		return
	}

	var togglePath string
	switch d.section {
	case sectionPinned:
		if d.pinnedSelected < 0 || d.pinnedSelected >= len(d.pinnedEntries) {
			return
		}
		togglePath = d.pinnedEntries[d.pinnedSelected].path
	case sectionRecent:
		if d.recentSelected < 0 || d.recentSelected >= len(d.recentEntries) {
			return
		}
		togglePath = d.recentEntries[d.recentSelected].path
	case sectionBrowse:
		if d.browseSelected < 0 || d.browseSelected >= len(d.browseFiltered) {
			return
		}
		entry := d.browseFiltered[d.browseSelected]
		if entry.kind == entryParentDir {
			return
		}
		togglePath = entry.path
	}

	ctx := context.Background()
	isFav, err := d.tuiStore.ToggleFavoriteDir(ctx, togglePath)
	if err != nil {
		return
	}

	if isFav {
		d.favoriteSet[togglePath] = true
		d.favoriteDirs = append(d.favoriteDirs, togglePath)
		d.recentDirs = removeFromSlice(d.recentDirs, togglePath)
	} else {
		delete(d.favoriteSet, togglePath)
		d.favoriteDirs = removeFromSlice(d.favoriteDirs, togglePath)
	}

	savedPinnedPath := ""
	if d.pinnedSelected >= 0 && d.pinnedSelected < len(d.pinnedEntries) {
		savedPinnedPath = d.pinnedEntries[d.pinnedSelected].path
	}

	d.rebuildPinnedEntries()
	d.rebuildRecentEntries()

	// Restore pinned selection to same path if possible
	if savedPinnedPath != "" {
		for i, e := range d.pinnedEntries {
			if e.path == savedPinnedPath {
				d.pinnedSelected = i
				break
			}
		}
	}
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

func (d *workingDirPickerDialog) handleMouseClick(msg tea.MouseClickMsg) (layout.Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft {
		return d, nil
	}

	// Check tab clicks
	if clicked := d.tabClickTarget(msg.X, msg.Y); clicked >= 0 {
		d.setSection(dirSection(clicked))
		return d, nil
	}

	entryIdx := d.mouseYToEntryIndex(msg.Y)
	if entryIdx < 0 {
		return d, nil
	}

	// Check if the click lands on the star/pin column
	if d.isStarClick(msg.X, entryIdx) {
		d.setSelected(entryIdx)
		if d.pinHelpLabel() != "" {
			d.toggleFavorite()
		}
		return d, nil
	}

	now := time.Now()
	if entryIdx == d.lastClickIndex && now.Sub(d.lastClickTime) < styles.DoubleClickThreshold {
		d.setSelected(entryIdx)
		d.lastClickTime = time.Time{}
		cmd := d.handleSelection()
		return d, cmd
	}

	d.setSelected(entryIdx)
	d.lastClickTime = now
	d.lastClickIndex = entryIdx

	return d, nil
}

// isStarClick returns true if the click X coordinate falls within the star prefix
// column for the given entry index, and the entry supports pinning.
func (d *workingDirPickerDialog) isStarClick(x, entryIdx int) bool {
	_, dialogCol := d.Position()
	starStartX := dialogCol + dirPickerContentOffsetX
	starEndX := starStartX + dirPickerStarPrefixWidth

	if x < starStartX || x >= starEndX {
		return false
	}

	switch d.section {
	case sectionPinned:
		return true
	case sectionBrowse:
		if entryIdx >= 0 && entryIdx < len(d.browseFiltered) {
			kind := d.browseFiltered[entryIdx].kind
			return kind == entryUseThisDir || kind == entryDir
		}
	}
	return false
}

func (d *workingDirPickerDialog) setSection(s dirSection) {
	d.section = s
	if d.section == sectionBrowse {
		d.textInput.Focus()
	} else {
		d.textInput.Blur()
	}
}

// tabClickTarget returns the section index if the click is on a tab, or -1.
func (d *workingDirPickerDialog) tabClickTarget(x, y int) int {
	dialogRow, dialogCol := d.Position()
	const rowsBeforeTabs = 2 // title(1) + titleGap(1)
	tabY := dialogRow + dirPickerContentOffsetY + rowsBeforeTabs
	if y != tabY {
		return -1
	}

	contentX := x - (dialogCol + dirPickerContentOffsetX)

	for _, r := range d.tabRegions {
		if contentX >= r.xStart && contentX < r.xEnd {
			return int(r.section)
		}
	}
	return -1
}

func (d *workingDirPickerDialog) setSelected(idx int) {
	switch d.section {
	case sectionPinned:
		d.pinnedSelected = idx
	case sectionRecent:
		d.recentSelected = idx
	case sectionBrowse:
		d.browseSelected = idx
	}
}

func (d *workingDirPickerDialog) mouseYToEntryIndex(y int) int {
	dialogRow, _ := d.Position()
	_, maxHeight, _ := d.dialogSize()

	var listStartY, overhead int
	var entries int
	switch d.section {
	case sectionPinned:
		listStartY = dialogRow + dirPickerListStartSimple
		overhead = dirPickerOverheadSimple
		entries = len(d.pinnedEntries)
	case sectionRecent:
		listStartY = dialogRow + dirPickerListStartSimple
		overhead = dirPickerOverheadSimple
		entries = len(d.recentEntries)
	case sectionBrowse:
		listStartY = dialogRow + dirPickerListStartBrowse
		overhead = dirPickerOverheadBrowse
		entries = len(d.browseFiltered)
	}

	maxItems := maxHeight - overhead
	listEndY := listStartY + maxItems

	if y < listStartY || y >= listEndY {
		return -1
	}

	lineInView := y - listStartY
	scroll := d.activeScrollview()
	entryIdx := scroll.ScrollOffset() + lineInView

	if entryIdx < 0 || entryIdx >= entries {
		return -1
	}

	return entryIdx
}

func (d *workingDirPickerDialog) filterBrowseEntries() {
	query := strings.ToLower(strings.TrimSpace(d.textInput.Value()))
	if query == "" {
		d.browseFiltered = d.browseEntries
		d.browseSelected = 0
		d.browseScroll.SetScrollOffset(0)
		return
	}

	d.browseFiltered = nil
	for _, entry := range d.browseEntries {
		// Always include current dir and parent
		if entry.kind == entryUseThisDir || entry.kind == entryParentDir {
			d.browseFiltered = append(d.browseFiltered, entry)
			continue
		}
		if strings.Contains(strings.ToLower(entry.name), query) ||
			strings.Contains(strings.ToLower(entry.path), query) {
			d.browseFiltered = append(d.browseFiltered, entry)
		}
	}

	if d.browseSelected >= len(d.browseFiltered) {
		d.browseSelected = 0
	}
	d.browseScroll.SetScrollOffset(0)
}

func (d *workingDirPickerDialog) dialogSize() (dialogWidth, maxHeight, contentWidth int) {
	dialogWidth = max(min(d.Width()*dirPickerWidthPercent/100, dirPickerMaxWidth), dirPickerMinWidth)
	maxHeight = min(d.Height()*dirPickerHeightPercent/100, dirPickerMaxHeight)
	contentWidth = dialogWidth - dirPickerHorizChrome - d.pinnedScroll.ReservedCols()
	return dialogWidth, maxHeight, contentWidth
}

func (d *workingDirPickerDialog) View() string {
	dialogWidth, _, contentWidth := d.dialogSize()
	d.textInput.SetWidth(contentWidth)
	regionWidth := contentWidth + d.pinnedScroll.ReservedCols()

	// Tab header
	tabLine := d.renderTabs(regionWidth)

	// Build content based on active section
	pinLabel := d.pinHelpLabel()
	helpKeys := []string{"↑/↓", "navigate", "tab/shift+tab", "section", "enter", "select"}
	if pinLabel != "" {
		helpKeys = append(helpKeys, "ctrl+p", pinLabel)
	}
	helpKeys = append(helpKeys, "esc", "cancel")
	var contentBuilder *Content
	switch d.section {
	case sectionPinned:
		scrollableContent := d.renderPinnedList(contentWidth)

		contentBuilder = NewContent(regionWidth).
			AddTitle("New Session: Select Working Directory").
			AddSpace().
			AddContent(tabLine).
			AddSpace().
			AddContent(scrollableContent).
			AddSpace().
			AddHelpKeys(helpKeys...)
	case sectionRecent:
		scrollableContent := d.renderRecentList(contentWidth)

		contentBuilder = NewContent(regionWidth).
			AddTitle("New Session: Select Working Directory").
			AddSpace().
			AddContent(tabLine).
			AddSpace().
			AddContent(scrollableContent).
			AddSpace().
			AddHelpKeys(helpKeys...)
	case sectionBrowse:
		scrollableContent := d.renderBrowseList(contentWidth)

		contentBuilder = NewContent(regionWidth).
			AddTitle("New Session: Select Working Directory").
			AddSpace().
			AddContent(tabLine).
			AddSpace().
			AddContent(d.textInput.View()).
			AddSpace().
			AddContent(scrollableContent).
			AddSpace().
			AddHelpKeys(helpKeys...)
	}

	content := contentBuilder.Build()
	return styles.DialogStyle.Width(dialogWidth).Render(content)
}

func (d *workingDirPickerDialog) renderTabs(width int) string {
	activeStyle := styles.HighlightWhiteStyle.Underline(true)
	inactiveStyle := styles.MutedStyle
	countStyle := styles.MutedStyle
	activeCountStyle := styles.SecondaryStyle

	type tabInfo struct {
		visualWidth int
		rendered    string
		section     dirSection
	}

	renderTab := func(label string, count int, active bool, section dirSection) tabInfo {
		style := inactiveStyle
		cStyle := countStyle
		if active {
			style = activeStyle
			cStyle = activeCountStyle
		}
		s := style.Render(label)
		vw := lipgloss.Width(s)
		if count > 0 {
			countStr := " " + cStyle.Render("("+strconv.Itoa(count)+")")
			s += countStr
			vw += lipgloss.Width(countStr)
		}
		return tabInfo{visualWidth: vw, rendered: s, section: section}
	}

	tabs := []tabInfo{
		renderTab("Browse", 0, d.section == sectionBrowse, sectionBrowse),
		renderTab("Recent", len(d.recentEntries), d.section == sectionRecent, sectionRecent),
		renderTab("Pinned", len(d.pinnedEntries), d.section == sectionPinned, sectionPinned),
	}

	totalWidth := 0
	for i, t := range tabs {
		totalWidth += t.visualWidth
		if i < len(tabs)-1 {
			totalWidth += dirPickerTabGap
		}
	}

	padLeft := max(0, (width-totalWidth)/2)

	d.tabRegions = d.tabRegions[:0]
	xPos := padLeft
	for i, t := range tabs {
		d.tabRegions = append(d.tabRegions, tabRegion{
			xStart:  xPos,
			xEnd:    xPos + t.visualWidth,
			section: t.section,
		})
		xPos += t.visualWidth
		if i < len(tabs)-1 {
			xPos += dirPickerTabGap
		}
	}

	var parts []string
	for i, t := range tabs {
		parts = append(parts, t.rendered)
		if i < len(tabs)-1 {
			parts = append(parts, strings.Repeat(" ", dirPickerTabGap))
		}
	}

	line := strings.Join(parts, "")
	return styles.BaseStyle.Width(width).Align(lipgloss.Center).Render(line)
}

func (d *workingDirPickerDialog) renderPinnedList(contentWidth int) string {
	var allLines []string
	for i, entry := range d.pinnedEntries {
		allLines = append(allLines, d.renderPinnedEntry(entry, i == d.pinnedSelected, contentWidth))
	}

	dialogRow, dialogCol := d.Position()
	d.pinnedScroll.SetPosition(dialogCol+dirPickerContentOffsetX, dialogRow+dirPickerListStartSimple)
	d.pinnedScroll.SetContent(allLines, len(allLines))

	if len(d.pinnedEntries) == 0 {
		visLines := d.pinnedScroll.VisibleHeight()
		emptyLines := []string{
			"", styles.DialogContentStyle.
				Italic(true).Align(lipgloss.Center).Width(contentWidth).
				Render("No pinned directories"), "",
			styles.MutedStyle.Align(lipgloss.Center).Width(contentWidth).
				Render("Use ctrl+p in Browse to pin directories"),
		}
		for len(emptyLines) < visLines {
			emptyLines = append(emptyLines, "")
		}
		return d.pinnedScroll.ViewWithLines(emptyLines)
	}

	return d.pinnedScroll.View()
}

func (d *workingDirPickerDialog) renderBrowseList(contentWidth int) string {
	var allLines []string
	for i, entry := range d.browseFiltered {
		allLines = append(allLines, d.renderBrowseEntry(entry, i == d.browseSelected, contentWidth))
	}

	dialogRow, dialogCol := d.Position()
	d.browseScroll.SetPosition(dialogCol+dirPickerContentOffsetX, dialogRow+dirPickerListStartBrowse)
	d.browseScroll.SetContent(allLines, len(allLines))

	if d.browseErr != nil {
		visLines := d.browseScroll.VisibleHeight()
		errLines := []string{"", styles.ErrorStyle.
			Align(lipgloss.Center).Width(contentWidth).
			Render(d.browseErr.Error())}
		for len(errLines) < visLines {
			errLines = append(errLines, "")
		}
		return d.browseScroll.ViewWithLines(errLines)
	}

	if len(d.browseFiltered) == 0 {
		visLines := d.browseScroll.VisibleHeight()
		emptyLines := []string{"", styles.DialogContentStyle.
			Italic(true).Align(lipgloss.Center).Width(contentWidth).
			Render("No directories found")}
		for len(emptyLines) < visLines {
			emptyLines = append(emptyLines, "")
		}
		return d.browseScroll.ViewWithLines(emptyLines)
	}

	return d.browseScroll.View()
}

func (d *workingDirPickerDialog) renderPinnedEntry(entry dirEntry, selected bool, maxWidth int) string {
	nameStyle := styles.PaletteUnselectedActionStyle
	if selected {
		nameStyle = styles.PaletteSelectedActionStyle
	}

	prefix := styles.StarredStyle.Render("★") + " "
	availableWidth := maxWidth - dirPickerStarPrefixWidth
	displayPath := truncatePath(entry.path, availableWidth)

	return prefix + nameStyle.Render(displayPath)
}

func (d *workingDirPickerDialog) renderBrowseEntry(entry dirEntry, selected bool, maxWidth int) string {
	nameStyle := styles.PaletteUnselectedActionStyle
	if selected {
		nameStyle = styles.PaletteSelectedActionStyle
	}

	availableWidth := maxWidth - dirPickerStarPrefixWidth

	switch entry.kind {
	case entryUseThisDir:
		prefix := styles.StarIndicator(d.favoriteSet[entry.path])
		suffixText := "  (use this dir)"
		suffix := styles.MutedStyle.Render(suffixText)
		name := truncatePath(entry.path, availableWidth-len(suffixText))
		return prefix + nameStyle.Render(name) + suffix

	case entryParentDir:
		indent := strings.Repeat(" ", dirPickerIndentPrefixWidth)
		return indent + nameStyle.Render("..")

	case entryDir:
		prefix := styles.StarIndicator(d.favoriteSet[entry.path])
		icon := "📁 "
		name := entry.name
		nameLimit := availableWidth - dirPickerFolderIconWidth
		if len(name) > nameLimit {
			name = name[:nameLimit-1] + "…"
		}
		return prefix + nameStyle.Render(icon+name)
	}

	return ""
}

func (d *workingDirPickerDialog) renderRecentList(contentWidth int) string {
	var allLines []string
	for i, entry := range d.recentEntries {
		allLines = append(allLines, d.renderRecentEntry(entry, i == d.recentSelected, contentWidth))
	}

	dialogRow, dialogCol := d.Position()
	d.recentScroll.SetPosition(dialogCol+dirPickerContentOffsetX, dialogRow+dirPickerListStartSimple)
	d.recentScroll.SetContent(allLines, len(allLines))

	if len(d.recentEntries) == 0 {
		visLines := d.recentScroll.VisibleHeight()
		emptyLines := []string{
			"", styles.DialogContentStyle.
				Italic(true).Align(lipgloss.Center).Width(contentWidth).
				Render("No recent directories"),
		}
		for len(emptyLines) < visLines {
			emptyLines = append(emptyLines, "")
		}
		return d.recentScroll.ViewWithLines(emptyLines)
	}

	return d.recentScroll.View()
}

func (d *workingDirPickerDialog) renderRecentEntry(entry dirEntry, selected bool, maxWidth int) string {
	nameStyle := styles.PaletteUnselectedActionStyle
	if selected {
		nameStyle = styles.PaletteSelectedActionStyle
	}

	availableWidth := maxWidth - dirPickerIndentPrefixWidth
	displayPath := truncatePath(entry.path, availableWidth)
	indent := strings.Repeat(" ", dirPickerIndentPrefixWidth)

	return indent + nameStyle.Render(displayPath)
}

func (d *workingDirPickerDialog) pinHelpLabel() string {
	switch d.section {
	case sectionPinned:
		return "unpin"
	case sectionBrowse:
		if d.browseSelected >= 0 && d.browseSelected < len(d.browseFiltered) {
			entry := d.browseFiltered[d.browseSelected]
			if entry.kind == entryParentDir {
				return ""
			}
			if d.favoriteSet[entry.path] {
				return "unpin"
			}
		}
	}
	return "pin"
}

func (d *workingDirPickerDialog) pageSize() int {
	_, maxHeight, _ := d.dialogSize()
	if d.section == sectionBrowse {
		return max(1, maxHeight-dirPickerOverheadBrowse)
	}
	return max(1, maxHeight-dirPickerOverheadSimple)
}

// SetSize sets the dialog dimensions and configures both scrollview regions.
func (d *workingDirPickerDialog) SetSize(width, height int) tea.Cmd {
	cmd := d.BaseDialog.SetSize(width, height)
	_, maxHeight, contentWidth := d.dialogSize()
	regionWidth := contentWidth + d.pinnedScroll.ReservedCols()

	pinnedVis := max(1, maxHeight-dirPickerOverheadSimple)
	d.pinnedScroll.SetSize(regionWidth, pinnedVis)
	d.recentScroll.SetSize(regionWidth, pinnedVis)

	browseVis := max(1, maxHeight-dirPickerOverheadBrowse)
	d.browseScroll.SetSize(regionWidth, browseVis)

	return cmd
}

func (d *workingDirPickerDialog) Position() (row, col int) {
	dialogWidth, maxHeight, _ := d.dialogSize()
	return CenterPosition(d.Width(), d.Height(), dialogWidth, maxHeight)
}
