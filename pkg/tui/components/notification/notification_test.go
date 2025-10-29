package notification

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNotification_InitialState(t *testing.T) {
	n := New()

	require.Equal(t, StateHidden, n.state)
	require.Empty(t, n.text)
	require.False(t, n.IsVisible())
}

func TestNotification_Show(t *testing.T) {
	n := New()

	updated, _ := n.Update(ShowMsg{Text: "Test notification"})

	require.Equal(t, StateVisible, updated.state)
	require.Equal(t, "Test notification", updated.text)
	require.True(t, updated.IsVisible())
	require.NotEmpty(t, updated.View())
}

func TestNotification_Hide(t *testing.T) {
	n := New()

	updated, _ := n.Update(ShowMsg{Text: "Test"})
	updated, _ = updated.Update(HideMsg{})

	require.Equal(t, StateHidden, updated.state)
	require.Empty(t, updated.text)
	require.False(t, updated.IsVisible())
	require.Empty(t, updated.View())
}

func TestNotification_Position(t *testing.T) {
	n := New()
	n.SetSize(100, 50)
	updated, _ := n.Update(ShowMsg{Text: "Test"})
	row, col := updated.position()

	require.Equal(t, 45, row)
	require.Equal(t, 90, col)
}

func TestNotification_GetLayer(t *testing.T) {
	n := New()

	require.Nil(t, n.GetLayer())

	updated, _ := n.Update(ShowMsg{Text: "Test"})
	require.NotNil(t, updated.GetLayer())
}
