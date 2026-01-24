package dialog

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/atotto/clipboard"

	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/tui/components/notification"
	"github.com/docker/cagent/pkg/tui/core"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/messages"
	"github.com/docker/cagent/pkg/tui/styles"
)

// sessionBrowserKeyMap defines key bindings for the session browser
type sessionBrowserKeyMap struct {
	Up         key.Binding
	Down       key.Binding
	PageUp     key.Binding
	PageDown   key.Binding
	Enter      key.Binding
	Escape     key.Binding
	Star       key.Binding
	FilterStar key.Binding
	CopyID     key.Binding
}

// defaultSessionBrowserKeyMap returns default key bindings
func defaultSessionBrowserKeyMap() sessionBrowserKeyMap {
	base := defaultCommandPaletteKeyMap()
	return sessionBrowserKeyMap{
		Up:         base.Up,
		Down:       base.Down,
		PageUp:     base.PageUp,
		PageDown:   base.PageDown,
		Enter:      base.Enter,
		Escape:     base.Escape,
		Star:       key.NewBinding(key.WithKeys("s")),
		FilterStar: key.NewBinding(key.WithKeys("f")),
		CopyID:     key.NewBinding(key.WithKeys("c")),
	}
}

type sessionBrowserDialog struct {
	BaseDialog
	textInput  textinput.Model
	sessions   []session.Summary
	filtered   []session.Summary
	selected   int
	offset     int                  // scroll offset for viewport
	keyMap     sessionBrowserKeyMap // key bindings
	openedAt   time.Time            // when dialog was opened, for stable time display
	starFilter int                  // 0 = all, 1 = starred only, 2 = unstarred only
}

// NewSessionBrowserDialog creates a new session browser dialog
func NewSessionBrowserDialog(sessions []session.Summary) Dialog {
	ti := textinput.New()
	ti.Placeholder = "Type to search sessions…"
	ti.Focus()
	ti.CharLimit = 100
	ti.SetWidth(50)

	// Filter out empty sessions (sessions without a title)
	nonEmptySessions := make([]session.Summary, 0, len(sessions))
	for _, s := range sessions {
		if s.Title != "" {
			nonEmptySessions = append(nonEmptySessions, s)
		}
	}

	d := &sessionBrowserDialog{
		textInput: ti,
		sessions:  nonEmptySessions,
		keyMap:    defaultSessionBrowserKeyMap(),
		openedAt:  time.Now(),
	}
	// Initialize filtered list
	d.filterSessions()
	return d
}

func (d *sessionBrowserDialog) Init() tea.Cmd {
	return textinput.Blink
}

func (d *sessionBrowserDialog) Update(msg tea.Msg) (layout.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		cmd := d.SetSize(msg.Width, msg.Height)
		return d, cmd

	case tea.PasteMsg:
		// Forward paste to text input
		var cmd tea.Cmd
		d.textInput, cmd = d.textInput.Update(msg)
		d.filterSessions()
		return d, cmd

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
			}
			return d, nil

		case key.Matches(msg, d.keyMap.Down):
			if d.selected < len(d.filtered)-1 {
				d.selected++
			}
			return d, nil

		case key.Matches(msg, d.keyMap.PageUp):
			d.selected -= d.pageSize()
			if d.selected < 0 {
				d.selected = 0
			}
			return d, nil

		case key.Matches(msg, d.keyMap.PageDown):
			d.selected += d.pageSize()
			if d.selected >= len(d.filtered) {
				d.selected = max(0, len(d.filtered)-1)
			}
			return d, nil

		case key.Matches(msg, d.keyMap.Enter):
			if d.selected >= 0 && d.selected < len(d.filtered) {
				return d, tea.Sequence(
					core.CmdHandler(CloseDialogMsg{}),
					core.CmdHandler(messages.LoadSessionMsg{SessionID: d.filtered[d.selected].ID}),
				)
			}
			return d, nil

		case key.Matches(msg, d.keyMap.Star):
			if d.selected >= 0 && d.selected < len(d.filtered) {
				sessionID := d.filtered[d.selected].ID
				// Toggle the starred state in our local data
				for i := range d.sessions {
					if d.sessions[i].ID == sessionID {
						d.sessions[i].Starred = !d.sessions[i].Starred
						break
					}
				}
				for i := range d.filtered {
					if d.filtered[i].ID == sessionID {
						d.filtered[i].Starred = !d.filtered[i].Starred
						break
					}
				}
				return d, core.CmdHandler(messages.ToggleSessionStarMsg{SessionID: sessionID})
			}
			return d, nil

		case key.Matches(msg, d.keyMap.FilterStar):
			// Cycle through filter modes: all -> starred -> unstarred -> all
			d.starFilter = (d.starFilter + 1) % 3
			d.filterSessions()
			return d, nil

		case key.Matches(msg, d.keyMap.CopyID):
			if d.selected >= 0 && d.selected < len(d.filtered) {
				sessionID := d.filtered[d.selected].ID
				_ = clipboard.WriteAll(sessionID)
				return d, notification.SuccessCmd("Session ID copied to clipboard.")
			}
			return d, nil

		default:
			var cmd tea.Cmd
			d.textInput, cmd = d.textInput.Update(msg)
			d.filterSessions()
			return d, cmd
		}
	}

	return d, nil
}

func (d *sessionBrowserDialog) filterSessions() {
	query := strings.ToLower(strings.TrimSpace(d.textInput.Value()))

	d.filtered = nil
	for _, sess := range d.sessions {
		// Apply star filter
		switch d.starFilter {
		case 1: // Starred only
			if !sess.Starred {
				continue
			}
		case 2: // Unstarred only
			if sess.Starred {
				continue
			}
		}

		// Apply text search filter
		if query != "" {
			title := sess.Title
			if title == "" {
				title = "Untitled"
			}
			if !strings.Contains(strings.ToLower(title), query) {
				continue
			}
		}

		d.filtered = append(d.filtered, sess)
	}

	if d.selected >= len(d.filtered) {
		d.selected = max(0, len(d.filtered)-1)
	}
	d.offset = 0
}

func (d *sessionBrowserDialog) dialogSize() (dialogWidth, maxHeight, contentWidth int) {
	dialogWidth = max(min(d.Width()*80/100, 80), 60)
	maxHeight = min(d.Height()*70/100, 30)
	contentWidth = dialogWidth - 6
	return dialogWidth, maxHeight, contentWidth
}

func (d *sessionBrowserDialog) View() string {
	dialogWidth, maxHeight, contentWidth := d.dialogSize()

	d.textInput.SetWidth(contentWidth)

	var sessionLines []string
	maxItems := maxHeight - 10 // Reduced to make room for ID footer

	// Adjust offset to keep selected item visible
	if d.selected < d.offset {
		d.offset = d.selected
	} else if d.selected >= d.offset+maxItems {
		d.offset = d.selected - maxItems + 1
	}

	// Render visible items based on offset
	visibleEnd := min(d.offset+maxItems, len(d.filtered))
	for i := d.offset; i < visibleEnd; i++ {
		sessionLines = append(sessionLines, d.renderSession(d.filtered[i], i == d.selected, contentWidth))
	}

	// Show indicator if there are more items
	if visibleEnd < len(d.filtered) {
		sessionLines = append(sessionLines, styles.MutedStyle.Render(fmt.Sprintf("  … and %d more", len(d.filtered)-visibleEnd)))
	}

	if len(d.filtered) == 0 {
		sessionLines = append(sessionLines, "", styles.DialogContentStyle.
			Italic(true).
			Align(lipgloss.Center).
			Width(contentWidth).
			Render("No sessions found"))
	}

	// Build title with filter indicator
	title := "Sessions"
	switch d.starFilter {
	case 1:
		title = "Sessions " + styles.StarredStyle.Render("★")
	case 2:
		title = "Sessions " + styles.UnstarredStyle.Render("☆")
	}

	// Build filter description for help
	var filterDesc string
	switch d.starFilter {
	case 0:
		filterDesc = "all"
	case 1:
		filterDesc = "★ only"
	case 2:
		filterDesc = "☆ only"
	}

	// Build session ID footer for selected session
	var idFooter string
	if d.selected >= 0 && d.selected < len(d.filtered) {
		idFooter = styles.MutedStyle.Render("ID: ") + styles.SecondaryStyle.Render(d.filtered[d.selected].ID)
	}

	content := NewContent(contentWidth).
		AddTitle(title).
		AddSpace().
		AddContent(d.textInput.View()).
		AddSeparator().
		AddContent(strings.Join(sessionLines, "\n")).
		AddSeparator().
		AddContent(idFooter).
		AddSpace().
		AddHelpKeys("↑/↓", "navigate", "s", "star", "f", filterDesc, "c", "copy id", "enter", "load", "esc", "close").
		Build()

	return styles.DialogStyle.Width(dialogWidth).Render(content)
}

func (d *sessionBrowserDialog) pageSize() int {
	_, maxHeight, _ := d.dialogSize()
	return max(1, maxHeight-10) // Match maxItems calculation in View
}

func (d *sessionBrowserDialog) renderSession(sess session.Summary, selected bool, maxWidth int) string {
	titleStyle, timeStyle := styles.PaletteUnselectedActionStyle, styles.PaletteUnselectedDescStyle
	if selected {
		titleStyle, timeStyle = styles.PaletteSelectedActionStyle, styles.PaletteSelectedDescStyle
	}

	title := sess.Title
	if title == "" {
		title = "Untitled"
	}

	// Account for star indicator width in title length calculation
	maxTitleLen := maxWidth - 28 // 25 for time + 3 for star indicator
	if len(title) > maxTitleLen {
		title = title[:maxTitleLen-1] + "…"
	}

	return styles.StarIndicator(sess.Starred) + titleStyle.Render(title) + timeStyle.Render(" • "+d.timeAgo(sess.CreatedAt))
}

func (d *sessionBrowserDialog) timeAgo(t time.Time) string {
	elapsed := d.openedAt.Sub(t)
	switch {
	case elapsed < time.Minute:
		return fmt.Sprintf("%ds ago", int(elapsed.Seconds()))
	case elapsed < time.Hour:
		return fmt.Sprintf("%dm ago", int(elapsed.Minutes()))
	case elapsed < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(elapsed.Hours()))
	case elapsed < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(elapsed.Hours()/24))
	default:
		return t.Format("Jan 2")
	}
}

func (d *sessionBrowserDialog) Position() (row, col int) {
	dialogWidth, maxHeight, _ := d.dialogSize()
	return CenterPosition(d.Width(), d.Height(), dialogWidth, maxHeight)
}
