package notification

import (
	"time"

	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"

	"github.com/docker/cagent/pkg/tui/styles"
)

const (
	defaultDuration     = 3 * time.Second
	notificationPadding = 2
	maxWidth            = 50
)

type ShowMsg struct {
	Text string
}

type HideMsg struct{}

type State int

const (
	StateHidden State = iota
	StateVisible
)

// Notification represents a notification component that displays
// a message in the bottom right corner of the screen
type Notification struct {
	width, height int
	text          string
	state         State
}

func New() Notification {
	return Notification{
		state: StateHidden,
	}
}

func (n *Notification) SetSize(width, height int) {
	n.width = width
	n.height = height
}

func (n *Notification) Update(msg tea.Msg) (Notification, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		n.width = msg.Width
		n.height = msg.Height
		return *n, nil

	case ShowMsg:
		n.text = msg.Text
		n.state = StateVisible
		return *n, tea.Tick(defaultDuration, func(t time.Time) tea.Msg {
			return HideMsg{}
		})

	case HideMsg:
		n.state = StateHidden
		n.text = ""
		return *n, nil
	}

	return *n, nil
}

func (n *Notification) View() string {
	if n.state == StateHidden || n.text == "" {
		return ""
	}

	return styles.
		NotificationStyle.
		MaxWidth(maxWidth).
		Render(n.text)
}

func (n *Notification) GetLayer() *lipgloss.Layer {
	if n.state == StateHidden || n.text == "" {
		return nil
	}

	view := n.View()
	row, col := n.position()

	return lipgloss.NewLayer(view).X(col).Y(row)
}

func (n *Notification) position() (row, col int) {
	notificationView := n.View()
	viewHeight := lipgloss.Height(notificationView)
	viewWidth := lipgloss.Width(notificationView)

	// Position in bottom right corner with padding
	row = max(0, n.height-viewHeight-notificationPadding)
	col = max(0, n.width-viewWidth-notificationPadding)

	return row, col
}

func (n *Notification) IsVisible() bool {
	return n.state != StateHidden
}
