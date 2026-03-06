package dialog

import (
	"cmp"
	"fmt"
	"sort"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/atotto/clipboard"

	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/tui/components/notification"
	"github.com/docker/cagent/pkg/tui/components/scrollview"
	"github.com/docker/cagent/pkg/tui/core"
	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/styles"
)

// costDialog displays detailed cost breakdown for a session.
type costDialog struct {
	BaseDialog
	session    *session.Session
	keyMap     costDialogKeyMap
	scrollview *scrollview.Model
}

type costDialogKeyMap struct {
	Close, Copy key.Binding
}

func NewCostDialog(sess *session.Session) Dialog {
	return &costDialog{
		session: sess,
		scrollview: scrollview.New(
			scrollview.WithKeyMap(scrollview.ReadOnlyScrollKeyMap()),
			scrollview.WithReserveScrollbarSpace(true),
		),
		keyMap: costDialogKeyMap{
			Close: key.NewBinding(key.WithKeys("esc", "enter", "q"), key.WithHelp("Esc", "close")),
			Copy:  key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "copy")),
		},
	}
}

func (d *costDialog) Init() tea.Cmd {
	return nil
}

func (d *costDialog) Update(msg tea.Msg) (layout.Model, tea.Cmd) {
	if handled, cmd := d.scrollview.Update(msg); handled {
		return d, cmd
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		cmd := d.SetSize(msg.Width, msg.Height)
		return d, cmd

	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, d.keyMap.Close):
			return d, core.CmdHandler(CloseDialogMsg{})
		case key.Matches(msg, d.keyMap.Copy):
			_ = clipboard.WriteAll(d.renderPlainText())
			return d, notification.SuccessCmd("Cost details copied to clipboard.")
		}
	}
	return d, nil
}

func (d *costDialog) dialogSize() (dialogWidth, maxHeight, contentWidth int) {
	dialogWidth = d.ComputeDialogWidth(70, 50, 80)
	maxHeight = min(d.Height()*70/100, 40)
	contentWidth = d.ContentWidth(dialogWidth, 2) - d.scrollview.ReservedCols()
	return dialogWidth, maxHeight, contentWidth
}

func (d *costDialog) Position() (row, col int) {
	dialogWidth, maxHeight, _ := d.dialogSize()
	return CenterPosition(d.Width(), d.Height(), dialogWidth, maxHeight)
}

func (d *costDialog) View() string {
	dialogWidth, maxHeight, contentWidth := d.dialogSize()
	content := d.renderContent(contentWidth, maxHeight)
	return styles.DialogStyle.Padding(1, 2).Width(dialogWidth).Render(content)
}

type totalUsage struct {
	chat.Usage
	label string
	cost  float64
}

func (u *totalUsage) add(cost float64, usage *chat.Usage) {
	u.cost += cost
	u.InputTokens += usage.InputTokens
	u.OutputTokens += usage.OutputTokens
	u.CachedInputTokens += usage.CachedInputTokens
	u.CacheWriteTokens += usage.CacheWriteTokens
}

func (u *totalUsage) totalInput() int64 {
	return u.InputTokens + u.CachedInputTokens + u.CacheWriteTokens
}

// costData holds aggregated cost data for display.
type costData struct {
	total             totalUsage
	models            []totalUsage
	messages          []totalUsage
	hasPerMessageData bool
}

func (d *costDialog) gatherCostData() costData {
	var data costData
	modelMap := make(map[string]*totalUsage)

	// Helper to add a usage record to the aggregated data
	addRecord := func(agentName, model string, cost float64, usage *chat.Usage) {
		data.hasPerMessageData = true
		data.total.add(cost, usage)

		// Per-model usage
		model = cmp.Or(model, "unknown")
		if modelMap[model] == nil {
			modelMap[model] = &totalUsage{label: model}
		}
		modelMap[model].add(cost, usage)

		// Per-message usage
		msgLabel := fmt.Sprintf("#%d", len(data.messages)+1)
		if agentName != "" {
			msgLabel = fmt.Sprintf("#%d [%s]", len(data.messages)+1, agentName)
		}
		data.messages = append(data.messages, totalUsage{
			label: msgLabel,
			cost:  cost,
			Usage: *usage,
		})
	}

	// Helper to add a compaction cost entry (no token usage, just cost)
	addCompactionCost := func(cost float64) {
		data.hasPerMessageData = true
		data.total.cost += cost

		data.messages = append(data.messages, totalUsage{
			label: "compaction",
			cost:  cost,
		})
	}

	// Try session messages first (local mode), then MessageUsageHistory (remote mode)
	for _, msg := range d.session.GetAllMessages() {
		if msg.Message.Usage != nil {
			addRecord(msg.AgentName, msg.Message.Model, msg.Message.Cost, msg.Message.Usage)
		}
	}
	if !data.hasPerMessageData {
		for _, record := range d.session.MessageUsageHistory {
			addRecord(record.AgentName, record.Model, record.Cost, &record.Usage)
		}
	}

	// Include compaction costs from summary items. These are stored on
	// session items that have a Summary but no Message, so they are not
	// returned by GetAllMessages().
	for _, item := range d.session.Messages {
		if item.Summary != "" && item.Cost > 0 {
			addCompactionCost(item.Cost)
		}
	}

	// Convert model map to sorted slice (by cost descending)
	for _, m := range modelMap {
		data.models = append(data.models, *m)
	}
	sort.Slice(data.models, func(i, j int) bool {
		return data.models[i].cost > data.models[j].cost
	})

	// Fall back to session-level totals if no per-message data (e.g., past sessions)
	if !data.hasPerMessageData {
		data.total = totalUsage{
			cost: d.session.TotalCost(),
			Usage: chat.Usage{
				InputTokens:  d.session.InputTokens,
				OutputTokens: d.session.OutputTokens,
			},
		}
	}

	return data
}

func (d *costDialog) renderContent(contentWidth, maxHeight int) string {
	data := d.gatherCostData()

	// Build all lines
	lines := []string{
		RenderTitle("Session Cost Details", contentWidth, styles.DialogTitleStyle),
		RenderSeparator(contentWidth),
		"",
		sectionStyle().Render("Total"),
		"",
		accentStyle().Render(formatCost(data.total.cost)),
		d.renderInputLine(data.total, true),
		fmt.Sprintf("%s %s", labelStyle().Render("output:"), valueStyle().Render(formatTokenCount(data.total.OutputTokens))),
		"",
	}

	// By Model Section
	if len(data.models) > 0 {
		lines = append(lines, sectionStyle().Render("By Model"), "")
		for _, m := range data.models {
			lines = append(lines, d.renderUsageLine(m))
		}
		lines = append(lines, "")
	}

	// By Message Section
	if len(data.messages) > 0 {
		lines = append(lines, sectionStyle().Render("By Message"), "")
		for _, m := range data.messages {
			lines = append(lines, d.renderUsageLine(m))
		}
		lines = append(lines, "")
	} else if !data.hasPerMessageData && data.total.cost > 0 {
		lines = append(lines, styles.MutedStyle.Render("Per-message breakdown not available for this session."), "")
	}

	// Apply scrolling
	return d.applyScrolling(lines, contentWidth, maxHeight)
}

func (d *costDialog) renderInputLine(u totalUsage, showBreakdown bool) string {
	line := fmt.Sprintf("%s %s", labelStyle().Render("input:"), valueStyle().Render(formatTokenCount(u.totalInput())))
	if showBreakdown && (u.CachedInputTokens > 0 || u.CacheWriteTokens > 0) {
		line += valueStyle().Render(fmt.Sprintf(" (%s new + %s cached + %s cache write)",
			formatTokenCount(u.InputTokens),
			formatTokenCount(u.CachedInputTokens),
			formatTokenCount(u.CacheWriteTokens)))
	}
	return line
}

func (d *costDialog) renderUsageLine(u totalUsage) string {
	return fmt.Sprintf("%s  %s %s  %s %s  %s",
		accentStyle().Render(padRight(formatCostPadded(u.cost))),
		labelStyle().Render("input:"),
		valueStyle().Render(padRight(formatTokenCount(u.totalInput()))),
		labelStyle().Render("output:"),
		valueStyle().Render(padRight(formatTokenCount(u.OutputTokens))),
		accentStyle().Render(u.label))
}

func (d *costDialog) applyScrolling(allLines []string, contentWidth, maxHeight int) string {
	const headerLines = 3 // title + separator + space
	const footerLines = 2 // space + help

	visibleLines := max(1, maxHeight-headerLines-footerLines-4)
	contentLines := allLines[headerLines:]

	regionWidth := contentWidth + d.scrollview.ReservedCols()
	d.scrollview.SetSize(regionWidth, visibleLines)

	// Set scrollview position for mouse hit-testing (auto-computed from dialog position)
	// Y offset: border(1) + padding(1) + headerLines(3) = 5
	dialogRow, dialogCol := d.Position()
	d.scrollview.SetPosition(dialogCol+3, dialogRow+2+headerLines)

	d.scrollview.SetContent(contentLines, len(contentLines))

	scrollableContent := d.scrollview.View()
	parts := append(allLines[:headerLines], scrollableContent)
	parts = append(parts, "", RenderHelpKeys(regionWidth, "↑↓", "scroll", "c", "copy", "Esc", "close"))
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (d *costDialog) renderPlainText() string {
	data := d.gatherCostData()
	var lines []string

	// Build input line with optional breakdown
	inputLine := fmt.Sprintf("input: %s", formatTokenCount(data.total.totalInput()))
	if data.total.CachedInputTokens > 0 || data.total.CacheWriteTokens > 0 {
		inputLine += fmt.Sprintf(" (%s new + %s cached + %s cache write)",
			formatTokenCount(data.total.InputTokens),
			formatTokenCount(data.total.CachedInputTokens),
			formatTokenCount(data.total.CacheWriteTokens))
	}

	lines = append(lines, "Session Cost Details", "", "Total", formatCost(data.total.cost),
		inputLine, fmt.Sprintf("output: %s", formatTokenCount(data.total.OutputTokens)), "")

	if len(data.models) > 0 {
		lines = append(lines, "By Model")
		for _, m := range data.models {
			lines = append(lines, fmt.Sprintf("%-8s  input: %-8s  output: %-8s  %s",
				formatCostPadded(m.cost), formatTokenCount(m.totalInput()), formatTokenCount(m.OutputTokens), m.label))
		}
		lines = append(lines, "")
	}

	if len(data.messages) > 0 {
		lines = append(lines, "By Message")
		for _, m := range data.messages {
			lines = append(lines, fmt.Sprintf("%-8s  input: %-8s  output: %-8s  %s",
				formatCostPadded(m.cost), formatTokenCount(m.totalInput()), formatTokenCount(m.OutputTokens), m.label))
		}
	}

	return strings.Join(lines, "\n")
}

// Style getters - use functions to pick up theme changes dynamically
func sectionStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(styles.TextSecondary)
}

func labelStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true)
}

func valueStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(styles.TextSecondary)
}

func accentStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(styles.Highlight)
}

func formatCost(cost float64) string {
	if cost < 0.0001 {
		return "$0.00"
	}
	if cost < 0.01 {
		return fmt.Sprintf("$%.4f", cost)
	}
	return fmt.Sprintf("$%.2f", cost)
}

func formatCostPadded(cost float64) string {
	if cost < 0.0001 {
		return "$0.0000"
	}
	if cost < 0.01 {
		return fmt.Sprintf("$%.4f", cost)
	}
	return fmt.Sprintf("$%.2f  ", cost)
}

func formatTokenCount(count int64) string {
	switch {
	case count >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(count)/1_000_000)
	case count >= 1_000:
		return fmt.Sprintf("%.1fK", float64(count)/1_000)
	default:
		return fmt.Sprintf("%d", count)
	}
}

func padRight(s string) string {
	const width = 8
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}
