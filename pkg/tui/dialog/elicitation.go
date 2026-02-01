package dialog

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/styles"
)

const (
	defaultCharLimit = 500
	numberCharLimit  = 50
	defaultWidth     = 50
)

// ElicitationField represents a form field extracted from a JSON schema.
type ElicitationField struct {
	Name, Title, Type, Description string
	Required                       bool
	EnumValues                     []string
	Default                        any
	MinLength, MaxLength           int
	Format, Pattern                string
	Minimum, Maximum               float64
	HasMinimum, HasMaximum         bool
}

// ElicitationDialog implements Dialog for MCP elicitation requests.
type ElicitationDialog struct {
	BaseDialog
	message      string
	fields       []ElicitationField
	inputs       []textinput.Model
	boolValues   map[int]bool
	enumIndexes  map[int]int // selected index for enum fields
	currentField int
	keyMap       elicitationKeyMap
	fieldErrors  map[int]string // validation error messages per field
}

type elicitationKeyMap struct {
	Up, Down, Enter, Escape, Space key.Binding
}

// NewElicitationDialog creates a new elicitation dialog.
func NewElicitationDialog(message string, schema any, _ map[string]any) Dialog {
	fields := parseElicitationSchema(schema)
	d := &ElicitationDialog{
		message:     message,
		fields:      fields,
		inputs:      make([]textinput.Model, len(fields)),
		boolValues:  make(map[int]bool),
		enumIndexes: make(map[int]int),
		fieldErrors: make(map[int]string),
		keyMap: elicitationKeyMap{
			Up:     key.NewBinding(key.WithKeys("up", "shift+tab")),
			Down:   key.NewBinding(key.WithKeys("down", "tab")),
			Enter:  key.NewBinding(key.WithKeys("enter")),
			Escape: key.NewBinding(key.WithKeys("esc")),
			Space:  key.NewBinding(key.WithKeys("space")),
		},
	}
	d.initInputs()
	return d
}

func (d *ElicitationDialog) Init() tea.Cmd {
	if len(d.inputs) > 0 {
		return textinput.Blink
	}
	return nil
}

func (d *ElicitationDialog) Update(msg tea.Msg) (layout.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		cmd := d.SetSize(msg.Width, msg.Height)
		return d, cmd
	case tea.PasteMsg:
		// Forward paste to text input if current field uses one
		if d.isTextInputField() {
			var cmd tea.Cmd
			d.inputs[d.currentField], cmd = d.inputs[d.currentField].Update(msg)
			return d, cmd
		}
		return d, nil
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			cmd := d.close(tools.ElicitationActionDecline, nil)
			return d, tea.Sequence(cmd, tea.Quit)
		}
		return d.handleKeyPress(msg)
	}
	return d, nil
}

func (d *ElicitationDialog) handleKeyPress(msg tea.KeyPressMsg) (layout.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, d.keyMap.Space) && !d.isTextInputField():
		// Only handle space for boolean/enum fields; let it pass through to text input otherwise
		d.toggleCurrentSelection()
		return d, nil
	case key.Matches(msg, d.keyMap.Escape):
		cmd := d.close(tools.ElicitationActionCancel, nil)
		return d, cmd
	case key.Matches(msg, d.keyMap.Up):
		d.moveFocus(-1)
		return d, nil
	case key.Matches(msg, d.keyMap.Down):
		d.moveFocus(1)
		return d, nil
	case key.Matches(msg, d.keyMap.Enter):
		return d.submit()
	default:
		return d.updateCurrentInput(msg)
	}
}

// toggleCurrentSelection toggles boolean or cycles enum for the current field.
func (d *ElicitationDialog) toggleCurrentSelection() {
	// Clear error when user interacts with the field
	delete(d.fieldErrors, d.currentField)

	switch d.currentFieldType() {
	case "boolean":
		d.boolValues[d.currentField] = !d.boolValues[d.currentField]
	case "enum":
		field := d.fields[d.currentField]
		d.enumIndexes[d.currentField] = (d.enumIndexes[d.currentField] + 1) % len(field.EnumValues)
	}
}

func (d *ElicitationDialog) currentFieldType() string {
	if d.currentField < len(d.fields) {
		return d.fields[d.currentField].Type
	}
	return ""
}

func (d *ElicitationDialog) submit() (layout.Model, tea.Cmd) {
	if len(d.fields) == 0 {
		cmd := d.close(tools.ElicitationActionAccept, nil)
		return d, cmd
	}

	// Clear previous errors and validate
	d.fieldErrors = make(map[int]string)
	content, firstErrorIdx := d.collectAndValidate()

	if firstErrorIdx >= 0 {
		// Focus the first field with an error
		d.focusField(firstErrorIdx)
		return d, nil
	}

	cmd := d.close(tools.ElicitationActionAccept, content)
	return d, cmd
}

func (d *ElicitationDialog) updateCurrentInput(msg tea.KeyPressMsg) (layout.Model, tea.Cmd) {
	// Only text-based fields (not boolean/enum) use the text input
	if d.isTextInputField() {
		// Clear error for current field when user types
		delete(d.fieldErrors, d.currentField)
		var cmd tea.Cmd
		d.inputs[d.currentField], cmd = d.inputs[d.currentField].Update(msg)
		return d, cmd
	}
	return d, nil
}

func (d *ElicitationDialog) moveFocus(delta int) {
	if len(d.fields) == 0 {
		return
	}
	newField := (d.currentField + delta + len(d.fields)) % len(d.fields)
	d.focusField(newField)
}

// focusField moves focus to the specified field index.
func (d *ElicitationDialog) focusField(idx int) {
	if idx < 0 || idx >= len(d.fields) {
		return
	}
	if len(d.inputs) > 0 && d.currentField < len(d.inputs) {
		d.inputs[d.currentField].Blur()
	}
	d.currentField = idx
	// Only focus text input for fields that use it
	if d.isTextInputField() {
		d.inputs[d.currentField].Focus()
	}
}

// isTextInputField returns true if the current field uses a text input (not boolean/enum).
func (d *ElicitationDialog) isTextInputField() bool {
	if d.currentField >= len(d.fields) || len(d.inputs) == 0 {
		return false
	}
	ft := d.fields[d.currentField].Type
	return ft != "boolean" && ft != "enum"
}

func (d *ElicitationDialog) close(action tools.ElicitationAction, content map[string]any) tea.Cmd {
	return CloseWithElicitationResponse(action, content)
}

// collectAndValidate validates all fields and returns the collected values.
// Returns the content map and the index of the first field with an error (-1 if valid).
func (d *ElicitationDialog) collectAndValidate() (map[string]any, int) {
	content := make(map[string]any)
	firstErrorIdx := -1

	for i, field := range d.fields {
		switch field.Type {
		case "boolean":
			content[field.Name] = d.boolValues[i]
		case "enum":
			idx := d.enumIndexes[i]
			if idx < 0 || idx >= len(field.EnumValues) {
				if field.Required {
					d.fieldErrors[i] = "Selection required"
					if firstErrorIdx < 0 {
						firstErrorIdx = i
					}
				}
				continue
			}
			content[field.Name] = field.EnumValues[idx]
		default:
			val := strings.TrimSpace(d.inputs[i].Value())
			if val == "" {
				if field.Required {
					d.fieldErrors[i] = "This field is required"
					if firstErrorIdx < 0 {
						firstErrorIdx = i
					}
				}
				continue
			}
			parsed, errMsg := d.parseAndValidateField(val, field)
			if errMsg != "" {
				d.fieldErrors[i] = errMsg
				if firstErrorIdx < 0 {
					firstErrorIdx = i
				}
				continue
			}
			content[field.Name] = parsed
		}
	}
	return content, firstErrorIdx
}

// parseAndValidateField parses and validates a field value, returning the parsed value and an error message.
func (d *ElicitationDialog) parseAndValidateField(val string, field ElicitationField) (any, string) {
	if val == "" {
		return nil, ""
	}

	switch field.Type {
	case "number":
		f, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return nil, "Must be a valid number"
		}
		if errMsg := validateNumberFieldWithMessage(f, field); errMsg != "" {
			return nil, errMsg
		}
		return f, ""

	case "integer":
		n, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			return nil, "Must be a whole number"
		}
		if errMsg := validateNumberFieldWithMessage(float64(n), field); errMsg != "" {
			return nil, errMsg
		}
		return n, ""

	case "enum":
		if !slices.Contains(field.EnumValues, val) {
			return nil, "Invalid selection"
		}
		return val, ""

	default: // string
		if errMsg := validateStringFieldWithMessage(val, field); errMsg != "" {
			return nil, errMsg
		}
		return val, ""
	}
}

func (d *ElicitationDialog) View() string {
	dialogWidth := d.ComputeDialogWidth(70, 60, 90)
	contentWidth := d.ContentWidth(dialogWidth, 2)

	content := NewContent(contentWidth)
	content.AddTitle("MCP Server Request")
	content.AddSeparator()
	content.AddContent(styles.DialogContentStyle.Width(contentWidth).Render(d.message))

	if len(d.fields) > 0 {
		content.AddSeparator()
		for i, field := range d.fields {
			d.renderField(content, i, field, contentWidth)
			if i < len(d.fields)-1 {
				content.AddSpace()
			}
		}
	}

	content.AddSpace()
	if len(d.fields) > 0 {
		if d.hasSelectionFields() {
			content.AddHelpKeys("↑/↓", "navigate", "space", "change", "enter", "submit", "esc", "cancel")
		} else {
			content.AddHelpKeys("↑/↓", "navigate", "enter", "submit", "esc", "cancel")
		}
	} else {
		content.AddHelpKeys("enter", "confirm", "esc", "cancel")
	}

	return styles.DialogStyle.Width(dialogWidth).Render(content.Build())
}

// hasSelectionFields returns true if any field uses selection-based input (boolean or enum).
func (d *ElicitationDialog) hasSelectionFields() bool {
	for _, field := range d.fields {
		if field.Type == "boolean" || field.Type == "enum" {
			return true
		}
	}
	return false
}

func (d *ElicitationDialog) renderField(content *Content, i int, field ElicitationField, contentWidth int) {
	// Use Title if available, otherwise capitalize the property name
	label := field.Title
	if label == "" {
		label = capitalizeFirst(field.Name)
	}
	if field.Required {
		label += "*"
	}

	// Check if this field has an error
	hasError := d.fieldErrors[i] != ""
	labelStyle := styles.DialogContentStyle.Bold(true)
	if hasError {
		labelStyle = labelStyle.Foreground(styles.Error)
	}
	content.AddContent(labelStyle.Render(label))

	// Render field input based on type
	isFocused := i == d.currentField
	switch field.Type {
	case "boolean":
		d.renderBooleanField(content, i, isFocused)
	case "enum":
		d.renderEnumField(content, i, field, isFocused)
	default:
		d.inputs[i].SetWidth(contentWidth)
		content.AddContent(d.inputs[i].View())
	}

	// Show error message if present
	if hasError {
		errorStyle := styles.DialogContentStyle.Foreground(styles.Error).Italic(true)
		content.AddContent(errorStyle.Render("  ⚠ " + d.fieldErrors[i]))
	}
}

func (d *ElicitationDialog) renderBooleanField(content *Content, i int, isFocused bool) {
	selectedIdx := 1
	if d.boolValues[i] {
		selectedIdx = 0
	}
	d.renderSelectionField(content, []string{"Yes", "No"}, selectedIdx, isFocused)
}

func (d *ElicitationDialog) renderEnumField(content *Content, i int, field ElicitationField, isFocused bool) {
	d.renderSelectionField(content, field.EnumValues, d.enumIndexes[i], isFocused)
}

func (d *ElicitationDialog) renderSelectionField(content *Content, options []string, selectedIdx int, isFocused bool) {
	selectedStyle := styles.DialogContentStyle.Foreground(styles.White).Bold(true)
	unselectedStyle := styles.DialogContentStyle.Foreground(styles.TextMuted)

	for j, option := range options {
		prefix := "  ○ "
		style := unselectedStyle
		if j == selectedIdx {
			prefix = "  ● "
			if isFocused {
				prefix = "› ● "
			}
			style = selectedStyle
		}
		content.AddContent(style.Render(prefix + option))
	}
}

// capitalizeFirst returns the string with its first letter capitalized.
func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

func (d *ElicitationDialog) Position() (row, col int) {
	return d.CenterDialog(d.View())
}

// --- Input initialization ---

func (d *ElicitationDialog) initInputs() {
	for i, field := range d.fields {
		d.inputs[i] = d.createInput(field, i)
	}
	// Focus the first text input field
	if d.isTextInputField() {
		d.inputs[0].Focus()
	}
}

func (d *ElicitationDialog) createInput(field ElicitationField, idx int) textinput.Model {
	ti := textinput.New()
	ti.SetStyles(styles.DialogInputStyle)
	ti.SetWidth(defaultWidth)
	ti.Prompt = "" // Remove the "> " prefix

	// Configure based on field type
	switch field.Type {
	case "boolean":
		d.boolValues[idx], _ = field.Default.(bool)
		return ti // Boolean fields don't use text input

	case "enum":
		// Initialize enum selection to first option
		d.enumIndexes[idx] = 0
		return ti // Enum fields don't use text input

	case "number", "integer":
		ti.Placeholder = cmp.Or(field.Description, "Enter a number")
		ti.CharLimit = numberCharLimit

	default: // string
		ti.Placeholder = cmp.Or(field.Description, "Enter value")
		ti.CharLimit = cmp.Or(field.MaxLength, defaultCharLimit)
	}

	// Set default value
	if field.Default != nil {
		ti.SetValue(fmt.Sprintf("%v", field.Default))
	}

	return ti
}
