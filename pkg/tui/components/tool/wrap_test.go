package tool

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWrapLines(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		width    int
		expected []string
	}{
		// Basic wrapping cases
		{
			name:     "simple text within width",
			text:     "hello world",
			width:    20,
			expected: []string{"hello world"},
		},
		{
			name:     "text exactly at width",
			text:     "hello",
			width:    5,
			expected: []string{"hello"},
		},
		{
			name:     "single line longer than width",
			text:     "hello world this is a long line",
			width:    10,
			expected: []string{"hello worl", "d this is ", "a long lin", "e"},
		},
		{
			name:     "text wraps at exact boundary",
			text:     "abcdefghij",
			width:    5,
			expected: []string{"abcde", "fghij"},
		},
		{
			name:     "text wraps with remainder",
			text:     "abcdefghijk",
			width:    5,
			expected: []string{"abcde", "fghij", "k"},
		},

		// Multi-line input cases
		{
			name:     "multiple short lines",
			text:     "line1\nline2\nline3",
			width:    10,
			expected: []string{"line1", "line2", "line3"},
		},
		{
			name:     "mixed short and long lines",
			text:     "short\nthis is a very long line that needs wrapping\nanother",
			width:    10,
			expected: []string{"short", "this is a ", "very long ", "line that ", "needs wrap", "ping", "another"},
		},
		{
			name:     "empty lines preserved",
			text:     "line1\n\nline3",
			width:    10,
			expected: []string{"line1", "", "line3"},
		},
		{
			name:     "lines with trailing newline",
			text:     "line1\nline2\n",
			width:    10,
			expected: []string{"line1", "line2", ""},
		},

		// Edge cases
		{
			name:     "empty string",
			text:     "",
			width:    10,
			expected: []string{""},
		},
		{
			name:     "only newlines",
			text:     "\n\n\n",
			width:    10,
			expected: []string{"", "", "", ""},
		},
		{
			name:     "zero width",
			text:     "hello world",
			width:    0,
			expected: []string{"hello world"},
		},
		{
			name:     "negative width",
			text:     "hello world",
			width:    -5,
			expected: []string{"hello world"},
		},
		{
			name:     "width of 1",
			text:     "hello",
			width:    1,
			expected: []string{"h", "e", "l", "l", "o"},
		},
		{
			name:     "single character",
			text:     "a",
			width:    1,
			expected: []string{"a"},
		},
		{
			name:     "single character with large width",
			text:     "a",
			width:    100,
			expected: []string{"a"},
		},

		// Boundary and special cases
		{
			name:     "text with spaces at boundaries",
			text:     "hello world test",
			width:    6,
			expected: []string{"hello ", "world ", "test"},
		},
		{
			name:     "very long single word",
			text:     "supercalifragilisticexpialidocious",
			width:    10,
			expected: []string{"supercalif", "ragilistic", "expialidoc", "ious"},
		},
		{
			name:     "multiple consecutive newlines",
			text:     "a\n\n\nb",
			width:    5,
			expected: []string{"a", "", "", "b"},
		},
		{
			name:     "line exactly matching width multiple times",
			text:     "12345",
			width:    5,
			expected: []string{"12345"},
		},
		{
			name:     "unicode characters",
			text:     "héllo wörld",
			width:    8,
			expected: []string{"héllo w", "örld"},
		},
		{
			name:     "tabs and special characters",
			text:     "hello\tworld\ntest",
			width:    8,
			expected: []string{"hello\two", "rld", "test"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			wrapped := wrapLines(tt.text, tt.width)

			assert.Equal(t, tt.expected, wrapped)
		})
	}
}
