package anthropic

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/tools"
)

func TestConvertMessages_SkipEmptySystemText(t *testing.T) {
	msgs := []chat.Message{{
		Role:    chat.MessageRoleSystem,
		Content: "   \n\t  ",
	}}

	out := convertMessages(msgs)
	assert.Empty(t, out)
}

func TestConvertMessages_SkipEmptyUserText_NoMultiContent(t *testing.T) {
	msgs := []chat.Message{{
		Role:    chat.MessageRoleUser,
		Content: "   \n\t  ",
	}}

	out := convertMessages(msgs)
	assert.Empty(t, out)
}

func TestConvertMessages_UserMultiContent_SkipEmptyText_KeepImage(t *testing.T) {
	msgs := []chat.Message{{
		Role: chat.MessageRoleUser,
		MultiContent: []chat.MessagePart{
			{Type: chat.MessagePartTypeText, Text: "   "},
			{Type: chat.MessagePartTypeImageURL, ImageURL: &chat.MessageImageURL{URL: "data:image/png;base64,AAAA"}},
		},
	}}

	out := convertMessages(msgs)
	require.Len(t, out, 1)

	b, err := json.Marshal(out[0])
	require.NoError(t, err)
	// Basic JSON structure checks
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	// role should be user
	assert.Equal(t, "user", m["role"])
	// content should contain exactly one block (the image)
	content, ok := m["content"].([]any)
	require.True(t, ok)
	assert.Len(t, content, 1)
	// and it should be an image block
	cb, ok := content[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "image", cb["type"])
}

func TestConvertMessages_SkipEmptyAssistantText_NoToolCalls(t *testing.T) {
	msgs := []chat.Message{{
		Role:    chat.MessageRoleAssistant,
		Content: "  \t\n  ",
	}}

	out := convertMessages(msgs)
	assert.Empty(t, out)
}

func TestConvertMessages_AssistantToolCalls_NoText_IncludesToolUse(t *testing.T) {
	msgs := []chat.Message{{
		Role:    chat.MessageRoleAssistant,
		Content: "   ",
		ToolCalls: []tools.ToolCall{
			{ID: "tool-1", Function: tools.FunctionCall{Name: "do_thing", Arguments: "{\"x\":1}"}},
		},
	}}

	out := convertMessages(msgs)
	require.Len(t, out, 1)

	b, err := json.Marshal(out[0])
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	assert.Equal(t, "assistant", m["role"])
	content, ok := m["content"].([]any)
	require.True(t, ok)
	assert.Len(t, content, 1)
	cb, ok := content[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "tool_use", cb["type"])
}

func TestSystemMessages_AreExtractedAndNotInMessageList(t *testing.T) {
	msgs := []chat.Message{
		{Role: chat.MessageRoleSystem, Content: "  system rules here  "},
		{Role: chat.MessageRoleUser, Content: "hi"},
	}

	// System blocks should be extracted
	sys := extractSystemBlocks(msgs)
	require.Len(t, sys, 1)
	assert.Equal(t, "system rules here", strings.TrimSpace(sys[0].Text))

	// System role messages must not appear in the anthropic messages list
	out := convertMessages(msgs)
	assert.Len(t, out, 1)
}

func TestSystemMessages_MultipleExtractedAndExcludedFromMessageList(t *testing.T) {
	msgs := []chat.Message{
		{Role: chat.MessageRoleSystem, Content: " sys A "},
		{Role: chat.MessageRoleSystem, Content: "\n sys B \t"},
		{Role: chat.MessageRoleUser, Content: "hello"},
	}

	sys := extractSystemBlocks(msgs)
	require.Len(t, sys, 2)
	assert.Equal(t, "sys A", strings.TrimSpace(sys[0].Text))
	assert.Equal(t, "sys B", strings.TrimSpace(sys[1].Text))

	out := convertMessages(msgs)
	assert.Len(t, out, 1)
}

func TestSystemMessages_InterspersedExtractedAndExcluded(t *testing.T) {
	msgs := []chat.Message{
		{Role: chat.MessageRoleSystem, Content: " S1 "},
		{Role: chat.MessageRoleUser, Content: "U1"},
		{Role: chat.MessageRoleAssistant, Content: "A1"},
		{Role: chat.MessageRoleSystem, Content: "S2"},
		{Role: chat.MessageRoleUser, Content: " U2 "},
	}

	// All system messages should be extracted in order of appearance
	sys := extractSystemBlocks(msgs)
	require.Len(t, sys, 2)
	assert.Equal(t, "S1", strings.TrimSpace(sys[0].Text))
	assert.Equal(t, "S2", strings.TrimSpace(sys[1].Text))

	// Converted messages must exclude system roles and preserve order of others
	out := convertMessages(msgs)
	require.Len(t, out, 3)
	// Check roles: user, assistant, user
	expectedRoles := []string{"user", "assistant", "user"}
	for i, expected := range expectedRoles {
		b, err := json.Marshal(out[i])
		require.NoError(t, err)
		var m map[string]any
		require.NoError(t, json.Unmarshal(b, &m))
		assert.Equal(t, expected, m["role"])
	}
}
