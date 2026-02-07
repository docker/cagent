package dialog

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewURLElicitationDialog(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		message string
		url     string
	}{
		{
			name:    "with message and URL",
			message: "Please authorize access",
			url:     "https://example.com/auth",
		},
		{
			name:    "with empty URL",
			message: "Confirmation required",
			url:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dialog := NewURLElicitationDialog(tt.message, tt.url)
			require.NotNil(t, dialog)

			ud, ok := dialog.(*URLElicitationDialog)
			require.True(t, ok)
			assert.Equal(t, tt.message, ud.message)
			assert.Equal(t, tt.url, ud.url)
		})
	}
}

func TestURLElicitationDialog_View(t *testing.T) {
	t.Parallel()

	dialog := NewURLElicitationDialog("Please visit the URL", "https://example.com/callback").(*URLElicitationDialog)
	dialog.SetSize(100, 50)

	view := dialog.View()

	// Should contain key elements
	assert.Contains(t, view, "MCP Server Request")
	assert.Contains(t, view, "Please visit the URL")
	assert.Contains(t, view, "https://example.com/callback")
	assert.Contains(t, view, "confirm")
	assert.Contains(t, view, "cancel")
	assert.Contains(t, view, "open") // New "open" key binding
}

func TestURLElicitationDialog_HasOpenKeyBinding(t *testing.T) {
	t.Parallel()

	dialog := NewURLElicitationDialog("Test", "https://example.com").(*URLElicitationDialog)

	// Verify the openBrowser key binding exists and is configured correctly
	require.NotNil(t, dialog.openBrowser)
	assert.NotEmpty(t, dialog.openBrowser.Keys())
}
