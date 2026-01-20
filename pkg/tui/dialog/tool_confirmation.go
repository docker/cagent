package dialog

import (
	"strings"

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

type (
	RuntimeResumeMsg struct {
		Response runtime.ResumeType
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

// SetSize implements [Dialog].
func (d *toolConfirmationDialog) SetSize(width, height int) tea.Cmd {
	d.BaseDialog.SetSize(width, height)

	// Calculate dialog dimensions
	dialogWidth := width * 70 / 100
	contentWidth := dialogWidth - 6
	maxDialogHeight := (height * 80) / 100

	titleStyle := styles.DialogTitleStyle.Width(contentWidth)
	title := titleStyle.Render("Tool Confirmation")
	titleHeight := lipgloss.Height(title)

	separatorWidth := max(contentWidth-10, 20)
	separator := styles.DialogSeparatorStyle.
		Align(lipgloss.Center).
		Width(contentWidth).
		Render(strings.Repeat("─", separatorWidth))
	separatorHeight := lipgloss.Height(separator)

	question := styles.DialogQuestionStyle.Width(contentWidth).Render("Do you want to allow this tool call?")
	questionHeight := lipgloss.Height(question)

	options := RenderHelpKeys(contentWidth, "Y", "yes", "N", "no", "A", "all (approve all tools this session)")
	optionsHeight := lipgloss.Height(options)

	// Calculate available height for scroll view
	// Total = maxDialogHeight - title - separator - 2 empty lines - question - empty line - options - 4 (dialog padding/border)
	availableHeight := max(maxDialogHeight-titleHeight-separatorHeight-2-questionHeight-1-optionsHeight-4, 5)
	d.scrollView.SetSize(contentWidth, availableHeight)

	return nil
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
	// Create scrollable view with initial size (will be updated in SetSize)
	scrollView := messages.NewScrollableView(100, 20, sessionState)

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
	undo := d.sessionState.SetInConfirmationDialog()
	defer undo()
	return d.scrollView.Init()
}

// Update handles messages for the tool confirmation dialog
func (d *toolConfirmationDialog) Update(msg tea.Msg) (layout.Model, tea.Cmd) {
	undo := d.sessionState.SetInConfirmationDialog()
	defer undo()
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		cmd := d.SetSize(msg.Width, msg.Height)
		return d, cmd

	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, d.keyMap.Yes):
			return d, tea.Sequence(
				core.CmdHandler(CloseDialogMsg{}),
				core.CmdHandler(RuntimeResumeMsg{Response: runtime.ResumeTypeApprove}),
			)
		case key.Matches(msg, d.keyMap.No):
			return d, tea.Sequence(
				core.CmdHandler(CloseDialogMsg{}),
				core.CmdHandler(RuntimeResumeMsg{Response: runtime.ResumeTypeReject}),
			)
		case key.Matches(msg, d.keyMap.All):
			d.sessionState.SetYoloMode(true)
			return d, tea.Sequence(
				core.CmdHandler(CloseDialogMsg{}),
				core.CmdHandler(RuntimeResumeMsg{Response: runtime.ResumeTypeApproveSession}),
			)
		}

		if cmd := HandleQuit(msg); cmd != nil {
			return d, cmd
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
	undo := d.sessionState.SetInConfirmationDialog()
	defer undo()
	dialogWidth := d.Width() * 70 / 100

	// Content width (accounting for padding and borders)
	contentWidth := dialogWidth - 6

	dialogStyle := styles.DialogStyle.Width(dialogWidth)

	// Title
	titleStyle := styles.DialogTitleStyle.Width(contentWidth)
	title := titleStyle.Render("Tool Confirmation")

	// Separator
	separatorWidth := max(contentWidth-10, 20)
	separator := styles.DialogSeparatorStyle.
		Align(lipgloss.Center).
		Width(contentWidth).
		Render(strings.Repeat("─", separatorWidth))

	// Get scrollable tool call view
	argumentsSection := d.scrollView.View()

	question := styles.DialogQuestionStyle.Width(contentWidth).Render("Do you want to allow this tool call?")
	options := RenderHelpKeys(contentWidth, "Y", "yes", "N", "no", "A", "all (approve all tools this session)")

	// Combine all parts with proper spacing
	parts := []string{title, separator}

	if argumentsSection != "" {
		parts = append(parts, "", argumentsSection)
	}

	parts = append(parts, "", question, "", options)

	content := lipgloss.JoinVertical(lipgloss.Left, parts...)

	return dialogStyle.Render(content)
}

// Position calculates the position to center the dialog
func (d *toolConfirmationDialog) Position() (row, col int) {
	dialogWidth := d.Width() * 70 / 100
	renderedDialog := d.View()
	return CenterPosition(d.Width(), d.Height(), dialogWidth, lipgloss.Height(renderedDialog))
}
