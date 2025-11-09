package notification

import (
	"strings"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/docker/cagent/pkg/tui/styles"
)

const (
	defaultDuration      = 3 * time.Second
	notificationPadding  = 2
	maxNotificationWidth = 80 // Maximum width to prevent covering too much screen
)

var nextID atomic.Uint64

// Type represents the type of notification
type Type int

const (
	TypeSuccess Type = iota
	TypeWarning
	TypeInfo
	TypeError
)

type ShowMsg struct {
	Text string
	Type Type // Defaults to TypeSuccess for backward compatibility
}

type HideMsg struct {
	ID uint64 // If 0, hides all notifications (backward compatibility)
}

// notificationItem represents a single notification
type notificationItem struct {
	ID       uint64
	Text     string
	Type     Type
	TimerCmd tea.Cmd
}

// Manager represents a notification manager that displays
// multiple stacked messages in the bottom right corner of the screen
type Manager struct {
	width, height int
	items         []notificationItem
}

func New() Manager {
	return Manager{
		items: make([]notificationItem, 0),
	}
}

func (n *Manager) SetSize(width, height int) {
	n.width = width
	n.height = height
}

func (n *Manager) Update(msg tea.Msg) (Manager, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		n.width = msg.Width
		n.height = msg.Height
		return *n, nil

	case ShowMsg:
		id := nextID.Add(1)
		notifType := msg.Type
		// Auto-detect error type for backward compatibility when Type is not set
		if notifType == TypeSuccess && msg.Text != "" {
			textLower := strings.ToLower(msg.Text)
			if strings.Contains(textLower, "failed") || strings.Contains(textLower, "error") {
				notifType = TypeError
			}
		}
		item := notificationItem{
			ID:   id,
			Text: msg.Text,
			Type: notifType,
		}

		item.TimerCmd = tea.Tick(defaultDuration, func(t time.Time) tea.Msg {
			return HideMsg{ID: id}
		})

		n.items = append([]notificationItem{item}, n.items...)

		return *n, item.TimerCmd

	case HideMsg:
		if msg.ID == 0 {
			n.items = nil
			return *n, nil
		}

		newItems := make([]notificationItem, 0, len(n.items))
		for _, item := range n.items {
			if item.ID != msg.ID {
				newItems = append(newItems, item)
			}
		}
		n.items = newItems
		return *n, nil
	}

	return *n, nil
}

func (n *Manager) View() string {
	if len(n.items) == 0 {
		return ""
	}

	var views []string
	for i := len(n.items) - 1; i >= 0; i-- {
		item := n.items[i]

		// Select style based on notification type
		var style lipgloss.Style
		switch item.Type {
		case TypeError:
			style = styles.NotificationErrorStyle
		case TypeWarning:
			style = styles.NotificationWarningStyle
		case TypeInfo:
			style = styles.NotificationInfoStyle
		default:
			style = styles.NotificationStyle
		}

		// Apply max width constraint and word wrapping
		text := item.Text
		maxWidth := maxNotificationWidth
		if n.width > 0 {
			// Use smaller of maxNotificationWidth or available width minus padding
			maxWidth = min(maxNotificationWidth, n.width-notificationPadding*2)
		}

		// Only constrain width if text actually exceeds maxWidth
		textWidth := lipgloss.Width(text)
		var view string
		if textWidth > maxWidth {
			// Wrap text using lipgloss Width style - lipgloss will automatically wrap
			view = style.Width(maxWidth).Render(text)
		} else {
			// Use natural width for short text
			view = style.Render(text)
		}
		views = append(views, view)
	}

	return lipgloss.JoinVertical(lipgloss.Right, views...)
}

func (n *Manager) GetLayer() *lipgloss.Layer {
	if len(n.items) == 0 {
		return nil
	}

	view := n.View()
	row, col := n.position()

	return lipgloss.NewLayer(view).X(col).Y(row)
}

func (n *Manager) position() (row, col int) {
	notificationView := n.View()
	viewHeight := lipgloss.Height(notificationView)
	viewWidth := lipgloss.Width(notificationView)

	// Position in bottom right corner with padding
	row = max(0, n.height-viewHeight-notificationPadding)
	col = max(0, n.width-viewWidth-notificationPadding)

	return row, col
}

func (n *Manager) Open() bool {
	return len(n.items) > 0
}
