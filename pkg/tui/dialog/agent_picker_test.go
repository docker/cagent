package dialog

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWrapText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		text     string
		maxWidth int
		maxLines int
		expected []string
	}{
		{
			name:     "empty text",
			text:     "",
			maxWidth: 50,
			maxLines: 3,
			expected: nil,
		},
		{
			name:     "single word fits",
			text:     "hello",
			maxWidth: 50,
			maxLines: 3,
			expected: []string{"hello"},
		},
		{
			name:     "multiple words on one line",
			text:     "hello world",
			maxWidth: 50,
			maxLines: 3,
			expected: []string{"hello world"},
		},
		{
			name:     "words wrap to multiple lines",
			text:     "hello world this is a test",
			maxWidth: 12,
			maxLines: 3,
			expected: []string{"hello world", "this is a", "test"},
		},
		{
			name:     "max lines truncation",
			text:     "one two three four five six seven",
			maxWidth: 10,
			maxLines: 2,
			expected: []string{"one two", "three fou…"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := wrapText(tt.text, tt.maxWidth, tt.maxLines)
			assert.Equal(t, tt.expected, result)
		})
	}
}
