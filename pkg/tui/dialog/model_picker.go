package dialog

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/tui/components/scrollview"
	"github.com/docker/cagent/pkg/tui/components/toolcommon"
	"github.com/docker/cagent/pkg/tui/core"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/messages"
	"github.com/docker/cagent/pkg/tui/styles"
)

// SupportedProviders lists the valid provider names that can be used in custom model specs.
// This includes both core providers and aliases.
var SupportedProviders = []string{
	// Core providers
	"openai", "anthropic", "google", "dmr",
	// Aliases (these map to core providers with different defaults)
	"requesty", "azure", "xai", "nebius", "mistral", "ollama",
}

// modelPickerDialog is a dialog for selecting a model for the current agent.
type modelPickerDialog struct {
	BaseDialog
	textInput  textinput.Model
	models     []runtime.ModelChoice
	filtered   []runtime.ModelChoice
	selected   int
	keyMap     commandPaletteKeyMap
	errMsg     string // validation error message
	scrollview *scrollview.Model

	// Double-click detection
	lastClickTime  time.Time
	lastClickIndex int
}

// NewModelPickerDialog creates a new model picker dialog.
func NewModelPickerDialog(models []runtime.ModelChoice) Dialog {
	ti := textinput.New()
	ti.Placeholder = "Type to search or enter custom model (provider/model)…"
	ti.Focus()
	ti.CharLimit = 100
	ti.SetWidth(50)

	// Sort models: config first, then catalog, then custom. Within each section: current first, then default, then alphabetically
	sortedModels := make([]runtime.ModelChoice, len(models))
	copy(sortedModels, models)
	sort.Slice(sortedModels, func(i, j int) bool {
		// Get section priority: config (0) < catalog (1) < custom (2)
		getPriority := func(m runtime.ModelChoice) int {
			if m.IsCustom {
				return 2
			}
			if m.IsCatalog {
				return 1
			}
			return 0
		}
		pi, pj := getPriority(sortedModels[i]), getPriority(sortedModels[j])
		if pi != pj {
			return pi < pj
		}
		// Within each section: current model first
		if sortedModels[i].IsCurrent != sortedModels[j].IsCurrent {
			return sortedModels[i].IsCurrent
		}
		// Then default model
		if sortedModels[i].IsDefault != sortedModels[j].IsDefault {
			return sortedModels[i].IsDefault
		}
		// Then alphabetically by name
		return sortedModels[i].Name < sortedModels[j].Name
	})

	d := &modelPickerDialog{
		textInput:  ti,
		models:     sortedModels,
		keyMap:     defaultCommandPaletteKeyMap(),
		scrollview: scrollview.New(scrollview.WithReserveScrollbarSpace(true)),
	}
	d.filterModels()
	return d
}

func (d *modelPickerDialog) Init() tea.Cmd {
	return textinput.Blink
}

func (d *modelPickerDialog) Update(msg tea.Msg) (layout.Model, tea.Cmd) {
	// Scrollview handles mouse scrollbar, wheel, and pgup/pgdn/home/end
	if handled, cmd := d.scrollview.Update(msg); handled {
		return d, cmd
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		cmd := d.SetSize(msg.Width, msg.Height)
		return d, cmd

	case tea.PasteMsg:
		var cmd tea.Cmd
		d.textInput, cmd = d.textInput.Update(msg)
		d.filterModels()
		d.errMsg = ""
		return d, cmd

	case tea.MouseClickMsg:
		// Scrollbar clicks handled above; this handles list item clicks
		if msg.Button == tea.MouseLeft {
			if modelIdx := d.mouseYToModelIndex(msg.Y); modelIdx >= 0 {
				now := time.Now()
				if modelIdx == d.lastClickIndex && now.Sub(d.lastClickTime) < styles.DoubleClickThreshold {
					d.selected = modelIdx
					d.lastClickTime = time.Time{}
					cmd := d.handleSelection()
					return d, cmd
				}
				d.selected = modelIdx
				d.lastClickTime = now
				d.lastClickIndex = modelIdx
			}
		}
		return d, nil

	case tea.KeyPressMsg:
		if cmd := HandleQuit(msg); cmd != nil {
			return d, cmd
		}

		switch {
		case key.Matches(msg, d.keyMap.Escape):
			return d, core.CmdHandler(CloseDialogMsg{})

		case key.Matches(msg, d.keyMap.Up):
			if d.selected > 0 {
				d.selected--
				d.scrollview.EnsureLineVisible(d.findSelectedLine(nil))
			}
			return d, nil

		case key.Matches(msg, d.keyMap.Down):
			if d.selected < len(d.filtered)-1 {
				d.selected++
				d.scrollview.EnsureLineVisible(d.findSelectedLine(nil))
			}
			return d, nil

		case key.Matches(msg, d.keyMap.Enter):
			cmd := d.handleSelection()
			return d, cmd

		default:
			var cmd tea.Cmd
			d.textInput, cmd = d.textInput.Update(msg)
			d.filterModels()
			d.errMsg = ""
			return d, cmd
		}
	}

	return d, nil
}

// mouseYToModelIndex converts a mouse Y position to a model index.
// Returns -1 if the position is not on a model (e.g., on a separator or outside the list).
func (d *modelPickerDialog) mouseYToModelIndex(y int) int {
	dialogRow, _ := d.Position()
	maxItems := d.scrollview.VisibleHeight()

	listStartY := dialogRow + pickerListStartOffset
	listEndY := listStartY + maxItems

	// Check if Y is within the model list area
	if y < listStartY || y >= listEndY {
		return -1
	}

	// Calculate which line in the visible area was clicked
	lineInView := y - listStartY
	scrollOffset := d.scrollview.ScrollOffset()

	// Calculate the actual line index in allModelLines
	actualLine := scrollOffset + lineInView

	// Now we need to map the line back to a model index, accounting for separators
	return d.lineToModelIndex(actualLine)
}

// lineToModelIndex converts a line index (in allModelLines) to a model index.
// Returns -1 if the line is a separator.
func (d *modelPickerDialog) lineToModelIndex(lineIdx int) int {
	// Pre-compute model type flags (same logic as View)
	hasConfigModels := false
	hasCatalogModels := false
	for _, m := range d.filtered {
		switch {
		case m.IsCustom:
			// Custom models don't affect separator logic for config/catalog
		case m.IsCatalog:
			hasCatalogModels = true
		default:
			hasConfigModels = true
		}
	}

	// Walk through the models, counting lines including separators
	currentLine := 0
	catalogSeparatorShown := false
	customSeparatorShown := false

	for i, model := range d.filtered {
		// Check if separator would be added before this model
		if model.IsCatalog && !catalogSeparatorShown && !model.IsCustom {
			if hasConfigModels {
				if currentLine == lineIdx {
					return -1 // Clicked on separator
				}
				currentLine++
			}
			catalogSeparatorShown = true
		}

		if model.IsCustom && !customSeparatorShown {
			if hasConfigModels || hasCatalogModels {
				if currentLine == lineIdx {
					return -1 // Clicked on separator
				}
				currentLine++
			}
			customSeparatorShown = true
		}

		if currentLine == lineIdx {
			return i // Found the model at this line
		}
		currentLine++
	}

	return -1 // Line index out of range
}

func (d *modelPickerDialog) handleSelection() tea.Cmd {
	query := strings.TrimSpace(d.textInput.Value())

	// If user typed something that looks like a custom model (contains /), validate and use it
	if strings.Contains(query, "/") {
		if err := validateCustomModelSpec(query); err != nil {
			d.errMsg = err.Error()
			return nil
		}
		return tea.Sequence(
			core.CmdHandler(CloseDialogMsg{}),
			core.CmdHandler(messages.ChangeModelMsg{ModelRef: query}),
		)
	}

	// Otherwise, use the selected item from the filtered list
	if d.selected >= 0 && d.selected < len(d.filtered) {
		selected := d.filtered[d.selected]
		// If selecting the default model, send empty ref to clear the override
		modelRef := selected.Ref
		if selected.IsDefault {
			modelRef = ""
		}
		return tea.Sequence(
			core.CmdHandler(CloseDialogMsg{}),
			core.CmdHandler(messages.ChangeModelMsg{ModelRef: modelRef}),
		)
	}

	return nil
}

// validateCustomModelSpec validates a custom model specification entered by the user.
// It checks that each provider/model pair is properly formatted and uses a supported provider.
func validateCustomModelSpec(spec string) error {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil
	}

	// Handle alloy specs (comma-separated)
	parts := strings.SplitSeq(spec, ",")
	for part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		providerName, modelName, ok := strings.Cut(part, "/")
		if !ok {
			return fmt.Errorf("invalid format: expected 'provider/model'")
		}

		providerName = strings.TrimSpace(providerName)
		modelName = strings.TrimSpace(modelName)

		if providerName == "" {
			return fmt.Errorf("provider name cannot be empty (got '/%s')", modelName)
		}
		if modelName == "" {
			return fmt.Errorf("model name cannot be empty (got '%s/')", providerName)
		}

		if !isValidProvider(providerName) {
			return fmt.Errorf("unknown provider '%s'. Supported: %s",
				providerName, strings.Join(SupportedProviders, ", "))
		}
	}

	return nil
}

// isValidProvider checks if the provider name is in the list of supported providers.
func isValidProvider(name string) bool {
	for _, p := range SupportedProviders {
		if strings.EqualFold(p, name) {
			return true
		}
	}
	return false
}

func (d *modelPickerDialog) filterModels() {
	query := strings.ToLower(strings.TrimSpace(d.textInput.Value()))

	// If query contains "/", show "Custom" option as well as matches
	isCustomQuery := strings.Contains(query, "/")

	d.filtered = nil
	for _, model := range d.models {
		if query == "" {
			d.filtered = append(d.filtered, model)
			continue
		}

		// Match against name, provider, and model
		searchText := strings.ToLower(model.Name + " " + model.Provider + " " + model.Model)
		if strings.Contains(searchText, query) {
			d.filtered = append(d.filtered, model)
		}
	}

	// If query looks like a custom model spec and we have no exact match, show it as an option
	if isCustomQuery && len(d.filtered) == 0 {
		d.filtered = append(d.filtered, runtime.ModelChoice{
			Name: "Custom: " + query,
			Ref:  query,
		})
	}

	if d.selected >= len(d.filtered) {
		d.selected = max(0, len(d.filtered)-1)
	}
	// Reset scroll when filtering
	d.scrollview.SetScrollOffset(0)
}

// Model picker dialog dimension constants
const (
	// pickerWidthPercent is the percentage of screen width to use for the dialog
	pickerWidthPercent = 80
	// pickerMinWidth is the minimum width of the dialog
	pickerMinWidth = 50
	// pickerMaxWidth is the maximum width of the dialog
	pickerMaxWidth = 100
	// pickerHeightPercent is the percentage of screen height to use for the dialog
	pickerHeightPercent = 70
	// pickerMaxHeight is the maximum height of the dialog
	pickerMaxHeight = 150

	// pickerDialogPadding is the horizontal padding inside the dialog border (2 on each side + border)
	pickerDialogPadding = 6

	// pickerListVerticalOverhead is the number of rows used by dialog chrome:
	// title(1) + space(1) + input(1) + separator(1) + space at bottom(1) + help keys(1) + borders/padding(2) = 8
	pickerListVerticalOverhead = 8

	// pickerListStartOffset is the Y offset from dialog top to where the model list starts:
	// border(1) + padding(1) + title(1) + space(1) + input(1) + separator(1) = 6
	pickerListStartOffset = 6

	// catalogSeparatorLabel is the text for the catalog section separator
	catalogSeparatorLabel = "── Other models "
	// customSeparatorLabel is the text for the custom models section separator
	customSeparatorLabel = "── Custom models "
)

func (d *modelPickerDialog) dialogSize() (dialogWidth, maxHeight, contentWidth int) {
	dialogWidth = max(min(d.Width()*pickerWidthPercent/100, pickerMaxWidth), pickerMinWidth)
	maxHeight = min(d.Height()*pickerHeightPercent/100, pickerMaxHeight)
	contentWidth = dialogWidth - pickerDialogPadding - d.scrollview.ReservedCols()
	return dialogWidth, maxHeight, contentWidth
}

// SetSize sets the dialog dimensions and configures the scrollview.
func (d *modelPickerDialog) SetSize(width, height int) tea.Cmd {
	cmd := d.BaseDialog.SetSize(width, height)
	_, maxHeight, contentWidth := d.dialogSize()
	regionWidth := contentWidth + d.scrollview.ReservedCols()
	visLines := max(1, maxHeight-pickerListVerticalOverhead)
	d.scrollview.SetSize(regionWidth, visLines)
	return cmd
}

func (d *modelPickerDialog) View() string {
	dialogWidth, _, contentWidth := d.dialogSize()

	d.textInput.SetWidth(contentWidth)

	// Build all model lines first to calculate total height
	var allModelLines []string
	catalogSeparatorShown := false
	customSeparatorShown := false

	// Pre-compute if we have different model types to decide on separators
	hasConfigModels := false
	hasCatalogModels := false
	for _, m := range d.filtered {
		switch {
		case m.IsCustom:
			// Custom models don't affect separator logic for config/catalog
		case m.IsCatalog:
			hasCatalogModels = true
		default:
			hasConfigModels = true
		}
	}

	for i, model := range d.filtered {
		// Add separator before first catalog model (if there are config models anywhere in the list)
		if model.IsCatalog && !catalogSeparatorShown && !model.IsCustom {
			if hasConfigModels {
				separatorLine := styles.MutedStyle.Render(catalogSeparatorLabel + strings.Repeat("─", max(0, contentWidth-len(catalogSeparatorLabel)-2)))
				allModelLines = append(allModelLines, separatorLine)
			}
			catalogSeparatorShown = true
		}

		// Add separator before first custom model (if there are other models anywhere in the list)
		if model.IsCustom && !customSeparatorShown {
			if hasConfigModels || hasCatalogModels {
				separatorLine := styles.MutedStyle.Render(customSeparatorLabel + strings.Repeat("─", max(0, contentWidth-len(customSeparatorLabel)-2)))
				allModelLines = append(allModelLines, separatorLine)
			}
			customSeparatorShown = true
		}

		allModelLines = append(allModelLines, d.renderModel(model, i == d.selected, contentWidth))
	}

	regionWidth := contentWidth + d.scrollview.ReservedCols()

	// Set scrollview position for mouse hit-testing (auto-computed from dialog position)
	dialogRow, dialogCol := d.Position()
	d.scrollview.SetPosition(dialogCol+3, dialogRow+pickerListStartOffset)

	d.scrollview.SetContent(allModelLines, len(allModelLines))

	var scrollableContent string
	if len(d.filtered) == 0 {
		visLines := d.scrollview.VisibleHeight()
		emptyLines := []string{"", styles.DialogContentStyle.
			Italic(true).Align(lipgloss.Center).Width(contentWidth).
			Render("No models found")}
		for len(emptyLines) < visLines {
			emptyLines = append(emptyLines, "")
		}
		scrollableContent = d.scrollview.ViewWithLines(emptyLines)
	} else {
		scrollableContent = d.scrollview.View()
	}

	contentBuilder := NewContent(regionWidth).
		AddTitle("Select Model").
		AddSpace().
		AddContent(d.textInput.View())

	// Show error message if present
	if d.errMsg != "" {
		contentBuilder.AddContent(styles.ErrorStyle.Render("⚠ " + d.errMsg))
	}

	content := contentBuilder.
		AddSeparator().
		AddContent(scrollableContent).
		AddSpace().
		AddHelpKeys("↑/↓", "navigate", "enter", "select", "esc", "cancel").
		Build()

	return styles.DialogStyle.Width(dialogWidth).Render(content)
}

// findSelectedLine returns the line index in allModelLines that corresponds to the selected model.
// This accounts for separator lines that are inserted before catalog and custom sections.
func (d *modelPickerDialog) findSelectedLine(allModelLines []string) int {
	if d.selected < 0 || d.selected >= len(d.filtered) {
		return 0
	}

	// Pre-compute model type flags (same logic as View)
	hasConfigModels := false
	hasCatalogModels := false
	for _, m := range d.filtered {
		switch {
		case m.IsCustom:
			// Custom models don't affect separator logic for config/catalog
		case m.IsCatalog:
			hasCatalogModels = true
		default:
			hasConfigModels = true
		}
	}

	// Count lines before the selected model, including separators
	lineIndex := 0
	catalogSeparatorShown := false
	customSeparatorShown := false

	for i := range d.selected + 1 {
		model := d.filtered[i]

		// Check if separator was added before this model
		if model.IsCatalog && !catalogSeparatorShown && !model.IsCustom {
			if hasConfigModels && i <= d.selected {
				lineIndex++ // Count the separator
			}
			catalogSeparatorShown = true
		}

		if model.IsCustom && !customSeparatorShown {
			if (hasConfigModels || hasCatalogModels) && i <= d.selected {
				lineIndex++ // Count the separator
			}
			customSeparatorShown = true
		}

		if i == d.selected {
			return lineIndex
		}
		lineIndex++
	}

	return min(lineIndex, len(allModelLines)-1)
}

func (d *modelPickerDialog) renderModel(model runtime.ModelChoice, selected bool, maxWidth int) string {
	nameStyle, descStyle := styles.PaletteUnselectedActionStyle, styles.PaletteUnselectedDescStyle
	alloyBadgeStyle, defaultBadgeStyle, currentBadgeStyle := styles.BadgeAlloyStyle, styles.BadgeDefaultStyle, styles.BadgeCurrentStyle
	if selected {
		nameStyle, descStyle = styles.PaletteSelectedActionStyle, styles.PaletteSelectedDescStyle
		// Keep badge colors visible on selection background
		alloyBadgeStyle = alloyBadgeStyle.Background(styles.MobyBlue)
		defaultBadgeStyle = defaultBadgeStyle.Background(styles.MobyBlue)
		currentBadgeStyle = currentBadgeStyle.Background(styles.MobyBlue)
	}

	// Check if this is an alloy model (no provider but has comma-separated models)
	isAlloy := model.Provider == "" && strings.Contains(model.Model, ",")

	// Calculate badge widths
	var badgeWidth int
	if isAlloy {
		badgeWidth += lipgloss.Width(" (alloy)")
	}
	if model.IsCurrent {
		badgeWidth += lipgloss.Width(" (current)")
	} else if model.IsDefault {
		badgeWidth += lipgloss.Width(" (default)")
	}

	// Build description
	var desc string
	switch {
	case model.IsCustom:
		// Custom models: name already is provider/model, no need to repeat
	case model.IsCatalog:
		// Catalog models: show provider/model as description (Name is the human-readable name)
		desc = model.Provider + "/" + model.Model
	case model.Provider != "" && model.Model != "":
		desc = model.Provider + "/" + model.Model
	case isAlloy:
		// Alloy model: show the constituent models
		desc = model.Model
	case model.Ref != "" && !strings.Contains(model.Name, model.Ref):
		desc = model.Ref
	}

	// Calculate available width for name and description
	separatorWidth := 0
	if desc != "" {
		separatorWidth = lipgloss.Width(" • ")
	}

	// Maximum width for name (leaving space for badges and description)
	maxNameWidth := maxWidth - badgeWidth
	if desc != "" {
		// Reserve at least some space for description (minimum 10 chars or available)
		minDescWidth := min(10, len(desc))
		maxNameWidth = maxWidth - badgeWidth - separatorWidth - minDescWidth
	}

	// Truncate name if needed
	displayName := model.Name
	if lipgloss.Width(displayName) > maxNameWidth {
		displayName = toolcommon.TruncateText(displayName, maxNameWidth)
	}

	// Build the name with colored badges
	var nameParts []string
	nameParts = append(nameParts, nameStyle.Render(displayName))
	if isAlloy {
		nameParts = append(nameParts, alloyBadgeStyle.Render(" (alloy)"))
	}
	if model.IsCurrent {
		nameParts = append(nameParts, currentBadgeStyle.Render(" (current)"))
	} else if model.IsDefault {
		nameParts = append(nameParts, defaultBadgeStyle.Render(" (default)"))
	}
	name := strings.Join(nameParts, "")

	if desc != "" {
		// Calculate remaining width for description
		nameWidth := lipgloss.Width(name)
		remainingWidth := maxWidth - nameWidth - separatorWidth
		if remainingWidth > 0 {
			truncatedDesc := toolcommon.TruncateText(desc, remainingWidth)
			return name + descStyle.Render(" • "+truncatedDesc)
		}
		// No room for description
		return name
	}
	return name
}

func (d *modelPickerDialog) Position() (row, col int) {
	dialogWidth, maxHeight, _ := d.dialogSize()
	return CenterPosition(d.Width(), d.Height(), dialogWidth, maxHeight)
}
