package dialog

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/tui/commands"
)

var categories = []commands.Category{
	{
		Name: "Session",
		Commands: []commands.Item{
			{
				ID:          "session.new",
				Label:       "New Session",
				Description: "Start a new conversation session",
				Category:    "Session",
				Execute:     func() tea.Cmd { return nil },
			},
			{
				ID:          "session.compact",
				Label:       "Compact Session",
				Description: "Summarize and compact the current conversation",
				Category:    "Session",
				Execute:     func() tea.Cmd { return nil },
			},
		},
	},
}

func TestCommandPaletteFiltering(t *testing.T) {
	dialog := NewCommandPaletteDialog(categories)
	d := dialog.(*commandPaletteDialog)

	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "filter by new",
			input:    "new",
			expected: []string{"session.new"},
		},
		{
			name:     "filter by compact",
			input:    "compact",
			expected: []string{"session.compact"},
		},
		{
			name:     "filter by description",
			input:    "summarize",
			expected: []string{"session.compact"},
		},
		{
			name:     "no match",
			input:    "nonexistent",
			expected: []string{},
		},
		{
			name:     "empty search shows all",
			input:    "",
			expected: []string{"session.new", "session.compact"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d.textInput.SetValue(tt.input)
			d.filterCommands()
			require.Len(t, d.filtered, len(tt.expected))

			for i, expectedID := range tt.expected {
				require.Equal(t, expectedID, d.filtered[i].ID)
			}
		})
	}
}
