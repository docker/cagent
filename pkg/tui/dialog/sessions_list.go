package dialog

import (
	"sort"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/junegunn/fzf/src/algo"
	"github.com/junegunn/fzf/src/util"

	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/tools/humanize"
	"github.com/docker/cagent/pkg/tui/core"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/styles"
)

const maxSessionItems = 10

// SessionSelectedMsg is sent when a session is selected for loading
type SessionSelectedMsg struct {
	Session *session.Session
}

type sessionItem struct {
	session     *session.Session
	label       string
	description string
}

type sessionMatchResult struct {
	item  sessionItem
	score int
}

type sessionsListDialog struct {
	width, height    int
	textInput        textinput.Model
	items            []sessionItem
	filtered         []sessionItem
	selected         int
	scrollOffset     int
	currentSessionID string
}

func NewSessionsListDialog(sessions []*session.Session, currentSessionID string) Dialog {
	ti := textinput.New()
	ti.Placeholder = "Type to search sessions..."
	ti.Focus()
	ti.CharLimit = 100
	ti.SetWidth(50)

	var items []sessionItem
	for _, s := range sessions {
		if s.ID != currentSessionID {
			items = append(items, sessionItem{
				session:     s,
				label:       getSessionTitle(s),
				description: humanize.Time(s.CreatedAt),
			})
		}
	}

	return &sessionsListDialog{
		textInput:        ti,
		items:            items,
		filtered:         items,
		selected:         0,
		scrollOffset:     0,
		currentSessionID: currentSessionID,
	}
}

func (d *sessionsListDialog) Init() tea.Cmd {
	return textinput.Blink
}

func (d *sessionsListDialog) Update(msg tea.Msg) (layout.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		d.width = msg.Width
		d.height = msg.Height
		return d, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			return d, core.CmdHandler(CloseDialogMsg{})

		case "up":
			if d.selected > 0 {
				d.selected--
			}
			if d.selected < d.scrollOffset {
				d.scrollOffset = d.selected
			}
			return d, nil

		case "down":
			if d.selected < len(d.filtered)-1 {
				d.selected++
			}
			if d.selected >= d.scrollOffset+maxSessionItems {
				d.scrollOffset = d.selected - maxSessionItems + 1
			}
			return d, nil

		case "pgup":
			d.selected -= maxSessionItems
			if d.selected < 0 {
				d.selected = 0
			}
			if d.selected < d.scrollOffset {
				d.scrollOffset = d.selected
			}
			return d, nil

		case "pgdown":
			d.selected += maxSessionItems
			if d.selected >= len(d.filtered) {
				d.selected = len(d.filtered) - 1
			}
			if d.selected < 0 {
				d.selected = 0
			}
			if d.selected >= d.scrollOffset+maxSessionItems {
				d.scrollOffset = d.selected - maxSessionItems + 1
			}
			return d, nil

		case "enter":
			if d.selected >= 0 && d.selected < len(d.filtered) {
				selectedSession := d.filtered[d.selected].session
				return d, core.CmdHandler(SessionSelectedMsg{Session: selectedSession})
			}
			return d, nil

		case "ctrl+c":
			return d, tea.Quit

		default:
			var cmd tea.Cmd
			d.textInput, cmd = d.textInput.Update(msg)
			cmds = append(cmds, cmd)
			d.filterSessions()
		}
	}

	return d, tea.Batch(cmds...)
}

func (d *sessionsListDialog) filterSessions() {
	query := strings.TrimSpace(d.textInput.Value())

	if query == "" {
		d.filtered = d.items
		if d.selected >= len(d.filtered) {
			d.selected = max(0, len(d.filtered)-1)
		}
		return
	}

	pattern := []rune(strings.ToLower(query))
	var matches []sessionMatchResult

	for _, item := range d.items {
		chars := util.ToChars([]byte(item.label))
		result, _ := algo.FuzzyMatchV1(
			false, // caseSensitive
			false, // normalize
			true,  // forward
			&chars,
			pattern,
			true, // withPos
			nil,  // slab
		)

		if result.Start >= 0 {
			matches = append(matches, sessionMatchResult{
				item:  item,
				score: result.Score,
			})
		}
	}

	// Sort by score descending
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].score > matches[j].score
	})

	// Build filtered list
	d.filtered = make([]sessionItem, 0, len(matches))
	for _, match := range matches {
		d.filtered = append(d.filtered, match.item)
	}

	// Adjust selection if beyond filtered list
	if d.selected >= len(d.filtered) {
		d.selected = max(0, len(d.filtered)-1)
	}

	// Adjust scroll offset to keep selected visible
	if d.selected < d.scrollOffset {
		d.scrollOffset = d.selected
	} else if d.selected >= d.scrollOffset+maxSessionItems {
		d.scrollOffset = max(0, d.selected-maxSessionItems+1)
	}
}

func getSessionTitle(s *session.Session) string {
	if s.Title != "" {
		return s.Title
	}

	return "Untitled"
}

func (d *sessionsListDialog) View() string {
	dialogWidth := max(min(d.width*80/100, 70), 50)
	contentWidth := dialogWidth - 6

	title := styles.DialogTitleStyle.Width(contentWidth).Render("Sessions")

	d.textInput.SetWidth(contentWidth)
	searchInput := d.textInput.View()

	separator := styles.DialogSeparatorStyle.
		Width(contentWidth).
		Render(strings.Repeat("─", contentWidth))

	var sessionList []string

	if len(d.filtered) == 0 {
		sessionList = append(sessionList, "", styles.DialogContentStyle.
			Italic(true).
			Align(lipgloss.Center).
			Width(contentWidth).
			Render("No sessions found"))
	} else {
		visibleStart := d.scrollOffset
		visibleEnd := min(d.scrollOffset+maxSessionItems, len(d.filtered))

		for i := visibleStart; i < visibleEnd; i++ {
			item := d.filtered[i]
			isSelected := i == d.selected
			sessionLine := d.renderSession(item, isSelected, contentWidth)
			sessionList = append(sessionList, sessionLine)
		}
	}

	help := styles.DialogHelpStyle.
		Width(contentWidth).
		Render("↑/↓ navigate • enter load • esc close")

	parts := []string{
		title,
		"",
		searchInput,
		separator,
	}
	parts = append(parts, sessionList...)
	parts = append(parts, "", help)

	return styles.DialogStyle.
		Width(dialogWidth).
		Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
}

func (d *sessionsListDialog) renderSession(item sessionItem, selected bool, contentWidth int) string {
	// Calculate max label length to leave room for date
	dateLen := len(item.description)
	maxLabelLen := contentWidth - dateLen - 6 // 6 for "  " prefix, spacing, and buffer

	label := item.label
	if len(label) > maxLabelLen {
		label = label[:maxLabelLen-3] + "..."
	}

	// Calculate padding to push date to the right
	paddingLen := contentWidth - len(label) - dateLen - 4 // 4 for "  " prefix and spacing
	if paddingLen < 1 {
		paddingLen = 1
	}

	text := "  " + label + strings.Repeat(" ", paddingLen) + styles.MutedStyle.Render(item.description)

	if selected {
		return styles.PaletteSelectedStyle.Width(contentWidth).Render(text)
	}

	return styles.PaletteUnselectedStyle.Width(contentWidth).Render(text)
}

func (d *sessionsListDialog) Position() (row, col int) {
	dialogWidth := max(min(d.width*80/100, 70), 50)
	maxHeight := min(d.height*70/100, 30)

	row = max(0, (d.height-maxHeight)/2)
	col = max(0, (d.width-dialogWidth)/2)
	return row, col
}

func (d *sessionsListDialog) SetSize(width, height int) tea.Cmd {
	d.width = width
	d.height = height
	return nil
}
