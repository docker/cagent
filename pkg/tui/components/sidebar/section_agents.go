package sidebar

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/tui/styles"
)

// AgentsSection renders the agents list in the sidebar.
type AgentsSection struct {
	agents         []runtime.AgentDetails
	currentAgent   string
	agentSwitching bool

	// Computed during Render for click handling
	agentLines []agentLineInfo
}

// agentLineInfo tracks where an agent is rendered within this section.
type agentLineInfo struct {
	name      string
	startLine int // relative to section start
	lineCount int
}

// NewAgentsSection creates a new agents section.
func NewAgentsSection(agents []runtime.AgentDetails, currentAgent string, switching bool) *AgentsSection {
	return &AgentsSection{
		agents:         agents,
		currentAgent:   currentAgent,
		agentSwitching: switching,
	}
}

// Update updates the section state.
func (s *AgentsSection) Update(agents []runtime.AgentDetails, currentAgent string, switching bool) {
	s.agents = agents
	s.currentAgent = currentAgent
	s.agentSwitching = switching
}

// Render renders the agents section and returns content + line count.
func (s *AgentsSection) Render(contentWidth int) (string, int) {
	if len(s.agents) == 0 {
		return "", 0
	}

	// Reset agent line tracking
	s.agentLines = nil

	// Build header (title line + tab body top padding)
	suffix := ""
	if len(s.agents) > 1 {
		suffix = "^s"
	}
	header := s.renderHeader(suffix, contentWidth)
	headerLines := strings.Split(header, "\n")

	var allLines []string
	allLines = append(allLines, headerLines...)

	// Track where each agent starts (relative to section start)
	currentLine := len(headerLines)

	// Find current agent for model display
	var currentAgentDetails runtime.AgentDetails
	for _, agent := range s.agents {
		if agent.Name == s.currentAgent {
			currentAgentDetails = agent
			break
		}
	}

	// Render agent list (simple list, current agent highlighted)
	for i, agent := range s.agents {
		isCurrent := agent.Name == s.currentAgent
		content := s.renderAgentLine(agent, isCurrent, i, contentWidth)

		s.agentLines = append(s.agentLines, agentLineInfo{
			name:      agent.Name,
			startLine: currentLine,
			lineCount: 1, // Each agent is now just one line
		})

		allLines = append(allLines, content)
		currentLine++
	}

	// Add model info separately below the list
	if currentAgentDetails.Model != "" {
		allLines = append(allLines, "") // blank line separator
		modelLines := s.renderModelInfo(currentAgentDetails.Model, contentWidth)
		allLines = append(allLines, modelLines...)
	}

	// Add bottom padding
	allLines = append(allLines, "")

	return strings.Join(allLines, "\n"), len(allLines)
}

// HandleClick handles a click within this section.
func (s *AgentsSection) HandleClick(lineInSection int) *ClickResult {
	for _, info := range s.agentLines {
		if lineInSection >= info.startLine && lineInSection < info.startLine+info.lineCount {
			return &ClickResult{Type: ClickAgentSwitch, AgentName: info.name}
		}
	}
	return nil
}

// renderHeader renders the section header with optional suffix.
func (s *AgentsSection) renderHeader(suffix string, contentWidth int) string {
	styleTitle := styles.TabTitleStyle

	var titleLine string
	if suffix != "" {
		titleRendered := styleTitle.PaddingRight(1).Render("Agents")
		suffixRendered := styles.MutedStyle.PaddingLeft(1).Render(suffix)
		titleWidth := lipgloss.Width(titleRendered)
		suffixWidth := lipgloss.Width(suffixRendered)
		dashWidth := contentWidth - titleWidth - suffixWidth
		if dashWidth < 1 {
			dashWidth = 1
		}
		dashes := styleTitle.Render(strings.Repeat("─", dashWidth))
		titleLine = titleRendered + dashes + suffixRendered
	} else {
		titleLine = lipgloss.PlaceHorizontal(contentWidth, lipgloss.Left,
			styleTitle.PaddingRight(1).Render("Agents"),
			lipgloss.WithWhitespaceChars("─"),
			lipgloss.WithWhitespaceStyle(styleTitle),
		)
	}

	// Add tab body top padding (1 blank line)
	return titleLine + "\n"
}

// renderAgentLine renders a single agent line in the list with right-aligned shortcut.
func (s *AgentsSection) renderAgentLine(agent runtime.AgentDetails, isCurrent bool, index, contentWidth int) string {
	var left string
	if isCurrent {
		icon := "●"
		if s.agentSwitching {
			icon = "↔"
		}
		iconStyled := styles.RadioSelectedStyle.Render(icon)
		nameStyled := styles.AgentNameSelectedStyle.Render(agent.Name)
		left = iconStyled + " " + nameStyled
	} else {
		// Non-current agents: muted radio button and name
		radio := styles.RadioUnselectedStyle.Render("○")
		name := styles.MutedStyle.Render(agent.Name)
		left = radio + " " + name
	}

	// Add right-aligned shortcut for agents 1-9 (index 0-8)
	if index < 9 {
		shortcut := styles.MutedStyle.Render("^" + string('1'+rune(index)))
		leftWidth := lipgloss.Width(left)
		shortcutWidth := lipgloss.Width(shortcut)
		gap := contentWidth - leftWidth - shortcutWidth
		if gap > 0 {
			return left + strings.Repeat(" ", gap) + shortcut
		}
	}

	return left
}

// renderModelInfo renders the model information with word-wrapping.
func (s *AgentsSection) renderModelInfo(model string, contentWidth int) []string {
	label := styles.MutedStyle.Render("Model ")
	labelWidth := lipgloss.Width(label)

	// Calculate available width for model text
	availableWidth := contentWidth - labelWidth
	if availableWidth < 10 {
		availableWidth = 10
	}

	// Wrap the model text
	wrappedLines := wrapText(model, availableWidth)
	if len(wrappedLines) == 0 {
		return nil
	}

	var result []string
	// First line: label + first part of model (SecondaryStyle to stand out more)
	result = append(result, label+styles.SecondaryStyle.Render(wrappedLines[0]))

	// Subsequent lines: indent to align with model text
	indent := strings.Repeat(" ", labelWidth)
	for i := 1; i < len(wrappedLines); i++ {
		result = append(result, indent+styles.SecondaryStyle.Render(wrappedLines[i]))
	}

	return result
}

// wrapText wraps text to fit within maxWidth, breaking on reasonable boundaries.
func wrapText(text string, maxWidth int) []string {
	if maxWidth <= 0 || text == "" {
		return nil
	}

	// If it fits, return as-is
	if len(text) <= maxWidth {
		return []string{text}
	}

	var lines []string
	remaining := text

	for remaining != "" {
		if len(remaining) <= maxWidth {
			lines = append(lines, remaining)
			break
		}

		// Find a good break point (prefer breaking at / or -)
		breakAt := maxWidth
		for i := maxWidth; i > maxWidth/2; i-- {
			if remaining[i] == '/' || remaining[i] == '-' {
				breakAt = i + 1 // Include the delimiter
				break
			}
		}

		lines = append(lines, remaining[:breakAt])
		remaining = remaining[breakAt:]
	}

	return lines
}
