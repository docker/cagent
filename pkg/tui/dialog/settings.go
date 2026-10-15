package dialog

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/docker/docker-agent/pkg/tui/components/scrollview"
	"github.com/docker/docker-agent/pkg/tui/components/toolcommon"
	"github.com/docker/docker-agent/pkg/tui/core"
	"github.com/docker/docker-agent/pkg/tui/core/layout"
	"github.com/docker/docker-agent/pkg/tui/messages"
	"github.com/docker/docker-agent/pkg/tui/styles"
)

const (
	settingsWidthPercent = 60
	settingsMinWidth     = 52
	settingsMaxWidth     = 72
	previewMaxWidth      = 44
	previewMinWidth      = 24
)

const (
	tabAppearance = iota
	tabBehavior
	tabNotifications
	tabCount
)

var settingsTabLabels = [tabCount]string{"Appearance", "Behavior", "Notifications"}

const (
	rowTheme = iota
	rowPosition
	rowSpacing
	rowInfoMode
	rowSessionPath
	rowUsage
	rowAgents
	rowActiveAgents
	rowTools
	rowPlans
	rowTodos
	rowSplitDiff
	rowExpandThinking
	rowHideToolResults
	rowRenderImages
	rowShowBanner
	appearanceRowCount
)

const (
	rowSendMode = iota
	rowYOLO
	rowRestoreTabs
	rowSnapshot
	rowCacheStablePrompts
	rowLean
	rowTabTitleLength
	rowInterruptConfirmation
	behaviorRowCount
)

const (
	rowSound = iota
	rowSoundThreshold
	rowWarnOnCacheMiss
	notificationsRowCount
)

var sidebarPositions = []messages.SidebarPosition{
	messages.SidebarRight, messages.SidebarLeft, messages.SidebarTop, messages.SidebarBottom,
}

var positionLabels = map[messages.SidebarPosition]string{
	messages.SidebarRight: "Right", messages.SidebarLeft: "Left",
	messages.SidebarTop: "Top", messages.SidebarBottom: "Bottom",
}

var sectionSpacings = []messages.SectionSpacing{
	messages.SpacingCompact, messages.SpacingNormal, messages.SpacingRelaxed,
}

var spacingLabels = map[messages.SectionSpacing]string{
	messages.SpacingCompact: "Compact", messages.SpacingNormal: "Normal", messages.SpacingRelaxed: "Relaxed",
}

var sidebarInfoModes = []messages.SidebarInfoMode{
	messages.InfoModeCompact, messages.InfoModeDetailed,
}

var infoModeLabels = map[messages.SidebarInfoMode]string{
	messages.InfoModeCompact: "Compact", messages.InfoModeDetailed: "Detailed",
}

var sendModes = []messages.SendMode{messages.SendModeSteer, messages.SendModeQueue}

type sendModeOption struct {
	mode  messages.SendMode
	label string
	desc  string
}

var sendModeOptions = []sendModeOption{
	{messages.SendModeSteer, "Steer", "send to the working agent mid-turn"},
	{messages.SendModeQueue, "Queue", "hold until the current turn ends"},
}

var interruptConfirmationModes = []messages.InterruptMode{
	messages.InterruptModeAlways, messages.InterruptModeDoubleTap, messages.InterruptModeNone,
}

var interruptConfirmationLabels = map[messages.InterruptMode]string{
	messages.InterruptModeAlways:    "Always (confirm dialog)",
	messages.InterruptModeDoubleTap: "Double-tap (press Esc twice)",
	messages.InterruptModeNone:      "None (immediate)",
}

type settingsDialog struct {
	BaseDialog

	original    messages.Preferences
	current     messages.Preferences
	showVisuals bool
	tab         int
	selected    [tabCount]int
	confirmYOLO bool
	body        *scrollview.Model
}

func NewSettingsDialog(preferences messages.Preferences, showVisuals bool) Dialog {
	preferences.Layout.SidebarPosition = messages.ParseSidebarPosition(string(preferences.Layout.SidebarPosition))
	preferences.Layout.SectionSpacing = messages.ParseSectionSpacing(string(preferences.Layout.SectionSpacing))
	preferences.Layout.SidebarInfoMode = messages.ParseSidebarInfoMode(string(preferences.Layout.SidebarInfoMode))
	preferences.SendMode = messages.ParseSendMode(string(preferences.SendMode))
	if preferences.TabTitleMaxLength <= 0 {
		preferences.TabTitleMaxLength = 20
	}
	if preferences.SoundThreshold <= 0 {
		preferences.SoundThreshold = 10
	}
	if preferences.InterruptConfirmation == "" {
		preferences.InterruptConfirmation = messages.InterruptModeAlways
	}
	return &settingsDialog{
		original: preferences, current: preferences, showVisuals: showVisuals,
		body: scrollview.New(scrollview.WithKeyMap(nil), scrollview.WithReserveScrollbarSpace(true)),
	}
}

func (d *settingsDialog) Init() tea.Cmd { return nil }

func (d *settingsDialog) Update(msg tea.Msg) (layout.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		cmd := d.SetSize(msg.Width, msg.Height)
		return d, cmd
	case tea.KeyPressMsg:
		if cmd := HandleQuit(msg); cmd != nil {
			return d, cmd
		}
		cmd := d.handleKey(msg)
		return d, cmd
	}
	return d, nil
}

func (d *settingsDialog) rowCount() int {
	switch d.tab {
	case tabBehavior:
		return behaviorRowCount
	case tabNotifications:
		return notificationsRowCount
	default:
		return appearanceRowCount
	}
}

func (d *settingsDialog) selectable(tab, row int) bool {
	if tab == tabAppearance && !d.showVisuals && row >= rowPosition && row <= rowTodos {
		return false
	}
	if tab == tabAppearance && row == rowActiveAgents && d.current.Layout.HideAgents {
		return false
	}
	if tab == tabNotifications && row == rowSoundThreshold && !d.current.Sound {
		return false
	}
	return true
}

func (d *settingsDialog) moveSelection(delta int) {
	for next := d.selected[d.tab] + delta; next >= 0 && next < d.rowCount(); next += delta {
		if d.selectable(d.tab, next) {
			d.selected[d.tab] = next
			return
		}
	}
}

var settingsKeys = struct {
	Close, NextTab, PreviousTab, Up, Down, Home, End, Left, Right, Enter key.Binding
}{
	key.NewBinding(key.WithKeys("esc", "q")),
	key.NewBinding(key.WithKeys("tab")),
	key.NewBinding(key.WithKeys("shift+tab")),
	key.NewBinding(key.WithKeys("up", "k", "ctrl+k")),
	key.NewBinding(key.WithKeys("down", "j", "ctrl+j")),
	key.NewBinding(key.WithKeys("home", "g")),
	key.NewBinding(key.WithKeys("end", "G")),
	key.NewBinding(key.WithKeys("left", "h")),
	key.NewBinding(key.WithKeys("right", "l", "space")),
	key.NewBinding(key.WithKeys("enter")),
}

func (d *settingsDialog) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(msg, settingsKeys.Close):
		return d.cancel()
	case key.Matches(msg, settingsKeys.NextTab):
		d.confirmYOLO = false
		d.tab = (d.tab + 1) % tabCount
	case key.Matches(msg, settingsKeys.PreviousTab):
		d.confirmYOLO = false
		d.tab = (d.tab + tabCount - 1) % tabCount
	case key.Matches(msg, settingsKeys.Up):
		d.confirmYOLO = false
		d.moveSelection(-1)
	case key.Matches(msg, settingsKeys.Down):
		d.confirmYOLO = false
		d.moveSelection(1)
	case key.Matches(msg, settingsKeys.Home):
		d.selected[d.tab] = 0
	case key.Matches(msg, settingsKeys.End):
		d.selected[d.tab] = d.rowCount() - 1
		if !d.selectable(d.tab, d.selected[d.tab]) {
			d.moveSelection(-1)
		}
	case key.Matches(msg, settingsKeys.Left):
		return d.changeValue(-1)
	case key.Matches(msg, settingsKeys.Right):
		return d.changeValue(1)
	case key.Matches(msg, settingsKeys.Enter):
		if d.tab == tabAppearance && d.selected[d.tab] == rowTheme {
			return core.CmdHandler(messages.OpenThemePickerMsg{})
		}
		if d.tab == tabBehavior && d.selected[d.tab] == rowYOLO && !d.current.YOLO {
			return d.changeValue(1)
		}
		return d.apply()
	}
	return nil
}

func (d *settingsDialog) changeValue(delta int) tea.Cmd {
	switch d.tab {
	case tabAppearance:
		switch d.selected[d.tab] {
		case rowTheme:
			if delta > 0 {
				return core.CmdHandler(messages.OpenThemePickerMsg{})
			}
		case rowPosition:
			d.current.Layout.SidebarPosition = cycleValue(sidebarPositions, d.current.Layout.SidebarPosition, delta)
		case rowSpacing:
			d.current.Layout.SectionSpacing = cycleValue(sectionSpacings, d.current.Layout.SectionSpacing, delta)
		case rowInfoMode:
			d.current.Layout.SidebarInfoMode = cycleValue(sidebarInfoModes, d.current.Layout.SidebarInfoMode, delta)
		case rowSessionPath:
			d.current.Layout.HideSessionPath = !d.current.Layout.HideSessionPath
		case rowUsage:
			d.current.Layout.HideUsage = !d.current.Layout.HideUsage
		case rowAgents:
			d.current.Layout.HideAgents = !d.current.Layout.HideAgents
		case rowActiveAgents:
			if !d.current.Layout.HideAgents {
				d.current.Layout.ActiveAgentsOnly = !d.current.Layout.ActiveAgentsOnly
			}
		case rowTools:
			d.current.Layout.HideTools = !d.current.Layout.HideTools
		case rowPlans:
			d.current.Layout.ShowPlans = !d.current.Layout.ShowPlans
		case rowTodos:
			d.current.Layout.HideTodos = !d.current.Layout.HideTodos
		case rowSplitDiff:
			d.current.SplitDiffView = !d.current.SplitDiffView
		case rowExpandThinking:
			d.current.ExpandThinking = !d.current.ExpandThinking
		case rowHideToolResults:
			d.current.HideToolResults = !d.current.HideToolResults
		case rowRenderImages:
			d.current.RenderImages = !d.current.RenderImages
		case rowShowBanner:
			d.current.ShowBanner = !d.current.ShowBanner
		}
		if d.selected[d.tab] >= rowPosition && d.selected[d.tab] <= rowTodos {
			return core.CmdHandler(messages.PreviewLayoutMsg{Layout: d.current.Layout})
		}
	case tabBehavior:
		switch d.selected[d.tab] {
		case rowSendMode:
			d.current.SendMode = cycleValue(sendModes, d.current.SendMode, delta)
		case rowYOLO:
			switch {
			case d.current.YOLO:
				d.current.YOLO = false
				d.confirmYOLO = false
			case d.confirmYOLO:
				d.current.YOLO = true
				d.confirmYOLO = false
			default:
				d.confirmYOLO = true
			}
		case rowRestoreTabs:
			d.current.RestoreTabs = !d.current.RestoreTabs
		case rowSnapshot:
			d.current.Snapshot = !d.current.Snapshot
		case rowCacheStablePrompts:
			d.current.CacheStablePrompts = !d.current.CacheStablePrompts
		case rowLean:
			d.current.Lean = !d.current.Lean
		case rowTabTitleLength:
			d.current.TabTitleMaxLength = stepValue(d.current.TabTitleMaxLength, delta, 1, 5, 100)
		case rowInterruptConfirmation:
			d.current.InterruptConfirmation = cycleValue(interruptConfirmationModes, d.current.InterruptConfirmation, delta)
		}
	case tabNotifications:
		switch d.selected[d.tab] {
		case rowSound:
			d.current.Sound = !d.current.Sound
			if !d.current.Sound && d.selected[d.tab] == rowSoundThreshold {
				d.selected[d.tab] = rowSound
			}
		case rowSoundThreshold:
			if d.current.Sound {
				d.current.SoundThreshold = stepValue(d.current.SoundThreshold, delta, 1, 1, 300)
			}
		case rowWarnOnCacheMiss:
			d.current.WarnOnCacheMiss = !d.current.WarnOnCacheMiss
		}
	}
	return nil
}

func cycleValue[T comparable](values []T, current T, delta int) T {
	idx := 0
	for i, v := range values {
		if v == current {
			idx = i
			break
		}
	}
	idx = (idx + delta + len(values)) % len(values)
	return values[idx]
}

func stepValue(current, delta, step, minimum, maximum int) int {
	return max(minimum, min(maximum, current+delta*step))
}

func (d *settingsDialog) apply() tea.Cmd {
	if d.current == d.original {
		return closeDialogCmd()
	}
	return tea.Sequence(closeDialogCmd(), core.CmdHandler(messages.ApplySettingsMsg{Preferences: d.current}))
}

func (d *settingsDialog) cancel() tea.Cmd {
	if d.current.Layout == d.original.Layout {
		return closeDialogCmd()
	}
	return tea.Sequence(closeDialogCmd(), core.CmdHandler(messages.CancelLayoutPreviewMsg{Original: d.original.Layout}))
}

func (d *settingsDialog) Position() (row, col int) { return d.CenterDialog(d.View()) }

func (d *settingsDialog) View() string {
	width := min(d.ComputeDialogWidth(settingsWidthPercent, settingsMinWidth, settingsMaxWidth), max(8, d.Width()))
	inner := max(1, width-styles.DialogStyle.GetHorizontalFrameSize())
	header := NewContent(inner).AddTitle("Settings").AddSeparator().AddContent(d.renderTabBar(inner)).AddSpace().Build()
	footer := d.renderFooter(inner, max(2, d.Height()-styles.DialogStyle.GetVerticalFrameSize()-lipgloss.Height(header)-3))
	bodyHeight := max(1, d.Height()-styles.DialogStyle.GetVerticalFrameSize()-lipgloss.Height(header)-lipgloss.Height(footer))
	d.body.SetSize(inner, bodyHeight)
	body := d.renderBody(d.body.ContentWidth())
	d.body.SetContent(body.lines, len(body.lines))
	d.body.SetSize(inner, min(bodyHeight, len(body.lines)))
	d.body.SetScrollOffset(d.body.ScrollOffset())
	if d.selected[d.tab] == 0 && body.selectedEnd < d.body.VisibleHeight() {
		d.body.EnsureRangeVisible(0, body.selectedEnd)
	} else {
		d.body.EnsureRangeVisible(body.selectedStart, body.selectedEnd)
	}

	content := lipgloss.JoinVertical(lipgloss.Left, header, d.body.View(), footer)
	return styles.DialogStyle.Width(width).Render(content)
}

func (d *settingsDialog) renderTabBar(width int) string {
	tabs := make([]string, 0, tabCount)
	for i, label := range settingsTabLabels {
		style := styles.MutedStyle
		if i == d.tab {
			style = styles.HighlightWhiteStyle.Underline(true)
		}
		tabs = append(tabs, style.Render(label))
	}
	bar := strings.Join(tabs, "  ·  ")
	if lipgloss.Width(bar) > width {
		bar = styles.HighlightWhiteStyle.Render("‹ " + settingsTabLabels[d.tab] + " ›")
	}
	return lipgloss.PlaceHorizontal(width, lipgloss.Center, toolcommon.TruncateText(bar, width))
}

type settingsContent struct {
	lines                      []string
	selectedStart, selectedEnd int
}

func (c *settingsContent) addRow(row, selected int, value string) {
	start := len(c.lines)
	c.lines = append(c.lines, strings.Split(value, "\n")...)
	if row == selected {
		c.selectedStart, c.selectedEnd = start, len(c.lines)-1
	}
}

func (c *settingsContent) addGroup(label string, width int) {
	if len(c.lines) > 0 {
		c.lines = append(c.lines, "")
	}
	c.lines = append(c.lines, ansi.Truncate(RenderGroupSeparator(label, width), width, "…"))
}

func (d *settingsDialog) renderBody(width int) settingsContent {
	var content settingsContent
	switch d.tab {
	case tabBehavior:
		d.renderBehaviorTab(&content, width)
	case tabNotifications:
		d.renderNotificationsTab(&content, width)
	default:
		d.renderAppearanceTab(&content, width)
	}
	return content
}

func (d *settingsDialog) renderAppearanceTab(content *settingsContent, width int) {
	add := func(row int, value string) { content.addRow(row, d.selected[d.tab], value) }
	toggle := func(row int, label string, enabled bool) { add(row, d.renderToggleRow(row, label, enabled, width)) }
	theme := styles.GetPersistedThemeRef()
	if theme == "" {
		theme = styles.DefaultThemeRef
	}
	content.addGroup("Theme", width)
	add(rowTheme, d.renderSelectorRow(rowTheme, "Theme", theme, width))
	if d.showVisuals {
		content.addGroup("Sidebar layout", width)
		add(rowPosition, d.renderSelectorRow(rowPosition, "Sidebar position", positionLabels[d.current.Layout.SidebarPosition], width))
		add(rowSpacing, d.renderSelectorRow(rowSpacing, "Section spacing", spacingLabels[d.current.Layout.SectionSpacing], width))
		add(rowInfoMode, d.renderSelectorRow(rowInfoMode, "Sidebar info mode", infoModeLabels[d.current.Layout.SidebarInfoMode], width))
		if d.Height() >= 44 && width >= previewMaxWidth {
			preview := lipgloss.PlaceHorizontal(width, lipgloss.Center, renderLayoutPreview(d.current.Layout, width))
			content.lines = append(content.lines, strings.Split(preview, "\n")...)
		}
		content.addGroup("Sidebar sections", width)
		toggle(rowSessionPath, "Session path", !d.current.Layout.HideSessionPath)
		toggle(rowUsage, "Token usage", !d.current.Layout.HideUsage)
		toggle(rowAgents, "Agents", !d.current.Layout.HideAgents)
		add(rowActiveAgents, d.renderNestedToggleRow(rowActiveAgents, "Active agents only", d.current.Layout.ActiveAgentsOnly, d.current.Layout.HideAgents, width))
		toggle(rowTools, "Tools", !d.current.Layout.HideTools)
		toggle(rowPlans, "Plans", d.current.Layout.ShowPlans)
		toggle(rowTodos, "Todos", !d.current.Layout.HideTodos)
	}
	content.addGroup("Chat display", width)
	toggle(rowSplitDiff, "Split diff view", d.current.SplitDiffView)
	toggle(rowExpandThinking, "Expand thinking by default", d.current.ExpandThinking)
	toggle(rowHideToolResults, "Hide tool results by default", d.current.HideToolResults)
	toggle(rowRenderImages, "Render images", d.current.RenderImages)
	toggle(rowShowBanner, "Show startup banner", d.current.ShowBanner)
}

func (d *settingsDialog) renderBehaviorTab(content *settingsContent, width int) {
	add := func(row int, value string) { content.addRow(row, d.selected[d.tab], value) }
	toggle := func(row int, label string, enabled bool) { add(row, d.renderToggleRow(row, label, enabled, width)) }
	content.addGroup("While agent is working", width)
	options := make([]string, 0, len(sendModeOptions))
	optionStart, optionEnd := 0, 0
	for _, opt := range sendModeOptions {
		option := d.renderSendModeOption(opt, width)
		if d.current.SendMode == opt.mode {
			optionStart = lipgloss.Height(strings.Join(options, "\n"))
			if len(options) == 0 {
				optionStart = 0
			}
			optionEnd = optionStart + lipgloss.Height(option) - 1
		}
		options = append(options, option)
	}
	start := len(content.lines)
	add(rowSendMode, strings.Join(options, "\n"))
	if d.selected[d.tab] == rowSendMode {
		content.selectedStart, content.selectedEnd = start+optionStart, start+optionEnd
	}
	content.addGroup("Tool approval", width)
	toggle(rowYOLO, "Auto-approve tools by default", d.current.YOLO)
	content.addGroup("Session defaults", width)
	toggle(rowRestoreTabs, "Restore tabs on launch", d.current.RestoreTabs)
	toggle(rowSnapshot, "Automatic snapshots", d.current.Snapshot)
	toggle(rowCacheStablePrompts, "Cache-stable dynamic prompts", d.current.CacheStablePrompts)
	toggle(rowLean, "Lean UI by default", d.current.Lean)
	add(rowTabTitleLength, d.renderStepperRow(rowTabTitleLength, "Tab title max length", d.current.TabTitleMaxLength, "chars", width, false))
	content.addGroup("Interrupts", width)
	add(rowInterruptConfirmation, d.renderSelectorRow(rowInterruptConfirmation, "Interrupt confirmation", interruptConfirmationLabels[d.current.InterruptConfirmation], width))
}

func (d *settingsDialog) renderNotificationsTab(content *settingsContent, width int) {
	add := func(row int, value string) { content.addRow(row, d.selected[d.tab], value) }
	content.addGroup("Completion sound", width)
	add(rowSound, d.renderToggleRow(rowSound, "Sound on task completion", d.current.Sound, width))
	add(rowSoundThreshold, d.renderStepperRow(rowSoundThreshold, "Sound threshold", d.current.SoundThreshold, "seconds", width, !d.current.Sound))
	content.addGroup("Cache warnings", width)
	add(rowWarnOnCacheMiss, d.renderToggleRow(rowWarnOnCacheMiss, "Warn when a turn misses the cache", d.current.WarnOnCacheMiss, width))
}

func (d *settingsDialog) renderFooter(width, maxHeight int) string {
	enter := "apply"
	detail := "Changes are saved only when applied."
	if d.tab == tabAppearance {
		switch d.selected[d.tab] {
		case rowTheme:
			enter, detail = "open", "Theme opens a separate picker and saves immediately."
		case rowPosition, rowSpacing, rowInfoMode, rowSessionPath, rowUsage, rowAgents, rowActiveAgents, rowTools, rowPlans, rowTodos:
			detail = "Layout previews live. Esc restores the previous layout."
		}
	}
	if d.tab == tabBehavior {
		switch d.selected[d.tab] {
		case rowYOLO:
			detail = "Auto-approve lets tools run without confirmation."
			if !d.current.YOLO {
				enter = "confirm"
			}
		case rowRestoreTabs, rowLean:
			detail = "Takes effect on the next launch."
		}
	}
	if d.confirmYOLO {
		detail = "Tools can run without confirmation.\nPress again to enable."
	}
	style := styles.MutedStyle
	if d.confirmYOLO {
		style = styles.WarningStyle
	}
	detail = style.Render(ansi.Wrap(detail, width, ""))
	navigation := renderSettingsHelp(width, "↑↓", "move", "←→/space", "change")
	actions := renderSettingsHelp(width, "tab", "category", "enter", enter, "esc", "cancel")
	parts := []string{"", detail, navigation, actions}
	if lipgloss.Height(strings.Join(parts, "\n")) > maxHeight {
		parts = []string{"", detail, actions}
	}
	if lipgloss.Height(strings.Join(parts, "\n")) > maxHeight && !d.confirmYOLO {
		parts = []string{"", actions}
	}
	if lipgloss.Height(strings.Join(parts, "\n")) > maxHeight {
		actions = renderSettingsHelp(width, "enter", enter, "esc", "cancel")
		parts = []string{detail, actions}
	}
	return strings.Join(parts, "\n")
}

func renderSettingsHelp(width int, bindings ...string) string {
	var lines, row []string
	for i := 0; i < len(bindings); i += 2 {
		next := append(row, bindings[i:i+2]...)
		if len(row) > 0 && HelpKeysWidth(next...) > width {
			lines = append(lines, RenderHelpKeys(width, row...))
			row = bindings[i : i+2]
		} else {
			row = next
		}
	}
	return strings.Join(append(lines, RenderHelpKeys(width, row...)), "\n")
}

func (d *settingsDialog) renderSendModeOption(opt sendModeOption, width int) string {
	glyph, prefix := "○", "  "
	labelStyle, glyphStyle := styles.PaletteUnselectedActionStyle, styles.SecondaryStyle
	if d.current.SendMode == opt.mode {
		glyph = "●"
		glyphStyle = glyphStyle.Foreground(styles.Success)
		if d.selected[d.tab] == rowSendMode {
			prefix, labelStyle = styles.HighlightWhiteStyle.Render("› "), styles.PaletteSelectedActionStyle
		}
	}
	label := prefix + glyphStyle.Render(glyph) + " " + labelStyle.Render(opt.label)
	if width >= 52 {
		return label + styles.MutedStyle.Render("  "+opt.desc)
	}
	return label + "\n    " + styles.MutedStyle.Render(strings.ReplaceAll(ansi.Wrap(opt.desc, max(1, width-4), ""), "\n", "\n    "))
}

func (d *settingsDialog) renderSelectorRow(row int, label, valueLabel string, width int) string {
	labelStyle, valueStyle, prefix := styles.PaletteUnselectedActionStyle, styles.SecondaryStyle, "  "
	if d.selected[d.tab] == row {
		labelStyle, valueStyle, prefix = styles.PaletteSelectedActionStyle, styles.HighlightWhiteStyle, styles.HighlightWhiteStyle.Render("› ")
	}
	left := prefix + labelStyle.Render(label)
	value := valueStyle.Render("‹ " + valueLabel + " ›")
	if lipgloss.Width(left)+lipgloss.Width(value)+1 <= width {
		return left + strings.Repeat(" ", width-lipgloss.Width(left)-lipgloss.Width(value)) + value
	}
	return ansi.Wrap(left, width, "") + "\n  " + strings.ReplaceAll(ansi.Wrap(value, max(1, width-2), ""), "\n", "\n  ")
}

func (d *settingsDialog) renderStepperRow(row int, label string, value int, unit string, width int, disabled bool) string {
	result := d.renderSelectorRow(row, label, fmt.Sprintf("%d %s", value, unit), width)
	if disabled {
		return styles.MutedStyle.Render(ansi.Strip(result))
	}
	return result
}

func (d *settingsDialog) renderToggleRow(row int, label string, enabled bool, width int) string {
	check := "[ ]"
	if enabled {
		check = "[x]"
	}
	labelStyle, checkStyle, prefix := styles.PaletteUnselectedActionStyle, styles.SecondaryStyle, "  "
	if d.selected[d.tab] == row {
		labelStyle, checkStyle, prefix = styles.PaletteSelectedActionStyle, styles.HighlightWhiteStyle, styles.HighlightWhiteStyle.Render("› ")
	}
	if enabled {
		checkStyle = checkStyle.Foreground(styles.Success)
	}
	label = strings.ReplaceAll(ansi.Wrap(labelStyle.Render(label), max(1, width-6), ""), "\n", "\n      ")
	return prefix + checkStyle.Render(check) + " " + label
}

func (d *settingsDialog) renderNestedToggleRow(row int, label string, enabled, disabled bool, width int) string {
	result := "  " + strings.ReplaceAll(d.renderToggleRow(row, label, enabled, width-2), "\n", "\n  ")
	if disabled {
		return styles.MutedStyle.Render(ansi.Strip(result))
	}
	return result
}

// visibleSectionLabels returns the sidebar section labels that are visible
// under the given settings. The session block is always shown; its label
// reads "session/path" while the session path is visible and "session" once
// it is hidden.
func visibleSectionLabels(s messages.LayoutSettings) []string {
	sessionLabel := "session/path"
	if s.HideSessionPath {
		sessionLabel = "session"
	}
	labels := []string{sessionLabel}
	if !s.HideUsage {
		labels = append(labels, "usage")
	}
	if !s.HideAgents {
		labels = append(labels, "agents")
	}
	if !s.HideTools {
		labels = append(labels, "tools")
	}
	if !s.HideTodos {
		labels = append(labels, "todos")
	}
	if s.ShowPlans {
		labels = append(labels, "plans")
	}
	return labels
}

// renderLayoutPreview draws a small schematic of the resulting layout: the
// chat box, the input box, and the sidebar (side column or horizontal band)
// listing only the visible sections. maxWidth caps the schematic width.
func renderLayoutPreview(s messages.LayoutSettings, maxWidth int) string {
	width := max(previewMinWidth, min(previewMaxWidth, maxWidth))
	switch messages.ParseSidebarPosition(string(s.SidebarPosition)) {
	case messages.SidebarLeft:
		return renderSidePreview(s, width, true)
	case messages.SidebarTop:
		return renderBandPreview(s, width, false)
	case messages.SidebarBottom:
		return renderBandPreview(s, width, true)
	default:
		return renderSidePreview(s, width, false)
	}
}

// borderStyle renders preview borders.
var previewBorder = func(text string) string { return styles.FadingStyle.Render(text) }

// previewLabelCell renders a label padded to width inside a preview box cell.
func previewLabelCell(label string, width int, style lipgloss.Style) string {
	label = toolcommon.TruncateText(label, max(0, width-1))
	cell := " " + style.Render(label)
	return cell + strings.Repeat(" ", max(0, width-lipgloss.Width(cell)))
}

// previewCenteredCell renders a label centered within width.
func previewCenteredCell(label string, width int, style lipgloss.Style) string {
	return lipgloss.PlaceHorizontal(width, lipgloss.Center, style.Render(label))
}

// renderSidePreview draws the vertical layout with the sidebar on the left or right.
func renderSidePreview(s messages.LayoutSettings, width int, onLeft bool) string {
	inner := width - 2
	sideW := max(9, inner/3)
	chatW := inner - sideW - 1
	labels := visibleSectionLabels(s)
	contentRows := max(5, len(labels))
	sectionStyle := styles.TabAccentStyle

	var lines []string
	hbarChat := strings.Repeat("─", chatW)
	hbarSide := strings.Repeat("─", sideW)
	hbarFull := strings.Repeat("─", inner)

	if onLeft {
		lines = append(lines, previewBorder("╭"+hbarSide+"┬"+hbarChat+"╮"))
	} else {
		lines = append(lines, previewBorder("╭"+hbarChat+"┬"+hbarSide+"╮"))
	}

	for i := range contentRows {
		var side, chat string
		if i < len(labels) {
			side = previewLabelCell(labels[i], sideW, sectionStyle)
		} else {
			side = strings.Repeat(" ", sideW)
		}
		if i == contentRows/2 {
			chat = previewCenteredCell("chat", chatW, styles.SecondaryStyle)
		} else {
			chat = strings.Repeat(" ", chatW)
		}

		v := previewBorder("│")
		if onLeft {
			lines = append(lines, v+side+v+chat+v)
		} else {
			lines = append(lines, v+chat+v+side+v)
		}
	}

	if onLeft {
		lines = append(lines, previewBorder("├"+hbarSide+"┴"+hbarChat+"┤"))
	} else {
		lines = append(lines, previewBorder("├"+hbarChat+"┴"+hbarSide+"┤"))
	}
	lines = append(lines,
		previewBorder("│")+previewLabelCell("input", inner, styles.SecondaryStyle)+previewBorder("│"),
		previewBorder("╰"+hbarFull+"╯"),
	)

	return strings.Join(lines, "\n")
}

// renderBandPreview draws the layout with the sidebar as a horizontal band
// above (bottom=false) or below (bottom=true) the chat.
func renderBandPreview(s messages.LayoutSettings, width int, bottom bool) string {
	inner := width - 2
	const chatRows = 3

	band := previewLabelCell(strings.Join(visibleSectionLabels(s), " · "), inner, styles.TabAccentStyle)
	hbarFull := strings.Repeat("─", inner)
	v := previewBorder("│")

	chatLines := make([]string, 0, chatRows)
	for i := range chatRows {
		if i == chatRows/2 {
			chatLines = append(chatLines, v+previewCenteredCell("chat", inner, styles.SecondaryStyle)+v)
		} else {
			chatLines = append(chatLines, v+strings.Repeat(" ", inner)+v)
		}
	}

	lines := []string{previewBorder("╭" + hbarFull + "╮")}
	if bottom {
		lines = append(lines, chatLines...)
		lines = append(lines, previewBorder("├"+hbarFull+"┤"), v+band+v)
	} else {
		lines = append(lines, v+band+v, previewBorder("├"+hbarFull+"┤"))
		lines = append(lines, chatLines...)
	}
	lines = append(lines,
		previewBorder("├"+hbarFull+"┤"),
		v+previewLabelCell("input", inner, styles.SecondaryStyle)+v,
		previewBorder("╰"+hbarFull+"╯"),
	)

	return strings.Join(lines, "\n")
}
