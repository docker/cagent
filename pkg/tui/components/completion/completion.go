package completion

import (
	"sort"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/junegunn/fzf/src/algo"
	"github.com/junegunn/fzf/src/util"

	"github.com/docker/cagent/pkg/tui/core"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/styles"
)

const maxItems = 10

type Item struct {
	Label       string
	Description string
	Value       string
	Execute     func() tea.Cmd
}

type OpenMsg struct {
	Items []Item
}

type OpenedMsg struct{}

type CloseMsg struct{}

type ClosedMsg struct{}

type QueryMsg struct {
	Query string
}

type SelectedMsg struct {
	Value   string
	Execute func() tea.Cmd
}

type matchResult struct {
	item  Item
	score int
}

type completionKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Enter  key.Binding
	Escape key.Binding
}

// defaultCompletionKeyMap returns default key bindings
func defaultCompletionKeyMap() completionKeyMap {
	return completionKeyMap{
		Up: key.NewBinding(
			key.WithKeys("up"),
			key.WithHelp("↑", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down"),
			key.WithHelp("↓", "down"),
		),
		Enter: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "select"),
		),
		Escape: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "cancel"),
		),
	}
}

// Manager manages the dialog stack and rendering
type Manager interface {
	layout.Model

	GetLayers() []*lipgloss.Layer
	Open() bool
}

// manager represents an item completion component that manages completion state and UI
type manager struct {
	keyMap        completionKeyMap
	width         int
	height        int
	items         []Item
	filteredItems []Item
	query         string
	selected      int
	scrollOffset  int
	visible       bool
}

// New creates a new  completion component
func New() Manager {
	return &manager{
		keyMap: defaultCompletionKeyMap(),
	}
}

func (c *manager) Init() tea.Cmd {
	return nil
}

func (c *manager) Open() bool {
	return c.visible
}

func (c *manager) Update(msg tea.Msg) (layout.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		c.width = msg.Width
		c.height = msg.Height
		return c, nil

	case QueryMsg:
		c.query = msg.Query
		c.filterItems(c.query)
		return c, nil

	case OpenMsg:
		c.visible = true
		c.items = msg.Items
		c.selected = 0
		c.scrollOffset = 0
		c.filterItems(c.query)
		return c, core.CmdHandler(OpenedMsg{})

	case CloseMsg:
		c.visible = false
		return c, nil

	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, c.keyMap.Up):
			if c.selected > 0 {
				c.selected--
			}
			if c.selected < c.scrollOffset {
				c.scrollOffset = c.selected
			}

		case key.Matches(msg, c.keyMap.Down):
			if c.selected < len(c.filteredItems)-1 {
				c.selected++
			}
			if c.selected >= c.scrollOffset+10 {
				c.scrollOffset = c.selected - 9
			}

		case key.Matches(msg, c.keyMap.Enter):
			c.visible = false
			return c, core.CmdHandler(SelectedMsg{Value: c.filteredItems[c.selected].Value, Execute: c.filteredItems[c.selected].Execute})
		case key.Matches(msg, c.keyMap.Escape):
			c.visible = false
			return c, core.CmdHandler(ClosedMsg{})
		}
	}

	return c, nil
}

func (c *manager) SetSize(width, height int) tea.Cmd {
	c.width = width
	c.height = height
	return nil
}

func (c *manager) View() string {
	if !c.visible {
		return ""
	}

	var lines []string

	if len(c.filteredItems) == 0 {
		lines = append(lines, styles.CompletionNoResultsStyle.Render("No results found"))
	} else {
		visibleStart := c.scrollOffset
		visibleEnd := min(c.scrollOffset+maxItems, len(c.filteredItems))

		maxLabelLen := 0
		for i := visibleStart; i < visibleEnd; i++ {
			labelLen := len(c.filteredItems[i].Label)
			if labelLen > maxLabelLen {
				maxLabelLen = labelLen
			}
		}

		for i := visibleStart; i < visibleEnd; i++ {
			item := c.filteredItems[i]
			isSelected := i == c.selected

			var itemStyle lipgloss.Style
			if isSelected {
				itemStyle = styles.CompletionSelectedStyle
			} else {
				itemStyle = styles.CompletionNormalStyle
			}

			// Pad label to maxLabelLen so descriptions align
			paddedLabel := item.Label + strings.Repeat(" ", maxLabelLen+1-len(item.Label))
			text := paddedLabel
			if item.Description != "" {
				text += " " + styles.CompletionDescStyle.Render(item.Description)
			}

			lines = append(lines, itemStyle.Width(c.width-6).Render(text))
		}
	}

	content := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return styles.CompletionBoxStyle.Render(content)
}

func (c *manager) GetLayers() []*lipgloss.Layer {
	if !c.visible {
		return nil
	}

	view := c.View()
	viewHeight := lipgloss.Height(view)

	editorHeight := 4
	yPos := max(c.height-viewHeight-editorHeight-1, 0)

	return []*lipgloss.Layer{
		lipgloss.NewLayer(view).SetContent(view).X(1).Y(yPos),
	}
}

func (c *manager) filterItems(query string) {
	if query == "" {
		c.filteredItems = c.items
		return
	}

	pattern := []rune(strings.ToLower(query))
	var matches []matchResult

	for _, item := range c.items {
		chars := util.ToChars([]byte(item.Label))
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
			matches = append(matches, matchResult{
				item:  item,
				score: result.Score,
			})
		}
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].score > matches[j].score
	})

	c.filteredItems = make([]Item, 0, len(matches))
	for _, match := range matches {
		c.filteredItems = append(c.filteredItems, match.item)
	}
}
