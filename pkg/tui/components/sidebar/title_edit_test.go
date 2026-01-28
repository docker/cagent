package sidebar

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/tui/service"
)

func TestSidebar_TitleEditStateTransitions(t *testing.T) {
	t.Parallel()

	sess := session.New()
	sessionState := service.NewSessionState(sess)
	sb := New(sessionState)

	// Initially not editing
	assert.False(t, sb.IsEditingTitle(), "should not be editing initially")

	// Begin editing
	sb.BeginTitleEdit()
	assert.True(t, sb.IsEditingTitle(), "should be editing after BeginTitleEdit")

	// Cancel editing
	sb.CancelTitleEdit()
	assert.False(t, sb.IsEditingTitle(), "should not be editing after CancelTitleEdit")

	// Begin editing again
	sb.BeginTitleEdit()
	assert.True(t, sb.IsEditingTitle(), "should be editing after second BeginTitleEdit")

	// Commit editing
	title := sb.CommitTitleEdit()
	assert.False(t, sb.IsEditingTitle(), "should not be editing after CommitTitleEdit")
	require.NotEmpty(t, title, "committed title should not be empty")
}

func TestSidebar_TitleEditPreservesInput(t *testing.T) {
	t.Parallel()

	sess := session.New()
	sessionState := service.NewSessionState(sess)
	sb := New(sessionState)

	// Set initial title by simulating session load
	m := sb.(*model)
	m.sessionTitle = "Original Title"

	// Begin editing - should populate input with current title
	sb.BeginTitleEdit()

	// The input should have the original title
	assert.Equal(t, "Original Title", m.titleInput.Value())

	// Commit should return the title
	title := sb.CommitTitleEdit()
	assert.Equal(t, "Original Title", title)
}

func TestSidebar_TitleEditCancelRestoresOriginal(t *testing.T) {
	t.Parallel()

	sess := session.New()
	sessionState := service.NewSessionState(sess)
	sb := New(sessionState)

	// Set initial title
	m := sb.(*model)
	m.sessionTitle = "Original Title"

	// Begin editing
	sb.BeginTitleEdit()

	// Simulate typing a new title
	m.titleInput.SetValue("New Title")

	// Cancel should not change the session title
	sb.CancelTitleEdit()
	assert.Equal(t, "Original Title", m.sessionTitle, "cancel should preserve original title")
}

func TestSidebar_HandleClickType(t *testing.T) {
	t.Parallel()

	sess := session.New()
	sessionState := service.NewSessionState(sess)
	sb := New(sessionState)

	m := sb.(*model)
	m.sessionHasContent = true // Enable star visibility

	// Default PaddingLeft is 1, so coordinates need to account for this
	paddingLeft := m.layoutCfg.PaddingLeft // 1

	// In vertical mode, the title line is at verticalStarY
	// Click on the star area (adjusted x = 0-2, so raw x = 1-3)
	result := sb.HandleClickType(paddingLeft+1, verticalStarY)
	assert.Equal(t, ClickStar, result, "click on star area should return ClickStar")

	// Click on the pencil icon area (at the end of title)
	// For sessionHasContent=true, star indicator is "☆ " (2 chars)
	// Set a short title so we can calculate the pencil position
	m.sessionTitle = "Hi"
	m.titleGenerated = true // Pencil only shows when title has been generated
	// Star "☆ " = 2 chars, title "Hi" = 2 chars, pencil " ✎" starts at position 4
	// Add padding to get raw x coordinate
	pencilX := paddingLeft + 4
	result = sb.HandleClickType(pencilX, verticalStarY)
	assert.Equal(t, ClickPencil, result, "click on pencil icon should return ClickPencil")

	// Click on the title text (not the star, not the pencil) should return ClickNone
	// Star ends at position 2, title starts at 2 and ends at 4
	titleX := paddingLeft + 3 // middle of title
	result = sb.HandleClickType(titleX, verticalStarY)
	assert.Equal(t, ClickNone, result, "click on title text (not pencil) should return ClickNone")

	// Click elsewhere (wrong y)
	result = sb.HandleClickType(10, 0)
	assert.Equal(t, ClickNone, result, "click elsewhere should return ClickNone")
}

func TestSidebar_TitleRegenerating(t *testing.T) {
	t.Parallel()

	sess := session.New()
	sessionState := service.NewSessionState(sess)
	sb := New(sessionState)

	m := sb.(*model)
	m.sessionTitle = "Original Title"

	// Initially not regenerating
	assert.False(t, m.titleRegenerating, "should not be regenerating initially")
	assert.False(t, m.needsSpinner(), "should not need spinner initially")

	// Start regenerating
	cmd := sb.SetTitleRegenerating(true)
	assert.True(t, m.titleRegenerating, "should be regenerating after SetTitleRegenerating(true)")
	assert.True(t, m.needsSpinner(), "should need spinner when regenerating")
	// The returned command starts the spinner animation
	assert.NotNil(t, cmd, "should return a command to start the spinner")

	// Stop regenerating
	cmd = sb.SetTitleRegenerating(false)
	assert.False(t, m.titleRegenerating, "should not be regenerating after SetTitleRegenerating(false)")
	assert.False(t, m.needsSpinner(), "should not need spinner after stopping regeneration")
	assert.Nil(t, cmd, "should return nil command when stopping")
}
