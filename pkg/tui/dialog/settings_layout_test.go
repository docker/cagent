package dialog

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/tui/messages"
)

func TestSettingsDialogFitsTerminal(t *testing.T) {
	t.Parallel()

	for _, size := range []struct{ width, height int }{
		{30, 18}, {36, 14}, {36, 16}, {36, 18}, {48, 18}, {60, 24}, {80, 24}, {100, 50}, {140, 60},
	} {
		t.Run(fmt.Sprintf("%dx%d", size.width, size.height), func(t *testing.T) {
			t.Parallel()
			d := newTestSettingsDialog(t, messages.LayoutSettings{})
			d.Update(tea.WindowSizeMsg{Width: size.width, Height: size.height})
			for tab := range tabCount {
				d.tab = tab
				for row := range d.rowCount() {
					if !d.selectable(tab, row) {
						continue
					}
					d.selected[tab] = row
					view := d.View()
					assert.LessOrEqual(t, lipgloss.Width(view), size.width, "tab %d row %d", tab, row)
					assert.LessOrEqual(t, lipgloss.Height(view), size.height, "tab %d row %d", tab, row)
					assert.Contains(t, ansi.Strip(d.body.View()), "›", "focused row must remain visible: tab %d row %d", tab, row)
					assert.Contains(t, settingsPlainText(view), "esc cancel")
					assert.Equal(t, view, d.View(), "repeated rendering must be stable")
				}
			}
		})
	}
}

func TestSettingsDialogScrollsAndResizes(t *testing.T) {
	t.Parallel()

	d := newTestSettingsDialog(t, messages.LayoutSettings{})
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	d.View()
	assert.Zero(t, d.body.ScrollOffset())
	assert.NotContains(t, ansi.Strip(d.body.View()), "Show startup banner")

	d.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
	assert.Contains(t, ansi.Strip(d.View()), "› [x] Show startup banner")
	assert.Positive(t, d.body.ScrollOffset())

	d.Update(tea.WindowSizeMsg{Width: 48, Height: 18})
	view := d.View()
	assert.Contains(t, ansi.Strip(view), "Show startup banner")
	assert.LessOrEqual(t, lipgloss.Height(view), 18)

	d.Update(tea.KeyPressMsg{Code: tea.KeyHome})
	assert.Contains(t, ansi.Strip(d.View()), "› Theme")
	assert.Zero(t, d.body.ScrollOffset())

	d.Update(tea.WindowSizeMsg{Width: 100, Height: 60})
	view = d.View()
	assert.Contains(t, ansi.Strip(view), "Show startup banner")
	assert.Contains(t, ansi.Strip(view), "chat")
}

func TestSettingsDialogCompactPreviewPreservesLiveUpdates(t *testing.T) {
	t.Parallel()

	d := newTestSettingsDialog(t, messages.LayoutSettings{})
	d.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	d.selected[tabAppearance] = rowPosition
	assert.NotContains(t, ansi.Strip(d.View()), "│input")
	assert.NotContains(t, ansi.Strip(d.body.View()), "chat")
	_, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	msgs := collectMsgs(cmd)
	require.Len(t, msgs, 1)
	preview, ok := msgs[0].(messages.PreviewLayoutMsg)
	require.True(t, ok)
	assert.Equal(t, messages.SidebarLeft, preview.Layout.SidebarPosition)
	assert.Contains(t, ansi.Strip(d.View()), "‹ Left ›")
}

func TestSettingsDialogSendModeFocus(t *testing.T) {
	t.Parallel()

	d := newTestSettingsDialog(t, messages.LayoutSettings{})
	d.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	assert.Contains(t, ansi.Strip(d.View()), "› ● Steer")

	d.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	view := ansi.Strip(d.View())
	assert.Contains(t, view, "● Steer")
	assert.NotContains(t, view, "› ● Steer")
	assert.Contains(t, view, "› [ ] Auto-approve")

	d.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	d.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	assert.Contains(t, ansi.Strip(d.View()), "› ● Queue")
}

func TestSettingsDialogContextualHelp(t *testing.T) {
	t.Parallel()

	d := newTestSettingsDialog(t, messages.LayoutSettings{})
	assert.Contains(t, ansi.Strip(d.View()), "enter open")
	d.selected[tabAppearance] = rowPosition
	assert.Contains(t, ansi.Strip(d.View()), "enter apply")

	d.tab = tabBehavior
	d.selected[tabBehavior] = rowRestoreTabs
	assert.Contains(t, ansi.Strip(d.View()), "Takes effect on the next launch.")

	d.selected[tabBehavior] = rowYOLO
	for _, size := range []struct{ width, height int }{{80, 24}, {36, 16}, {30, 18}, {36, 14}} {
		d.Update(tea.WindowSizeMsg{Width: size.width, Height: size.height})
		d.Update(tea.KeyPressMsg{Code: tea.KeySpace})
		require.True(t, d.confirmYOLO)
		require.False(t, d.current.YOLO)
		view := ansi.Strip(d.View())
		assert.Contains(t, settingsPlainText(view), "enter confirm")
		assert.Contains(t, settingsPlainText(view), "Press again to enable.")
		assert.LessOrEqual(t, lipgloss.Height(view), size.height)
		d.Update(tea.KeyPressMsg{Code: tea.KeySpace})
		require.True(t, d.current.YOLO)
		assert.Contains(t, ansi.Strip(d.View()), "enter apply")
		d.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	}
}

func TestSettingsDialogWrapsLongValues(t *testing.T) {
	t.Parallel()

	d := newTestSettingsDialog(t, messages.LayoutSettings{})
	value := "registry.example.com/" + strings.Repeat("界", 40) + ":latest"
	for _, width := range []int{28, 42, 64} {
		row := d.renderSelectorRow(rowTheme, "Theme", value, width)
		assert.LessOrEqual(t, lipgloss.Width(row), width)
		plain := strings.Join(strings.Fields(ansi.Strip(row)), "")
		assert.Contains(t, plain, value)
	}
}

func settingsPlainText(view string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(ansi.Strip(view), "│", " ")), " ")
}

func TestSettingsDialogQueueVisibleInShortTerminal(t *testing.T) {
	t.Parallel()

	d := newTestSettingsDialog(t, messages.LayoutSettings{})
	d.Update(tea.WindowSizeMsg{Width: 36, Height: 18})
	d.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	d.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	assert.Contains(t, ansi.Strip(d.View()), "› ● Queue")

	d.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	assert.Contains(t, ansi.Strip(d.View()), "› ● Steer")
}

func TestSettingsDialogGroupHeadingFitsNarrowTerminal(t *testing.T) {
	t.Parallel()

	d := newTestSettingsDialog(t, messages.LayoutSettings{})
	d.Update(tea.WindowSizeMsg{Width: 36, Height: 18})
	d.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	assert.Contains(t, ansi.Strip(d.View()), "── While agent is work")
}

func TestSettingsDialogRenderingDoesNotChangePreferences(t *testing.T) {
	t.Parallel()

	d := newTestSettingsDialog(t, messages.LayoutSettings{})
	original := d.current
	for tab := range tabCount {
		d.tab = tab
		for row := range d.rowCount() {
			d.selected[tab] = row
			_, cmd := d.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
			assert.Nil(t, cmd)
			d.View()
			d.Position()
			assert.Equal(t, original, d.current)
			assert.Equal(t, original, d.original)
		}
	}
}

func TestSettingsDialogUsesAvailableSpace(t *testing.T) {
	t.Parallel()

	for _, size := range []struct {
		width, height, dialogWidth, dialogHeight int
	}{
		{80, 24, 76, 24},
		{100, 40, 95, 38},
		{140, 50, 128, 48},
	} {
		t.Run(fmt.Sprintf("%dx%d", size.width, size.height), func(t *testing.T) {
			t.Parallel()
			d := newTestSettingsDialog(t, messages.LayoutSettings{})
			d.Update(tea.WindowSizeMsg{Width: size.width, Height: size.height})
			for tab := range tabCount {
				d.tab = tab
				view := d.View()
				assert.Equal(t, size.dialogWidth, lipgloss.Width(view))
				assert.Equal(t, size.dialogHeight, lipgloss.Height(view))
				assert.GreaterOrEqual(t, d.body.VisibleHeight(), 16)
				assert.Contains(t, ansi.Strip(view), "Appearance")
				assert.Contains(t, ansi.Strip(view), "Notifications")
				if tab == tabBehavior {
					assert.Contains(t, ansi.Strip(view), "● Steer  send to the working agent mid-turn")
				}
			}
			d.tab = tabBehavior
			d.selected[tabBehavior] = rowYOLO
			d.Update(tea.KeyPressMsg{Code: tea.KeySpace})
			view := d.View()
			assert.Equal(t, size.dialogHeight, lipgloss.Height(view))
			assert.Contains(t, view, "Press again to enable.")
		})
	}
}
