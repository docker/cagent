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

func TestSequencingRepair_Standard(t *testing.T) {
	msgs := []chat.Message{
		{Role: chat.MessageRoleUser, Content: "start"},
		{
			Role: chat.MessageRoleAssistant,
			ToolCalls: []tools.ToolCall{
				{ID: "tool-1", Function: tools.FunctionCall{Name: "do_thing", Arguments: "{}"}},
			},
		},
		// Intentionally missing the user/tool_result message here
		{Role: chat.MessageRoleUser, Content: "continue"},
	}

	converted := convertMessages(msgs)
	err := validateAnthropicSequencing(converted)
	require.Error(t, err)

	repaired := repairAnthropicSequencing(converted)
	err = validateAnthropicSequencing(repaired)
	require.NoError(t, err)
}

func TestSequencingRepair_Beta(t *testing.T) {
	msgs := []chat.Message{
		{Role: chat.MessageRoleUser, Content: "start"},
		{
			Role: chat.MessageRoleAssistant,
			ToolCalls: []tools.ToolCall{
				{ID: "tool-1", Function: tools.FunctionCall{Name: "do_thing", Arguments: "{}"}},
			},
		},
		// Intentionally missing the user/tool_result message here
		{Role: chat.MessageRoleUser, Content: "continue"},
	}

	converted := convertBetaMessages(msgs)
	err := validateAnthropicSequencingBeta(converted)
	require.Error(t, err)

	repaired := repairAnthropicSequencingBeta(converted)
	err = validateAnthropicSequencingBeta(repaired)
	require.NoError(t, err)
}

func TestConvertMessages_DropOrphanToolResults_NoPrecedingToolUse(t *testing.T) {
	msgs := []chat.Message{
		{Role: chat.MessageRoleUser, Content: "start"},
		// Orphan tool result (no assistant tool_use immediately before)
		{Role: chat.MessageRoleTool, ToolCallID: "tool-1", Content: "result-1"},
		{Role: chat.MessageRoleUser, Content: "continue"},
	}

	converted := convertMessages(msgs)
	// Expect only the two user text messages to appear
	require.Len(t, converted, 2)

	// Ensure none of the converted messages contain tool_result blocks
	for i := range converted {
		b, err := json.Marshal(converted[i])
		require.NoError(t, err)
		var m map[string]any
		require.NoError(t, json.Unmarshal(b, &m))
		content, _ := m["content"].([]any)
		for _, c := range content {
			if cb, ok := c.(map[string]any); ok {
				assert.NotEqual(t, "tool_result", cb["type"], "unexpected orphan tool_result included in payload")
			}
		}
	}
}

func TestConvertMessages_GroupToolResults_AfterAssistantToolUse(t *testing.T) {
	msgs := []chat.Message{
		{Role: chat.MessageRoleUser, Content: "start"},
		{
			Role: chat.MessageRoleAssistant,
			ToolCalls: []tools.ToolCall{
				{ID: "tool-1", Function: tools.FunctionCall{Name: "t1", Arguments: "{}"}},
				{ID: "tool-2", Function: tools.FunctionCall{Name: "t2", Arguments: "{}"}},
			},
		},
		{Role: chat.MessageRoleTool, ToolCallID: "tool-1", Content: "r1"},
		{Role: chat.MessageRoleTool, ToolCallID: "tool-2", Content: "r2"},
		{Role: chat.MessageRoleUser, Content: "ok"},
	}

	converted := convertMessages(msgs)
	// Expect: user(start), assistant(tool_use), user(grouped tool_result), user(ok)
	require.Len(t, converted, 4)

	// Validate sequencing is acceptable to Anthropic
	require.NoError(t, validateAnthropicSequencing(converted))

	// Check the third message (index 2) is a user with tool_result blocks for both tools
	b, err := json.Marshal(converted[2])
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	assert.Equal(t, "user", m["role"])
	content, ok := m["content"].([]any)
	require.True(t, ok)

	// Collect tool_result IDs
	ids := make(map[string]struct{})
	for _, c := range content {
		if cb, ok := c.(map[string]any); ok {
			if cb["type"] == "tool_result" {
				if id, _ := cb["tool_use_id"].(string); id != "" {
					ids[id] = struct{}{}
				}
			}
		}
	}
	assert.Contains(t, ids, "tool-1")
	assert.Contains(t, ids, "tool-2")
}
