package dialog

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/tui/components/messages"
	"github.com/docker/cagent/pkg/tui/core"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/service"
	"github.com/docker/cagent/pkg/tui/styles"
	"github.com/docker/cagent/pkg/tui/types"
)

// Layout constants for tool confirmation dialog.
const (
	toolConfirmDialogWidthPercent  = 70 // Dialog width as percentage of screen
	toolConfirmDialogHeightPercent = 80 // Max dialog height as percentage of screen
	toolConfirmMinScrollHeight     = 5  // Minimum height for the scroll view
	toolConfirmEmptyLinesBefore    = 2  // Empty lines before question
	toolConfirmEmptyLinesAfter     = 1  // Empty lines after question
)

type (
	RuntimeResumeMsg struct {
		Request runtime.ResumeRequest
	}
)

// ToolConfirmationResponse represents the user's response to tool confirmation
type ToolConfirmationResponse struct {
	Response string // "approve", "reject", or "approve-session"
}

type toolConfirmationDialog struct {
	BaseDialog
	msg          *runtime.ToolCallConfirmationEvent
	keyMap       toolConfirmationKeyMap
	sessionState *service.SessionState
	scrollView   messages.Model
}

// dialogDimensions returns computed dialog width and content width.
func (d *toolConfirmationDialog) dialogDimensions() (dialogWidth, contentWidth int) {
	dialogWidth = d.Width() * toolConfirmDialogWidthPercent / 100
	contentWidth = dialogWidth - styles.DialogStyle.GetHorizontalFrameSize()
	return dialogWidth, contentWidth
}

// SetSize implements [Dialog].
func (d *toolConfirmationDialog) SetSize(width, height int) tea.Cmd {
	d.BaseDialog.SetSize(width, height)

	// Calculate dialog dimensions using helper
	_, contentWidth := d.dialogDimensions()
	maxDialogHeight := height * toolConfirmDialogHeightPercent / 100

	// Measure fixed UI elements using the same rendering as View()
	titleStyle := styles.DialogTitleStyle.Width(contentWidth)
	title := titleStyle.Render("Tool Confirmation")
	titleHeight := lipgloss.Height(title)

	separator := d.renderSeparator(contentWidth)
	separatorHeight := lipgloss.Height(separator)

	question := styles.DialogQuestionStyle.Width(contentWidth).Render("Do you want to allow this tool call?")
	questionHeight := lipgloss.Height(question)

	options := RenderHelpKeys(contentWidth, "Y", "yes", "N", "no", "A", "all (approve all tools this session)")
	optionsHeight := lipgloss.Height(options)

	// Calculate available height for scroll view
	frameHeight := styles.DialogStyle.GetVerticalFrameSize()
	fixedContentHeight := titleHeight + separatorHeight + toolConfirmEmptyLinesBefore + questionHeight + toolConfirmEmptyLinesAfter + optionsHeight
	availableHeight := max(maxDialogHeight-frameHeight-fixedContentHeight, toolConfirmMinScrollHeight)
	d.scrollView.SetSize(contentWidth, availableHeight)

	return nil
}

// renderSeparator renders the separator line consistently.
func (d *toolConfirmationDialog) renderSeparator(contentWidth int) string {
	return RenderSeparator(contentWidth)
}

// toolConfirmationKeyMap defines key bindings for tool confirmation dialog
type toolConfirmationKeyMap struct {
	Yes key.Binding
	No  key.Binding
	All key.Binding
}

// defaultToolConfirmationKeyMap returns default key bindings
func defaultToolConfirmationKeyMap() toolConfirmationKeyMap {
	return toolConfirmationKeyMap{
		Yes: key.NewBinding(
			key.WithKeys("y", "Y"),
			key.WithHelp("Y", "approve"),
		),
		No: key.NewBinding(
			key.WithKeys("n", "N"),
			key.WithHelp("N", "reject"),
		),
		All: key.NewBinding(
			key.WithKeys("a", "A"),
			key.WithHelp("A", "approve all"),
		),
	}
}

// NewToolConfirmationDialog creates a new tool confirmation dialog
func NewToolConfirmationDialog(msg *runtime.ToolCallConfirmationEvent, sessionState *service.SessionState) Dialog {
	// Create scrollable view with minimal initial size (will be updated in SetSize)
	scrollView := messages.NewScrollableView(1, 1, sessionState)

	// Add the tool call message to the view
	scrollView.AddOrUpdateToolCall(
		"", // agentName - empty for dialog context
		msg.ToolCall,
		msg.ToolDefinition,
		types.ToolStatusConfirmation,
	)

	return &toolConfirmationDialog{
		msg:          msg,
		sessionState: sessionState,
		keyMap:       defaultToolConfirmationKeyMap(),
		scrollView:   scrollView,
	}
}

// Init initializes the tool confirmation dialog
func (d *toolConfirmationDialog) Init() tea.Cmd {
	return d.scrollView.Init()
}

// Update handles messages for the tool confirmation dialog
func (d *toolConfirmationDialog) Update(msg tea.Msg) (layout.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		cmd := d.SetSize(msg.Width, msg.Height)
		return d, cmd

	case tea.KeyPressMsg:
		if cmd := HandleQuit(msg); cmd != nil {
			return d, cmd
		}

		switch {
		case key.Matches(msg, d.keyMap.Yes):
			return d, tea.Sequence(
				core.CmdHandler(CloseDialogMsg{}),
				core.CmdHandler(RuntimeResumeMsg{Request: runtime.ResumeApprove()}),
			)
		case key.Matches(msg, d.keyMap.No):
			// Open the rejection reason dialog on top of this dialog
			return d, core.CmdHandler(OpenDialogMsg{
				Model: NewToolRejectionReasonDialog(),
			})
		case key.Matches(msg, d.keyMap.All):
			d.sessionState.SetYoloMode(true)
			return d, tea.Sequence(
				core.CmdHandler(CloseDialogMsg{}),
				core.CmdHandler(RuntimeResumeMsg{Request: runtime.ResumeApproveSession()}),
			)
		}

		// Forward scrolling keys to the scroll view
		if _, isScrollKey := core.GetScrollDirection(msg); isScrollKey {
			updatedScrollView, cmd := d.scrollView.Update(msg)
			d.scrollView = updatedScrollView.(messages.Model)
			return d, cmd
		}

	case tea.MouseWheelMsg:
		// Forward mouse wheel events to scroll view
		updatedScrollView, cmd := d.scrollView.Update(msg)
		d.scrollView = updatedScrollView.(messages.Model)
		return d, cmd
	}

	return d, nil
}

// View renders the tool confirmation dialog
func (d *toolConfirmationDialog) View() string {
	dialogWidth, contentWidth := d.dialogDimensions()

	dialogStyle := styles.DialogStyle.Width(dialogWidth)

	titleStyle := styles.DialogTitleStyle.Width(contentWidth)
	title := titleStyle.Render("Tool Confirmation")

	// Separator
	separator := d.renderSeparator(contentWidth)

	// Get scrollable tool call view
	argumentsSection := d.scrollView.View()

	// Combine all parts with proper spacing
	parts := []string{title, separator}

	if argumentsSection != "" {
		parts = append(parts, "", argumentsSection)
	}

	// Confirmation prompt
	question := styles.DialogQuestionStyle.Width(contentWidth).Render("Do you want to allow this tool call?")
	options := RenderHelpKeys(contentWidth, "Y", "yes", "N", "no", "A", "all (approve all tools this session)")

	parts = append(parts, "", question, "", options)

	content := lipgloss.JoinVertical(lipgloss.Left, parts...)

	return dialogStyle.Render(content)
}

// Position calculates the position to center the dialog
func (d *toolConfirmationDialog) Position() (row, col int) {
	dialogWidth, _ := d.dialogDimensions()
	renderedDialog := d.View()
	dialogHeight := lipgloss.Height(renderedDialog)
	return CenterPosition(d.Width(), d.Height(), dialogWidth, dialogHeight)
}
