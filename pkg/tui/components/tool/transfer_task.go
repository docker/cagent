package tool

import (
	"encoding/json"

	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/docker/cagent/pkg/tui/styles"
	"github.com/docker/cagent/pkg/tui/types"
)

type transferTaskModel struct {
	msg *types.Message
}

func (m *transferTaskModel) Init() tea.Cmd {
	return nil
}

func (m *transferTaskModel) SetSize(width, height int) tea.Cmd {
	return nil
}

func (m *transferTaskModel) Update(tea.Msg) (tea.Model, tea.Cmd) {
	return m, nil
}

func (m *transferTaskModel) View() string {
	var params struct {
		Agent          string `json:"agent"`
		Task           string `json:"task"`
		ExpectedOutput string `json:"expected_output"`
	}
	if err := json.Unmarshal([]byte(m.msg.ToolCall.Function.Arguments), &params); err != nil {
		return "" // TODO: Partial tool call
	}

	return m.msg.Sender + " -> " + params.Agent + " task : " + styles.MutedStyle.Render(params.Task)
}
