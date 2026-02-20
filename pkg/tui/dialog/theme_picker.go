package dialog

import (
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/docker/cagent/pkg/tui/components/scrollview"
	"github.com/docker/cagent/pkg/tui/components/toolcommon"
	"github.com/docker/cagent/pkg/tui/core"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/messages"
	"github.com/docker/cagent/pkg/tui/styles"
)

// ThemeChoice represents a selectable theme option
type ThemeChoice struct {
	Ref       string // Theme reference ("default" for built-in default)
	Name      string // Display name
	IsCurrent bool   // Currently active theme
	IsDefault bool   // Built-in default theme ("default")
	IsBuiltin bool   // Built-in theme shipped with cagent
}

// themePickerDialog is a dialog for selecting a theme.
type themePickerDialog struct {
	BaseDialog
	textInput  textinput.Model
	themes     []ThemeChoice
	filtered   []ThemeChoice
	selected   int
	keyMap     commandPaletteKeyMap
	scrollview *scrollview.Model

	// Double-click detection
	lastClickTime  time.Time
	lastClickIndex int

	// Original theme for restoration on cancel
	originalThemeRef string

	// Avoid re-applying the same preview repeatedly (e.g., during filtering)
	lastPreviewRef string
}

// NewThemePickerDialog creates a new theme picker dialog.
// originalThemeRef is the currently active theme ref (for restoration on cancel).
func NewThemePickerDialog(themes []ThemeChoice, originalThemeRef string) Dialog {
	ti := textinput.New()
	ti.Placeholder = "Type to search themes…"
	ti.Focus()
	ti.CharLimit = 100
	ti.SetWidth(50)
	ti.SetStyles(styles.DialogInputStyle)

	// Sort themes: built-in first, then custom. Within each section:
	// current first, then default, then alphabetically.
	sortedThemes := make([]ThemeChoice, len(themes))
	copy(sortedThemes, themes)
	sort.Slice(sortedThemes, func(i, j int) bool {
		getPriority := func(t ThemeChoice) int {
			if t.IsBuiltin {
				return 0
			}
			return 1
		}
		pi, pj := getPriority(sortedThemes[i]), getPriority(sortedThemes[j])
		if pi != pj {
			return pi < pj
		}
		if sortedThemes[i].IsCurrent != sortedThemes[j].IsCurrent {
			return sortedThemes[i].IsCurrent
		}
		if sortedThemes[i].IsDefault != sortedThemes[j].IsDefault {
			return sortedThemes[i].IsDefault
		}
		ni := strings.ToLower(sortedThemes[i].Name)
		nj := strings.ToLower(sortedThemes[j].Name)
		if ni != nj {
			return ni < nj
		}
		return sortedThemes[i].Ref < sortedThemes[j].Ref
	})

	d := &themePickerDialog{
		textInput:        ti,
		themes:           themes,
		filtered:         nil,
		keyMap:           defaultCommandPaletteKeyMap(),
		scrollview:       scrollview.New(scrollview.WithReserveScrollbarSpace(true)),
		originalThemeRef: originalThemeRef,
	}

	d.themes = sortedThemes
	d.filterThemes()

	// Find current theme and select it (if multiple are marked current, pick first)
	for i, t := range d.filtered {
		if t.IsCurrent {
			d.selected = i
			d.scrollview.EnsureLineVisible(d.findSelectedLine(nil)) // Scroll to current selection on open
			break
		}
	}

	// Initialize preview tracking to current selection (theme is already applied when dialog opens)
	if d.selected >= 0 && d.selected < len(d.filtered) {
		sel := d.filtered[d.selected]
		d.lastPreviewRef = sel.Ref
	}

	return d
}

func (d *themePickerDialog) Init() tea.Cmd {
	return textinput.Blink
}

func (d *themePickerDialog) Update(msg tea.Msg) (layout.Model, tea.Cmd) {
	// Scrollview handles mouse scrollbar, wheel, and pgup/pgdn/home/end
	if handled, cmd := d.scrollview.Update(msg); handled {
		return d, cmd
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		cmd := d.SetSize(msg.Width, msg.Height)
		return d, cmd

	case messages.ThemeChangedMsg:
		d.textInput.SetStyles(styles.DialogInputStyle)
		return d, nil

	case tea.PasteMsg:
		var cmd tea.Cmd
		d.textInput, cmd = d.textInput.Update(msg)
		if selectionChanged := d.filterThemes(); selectionChanged {
			d.scrollview.EnsureLineVisible(d.findSelectedLine(nil))
			return d, tea.Batch(cmd, d.emitPreview())
		}
		return d, cmd

	case tea.MouseClickMsg:
		// Scrollbar clicks handled above; this handles list item clicks
		if msg.Button == tea.MouseLeft {
			if themeIdx := d.mouseYToThemeIndex(msg.Y); themeIdx >= 0 {
				now := time.Now()
				if themeIdx == d.lastClickIndex && now.Sub(d.lastClickTime) < styles.DoubleClickThreshold {
					d.selected = themeIdx
					d.lastClickTime = time.Time{}
					cmd := d.handleSelection()
					return d, cmd
				}
				oldSelected := d.selected
				d.selected = themeIdx
				d.lastClickTime = now
				d.lastClickIndex = themeIdx
				if d.selected != oldSelected {
					cmd := d.emitPreview()
					return d, cmd
				}
			}
		}
		return d, nil

	case tea.KeyPressMsg:
		if cmd := HandleQuit(msg); cmd != nil {
			return d, cmd
		}

		switch {
		case key.Matches(msg, d.keyMap.Escape):
			return d, tea.Sequence(
				core.CmdHandler(CloseDialogMsg{}),
				core.CmdHandler(messages.ThemeCancelPreviewMsg{OriginalRef: d.originalThemeRef}),
			)

		case key.Matches(msg, d.keyMap.Up):
			if d.selected > 0 {
				d.selected--
				d.scrollview.EnsureLineVisible(d.findSelectedLine(nil))
				cmd := d.emitPreview()
				return d, cmd
			}
			return d, nil

		case key.Matches(msg, d.keyMap.Down):
			if d.selected < len(d.filtered)-1 {
				d.selected++
				d.scrollview.EnsureLineVisible(d.findSelectedLine(nil))
				cmd := d.emitPreview()
				return d, cmd
			}
			return d, nil

		case key.Matches(msg, d.keyMap.Enter):
			cmd := d.handleSelection()
			return d, cmd

		default:
			var cmd tea.Cmd
			d.textInput, cmd = d.textInput.Update(msg)
			if selectionChanged := d.filterThemes(); selectionChanged {
				d.scrollview.EnsureLineVisible(d.findSelectedLine(nil))
				return d, tea.Batch(cmd, d.emitPreview())
			}
			return d, cmd
		}
	}

	return d, nil
}

func (d *themePickerDialog) mouseYToThemeIndex(y int) int {
	dialogRow, _ := d.Position()
	maxItems := d.scrollview.VisibleHeight()

	listStartY := dialogRow + pickerListStartOffset
	listEndY := listStartY + maxItems

	if y < listStartY || y >= listEndY {
		return -1
	}

	lineInView := y - listStartY
	scrollOffset := d.scrollview.ScrollOffset()
	actualLine := scrollOffset + lineInView

	return d.lineToThemeIndex(actualLine)
}

func (d *themePickerDialog) handleSelection() tea.Cmd {
	if d.selected >= 0 && d.selected < len(d.filtered) {
		selected := d.filtered[d.selected]
		return tea.Sequence(
			core.CmdHandler(CloseDialogMsg{}),
			core.CmdHandler(messages.ChangeThemeMsg{ThemeRef: selected.Ref}),
		)
	}
	return nil
}

// emitPreview requests a theme preview via an app-level message.
func (d *themePickerDialog) emitPreview() tea.Cmd {
	if d.selected >= 0 && d.selected < len(d.filtered) {
		selected := d.filtered[d.selected]

		// Skip if we're already previewing this exact selection.
		if selected.Ref == d.lastPreviewRef {
			return nil
		}
		d.lastPreviewRef = selected.Ref

		return core.CmdHandler(messages.ThemePreviewMsg{
			ThemeRef:    selected.Ref,
			OriginalRef: d.originalThemeRef,
		})
	}
	return nil
}

const customThemesSeparatorLabel = "── Custom themes "

func (d *themePickerDialog) dialogSize() (dialogWidth, maxHeight, contentWidth int) {
	dialogWidth = max(min(d.Width()*pickerWidthPercent/100, pickerMaxWidth), pickerMinWidth)
	maxHeight = min(d.Height()*pickerHeightPercent/100, pickerMaxHeight)
	contentWidth = dialogWidth - pickerDialogPadding - d.scrollview.ReservedCols()
	return dialogWidth, maxHeight, contentWidth
}

// SetSize sets the dialog dimensions and configures the scrollview.
func (d *themePickerDialog) SetSize(width, height int) tea.Cmd {
	cmd := d.BaseDialog.SetSize(width, height)
	_, maxHeight, contentWidth := d.dialogSize()
	regionWidth := contentWidth + d.scrollview.ReservedCols()
	visLines := max(1, maxHeight-pickerListVerticalOverhead)
	d.scrollview.SetSize(regionWidth, visLines)
	return cmd
}

func (d *themePickerDialog) View() string {
	dialogWidth, _, contentWidth := d.dialogSize()
	d.textInput.SetWidth(contentWidth)

	// Build all theme lines
	var allLines []string
	customSeparatorShown := false

	// Pre-compute which groups exist to decide on separators
	hasBuiltinThemes := false
	for _, t := range d.filtered {
		if t.IsBuiltin {
			hasBuiltinThemes = true
			break
		}
	}

	for i, theme := range d.filtered {
		// Add separator before first custom theme if there are built-in themes above.
		if !theme.IsBuiltin && !customSeparatorShown {
			if hasBuiltinThemes {
				separatorLine := styles.MutedStyle.Render(customThemesSeparatorLabel + strings.Repeat("─", max(0, contentWidth-lipgloss.Width(customThemesSeparatorLabel)-2)))
				allLines = append(allLines, separatorLine)
			}
			customSeparatorShown = true
		}

		allLines = append(allLines, d.renderTheme(theme, i == d.selected, contentWidth))
	}

	regionWidth := contentWidth + d.scrollview.ReservedCols()

	// Set scrollview position for mouse hit-testing (auto-computed from dialog position)
	dialogRow, dialogCol := d.Position()
	d.scrollview.SetPosition(dialogCol+3, dialogRow+pickerListStartOffset)

	d.scrollview.SetContent(allLines, len(allLines))

	var scrollableContent string
	if len(d.filtered) == 0 {
		visLines := d.scrollview.VisibleHeight()
		emptyLines := []string{"", styles.DialogContentStyle.
			Italic(true).Align(lipgloss.Center).Width(contentWidth).
			Render("No themes found")}
		for len(emptyLines) < visLines {
			emptyLines = append(emptyLines, "")
		}
		scrollableContent = d.scrollview.ViewWithLines(emptyLines)
	} else {
		scrollableContent = d.scrollview.View()
	}

	content := NewContent(regionWidth).
		AddTitle("Select Theme").
		AddSpace().
		AddContent(d.textInput.View()).
		AddSeparator().
		AddContent(scrollableContent).
		AddSpace().
		AddHelpKeys("↑/↓", "navigate", "enter", "select", "esc", "cancel").
		Build()

	return styles.DialogStyle.Width(dialogWidth).Render(content)
}

func (d *themePickerDialog) renderTheme(theme ThemeChoice, selected bool, maxWidth int) string {
	nameStyle, descStyle := styles.PaletteUnselectedActionStyle, styles.PaletteUnselectedDescStyle
	defaultBadgeStyle := styles.BadgeDefaultStyle
	currentBadgeStyle := styles.BadgeCurrentStyle
	if selected {
		nameStyle, descStyle = styles.PaletteSelectedActionStyle, styles.PaletteSelectedDescStyle
		defaultBadgeStyle = defaultBadgeStyle.Background(styles.MobyBlue)
		currentBadgeStyle = currentBadgeStyle.Background(styles.MobyBlue)
	}

	// Display name
	displayName := theme.Name

	// Build description: for custom themes, show filename (without user: prefix)
	// For built-in themes, don't show filename - just the name is enough
	var desc string
	if !theme.IsBuiltin {
		// Custom theme - show filename for identification
		baseRef := strings.TrimPrefix(theme.Ref, styles.UserThemePrefix)
		desc = baseRef
	}

	// Calculate badge widths - show all applicable badges
	var badgeWidth int
	if theme.IsCurrent {
		badgeWidth += lipgloss.Width(" (current)")
	}
	if theme.IsDefault {
		badgeWidth += lipgloss.Width(" (default)")
	}

	separatorWidth := 0
	if desc != "" {
		separatorWidth = lipgloss.Width(" • ")
	}

	// Maximum width for name (leaving space for badges and description).
	maxNameWidth := maxWidth - badgeWidth
	if desc != "" {
		minDescWidth := min(10, lipgloss.Width(desc))
		maxNameWidth = maxWidth - badgeWidth - separatorWidth - minDescWidth
	}

	// Truncate name if needed.
	if lipgloss.Width(displayName) > maxNameWidth {
		displayName = toolcommon.TruncateText(displayName, maxNameWidth)
	}

	// Build the name with colored badges - show all applicable badges.
	// Order: name (current) (default) - most important context first.
	var nameParts []string
	nameParts = append(nameParts, nameStyle.Render(displayName))
	if theme.IsCurrent {
		nameParts = append(nameParts, currentBadgeStyle.Render(" (current)"))
	}
	if theme.IsDefault {
		nameParts = append(nameParts, defaultBadgeStyle.Render(" (default)"))
	}
	name := strings.Join(nameParts, "")

	if desc != "" {
		nameWidth := lipgloss.Width(name)
		remainingWidth := maxWidth - nameWidth - separatorWidth
		if remainingWidth > 0 {
			truncatedDesc := toolcommon.TruncateText(desc, remainingWidth)
			return name + descStyle.Render(" • "+truncatedDesc)
		}
	}

	return name
}

func (d *themePickerDialog) Position() (row, col int) {
	dialogWidth, maxHeight, _ := d.dialogSize()
	return CenterPosition(d.Width(), d.Height(), dialogWidth, maxHeight)
}

func (d *themePickerDialog) filterThemes() (selectionChanged bool) {
	query := strings.ToLower(strings.TrimSpace(d.textInput.Value()))

	// Remember current selection so filtering doesn't cause surprising jumps.
	prevRef := ""
	if d.selected >= 0 && d.selected < len(d.filtered) {
		prevRef = d.filtered[d.selected].Ref
	}

	d.filtered = nil
	for _, theme := range d.themes {
		if query == "" {
			d.filtered = append(d.filtered, theme)
			continue
		}

		searchText := strings.ToLower(theme.Name + " " + theme.Ref)
		if strings.Contains(searchText, query) {
			d.filtered = append(d.filtered, theme)
		}
	}

	// Restore selection if possible; otherwise fall back to first item.
	d.selected = 0
	if prevRef != "" {
		for i, t := range d.filtered {
			if t.Ref == prevRef {
				d.selected = i
				break
			}
		}
	}

	// Reset scroll when filtering.
	d.scrollview.SetScrollOffset(0)

	// Determine if selection changed.
	newRef := ""
	if d.selected >= 0 && d.selected < len(d.filtered) {
		newRef = d.filtered[d.selected].Ref
	}
	return newRef != prevRef
}

// lineToThemeIndex converts a line index (in the rendered list including separators)
// to a theme index in d.filtered. Returns -1 if the line is a separator.
func (d *themePickerDialog) lineToThemeIndex(lineIdx int) int {
	hasBuiltinThemes := false
	for _, t := range d.filtered {
		if t.IsBuiltin {
			hasBuiltinThemes = true
			break
		}
	}

	currentLine := 0
	customSeparatorShown := false

	for i, theme := range d.filtered {
		// Custom separator before first custom theme (if built-in themes exist above).
		if !theme.IsBuiltin && !customSeparatorShown {
			if hasBuiltinThemes {
				if currentLine == lineIdx {
					return -1
				}
				currentLine++
			}
			customSeparatorShown = true
		}

		if currentLine == lineIdx {
			return i
		}
		currentLine++
	}

	return -1
}

// findSelectedLine returns the line index (including separators) that corresponds to the selected theme.
func (d *themePickerDialog) findSelectedLine(_ []string) int {
	if d.selected < 0 || d.selected >= len(d.filtered) {
		return 0
	}

	hasBuiltinThemes := false
	for _, t := range d.filtered {
		if t.IsBuiltin {
			hasBuiltinThemes = true
			break
		}
	}

	lineIndex := 0
	customSeparatorShown := false

	for i := range d.selected + 1 {
		theme := d.filtered[i]

		if !theme.IsBuiltin && !customSeparatorShown {
			if hasBuiltinThemes && i <= d.selected {
				lineIndex++
			}
			customSeparatorShown = true
		}

		if i == d.selected {
			return lineIndex
		}
		lineIndex++
	}

	return lineIndex
}
