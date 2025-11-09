package anthropic

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
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

// TestCountAnthropicTokens_Success tests successful token counting for standard API
func TestCountAnthropicTokens_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/messages/count_tokens", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("content-type"))
		assert.NotEmpty(t, r.Header.Get("x-api-key"))

		var payload map[string]any
		err := json.NewDecoder(r.Body).Decode(&payload)
		assert.NoError(t, err)
		assert.Equal(t, "claude-3-5-sonnet-20241022", payload["model"])
		assert.NotNil(t, payload["messages"])

		// Return mock response
		w.Header().Set("content-type", "application/json")
		err = json.NewEncoder(w).Encode(map[string]int64{"input_tokens": 150})
		assert.NoError(t, err)
	}))
	defer server.Close()

	messages := []anthropic.MessageParam{
		{
			Role: anthropic.MessageParamRoleUser,
			Content: []anthropic.ContentBlockParamUnion{
				{OfText: &anthropic.TextBlockParam{Text: "Hello"}},
			},
		},
	}
	system := []anthropic.TextBlockParam{
		{Text: "You are helpful"},
	}

	client := anthropic.NewClient(
		option.WithAPIKey("test-key"),
		option.WithBaseURL(server.URL),
	)

	tokens, err := countAnthropicTokens(t.Context(), client, "claude-3-5-sonnet-20241022", messages, system, nil)

	require.NoError(t, err)
	assert.Equal(t, int64(150), tokens)
}

// TestCountAnthropicTokens_NoAPIKey tests error when API key is missing
func TestCountAnthropicTokens_NoAPIKey(t *testing.T) {
	messages := []anthropic.MessageParam{}
	system := []anthropic.TextBlockParam{}

	client := anthropic.NewClient(
		option.WithAPIKey("test-key"),
		// No base URL set
	)

	tokens, err := countAnthropicTokens(t.Context(), client, "claude-3-5-sonnet-20241022", messages, system, nil)

	require.Error(t, err)
	assert.Equal(t, int64(0), tokens)
}

// TestCountAnthropicTokens_ServerError tests error handling for server errors
func TestCountAnthropicTokens_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	messages := []anthropic.MessageParam{}
	system := []anthropic.TextBlockParam{}

	client := anthropic.NewClient(
		option.WithAPIKey("test-key"),
		option.WithBaseURL(server.URL),
	)

	tokens, err := countAnthropicTokens(t.Context(), client, "claude-3-5-sonnet-20241022", messages, system, nil)

	require.Error(t, err)
	assert.Equal(t, int64(0), tokens)
}

// TestCountAnthropicTokens_WithTools tests token counting includes tools
func TestCountAnthropicTokens_WithTools(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		err := json.NewDecoder(r.Body).Decode(&payload)
		assert.NoError(t, err)

		assert.NotNil(t, payload["tools"])
		tools, ok := payload["tools"].([]any)
		assert.True(t, ok)
		assert.Len(t, tools, 1)

		w.Header().Set("content-type", "application/json")
		err = json.NewEncoder(w).Encode(map[string]int64{"input_tokens": 200})
		assert.NoError(t, err)
	}))
	defer server.Close()

	messages := []anthropic.MessageParam{}
	system := []anthropic.TextBlockParam{}
	aiTools := []anthropic.ToolUnionParam{
		{OfTool: &anthropic.ToolParam{
			Name:        "test_tool",
			Description: anthropic.String("A test tool"),
		}},
	}

	client := anthropic.NewClient(
		option.WithAPIKey("test-key"),
		option.WithBaseURL(server.URL),
	)

	tokens, err := countAnthropicTokens(t.Context(), client, "claude-3-5-sonnet-20241022", messages, system, aiTools)

	require.NoError(t, err)
	assert.Equal(t, int64(200), tokens)
}

// TestExtractSystemBlocks_SingleSystemMessage tests extracting system messages
func TestExtractSystemBlocks_SingleSystemMessage(t *testing.T) {
	msgs := []chat.Message{
		{
			Role:    chat.MessageRoleSystem,
			Content: "You are a helpful assistant",
		},
	}

	blocks := extractSystemBlocks(msgs)

	require.Len(t, blocks, 1)
	assert.Equal(t, "You are a helpful assistant", blocks[0].Text)
}

// TestExtractSystemBlocks_MultipleSystemMessages tests extracting multiple system messages
func TestExtractSystemBlocks_MultipleSystemMessages(t *testing.T) {
	msgs := []chat.Message{
		{
			Role:    chat.MessageRoleSystem,
			Content: "You are helpful",
		},
		{
			Role:    chat.MessageRoleUser,
			Content: "Hello",
		},
		{
			Role:    chat.MessageRoleSystem,
			Content: "Be concise",
		},
	}

	blocks := extractSystemBlocks(msgs)

	require.Len(t, blocks, 2)
	assert.Equal(t, "You are helpful", blocks[0].Text)
	assert.Equal(t, "Be concise", blocks[1].Text)
}

// TestExtractSystemBlocks_SkipsEmptyText tests that empty system text is skipped
func TestExtractSystemBlocks_SkipsEmptyText(t *testing.T) {
	msgs := []chat.Message{
		{
			Role:    chat.MessageRoleSystem,
			Content: "   \n\t  ",
		},
		{
			Role:    chat.MessageRoleSystem,
			Content: "Valid system prompt",
		},
	}

	blocks := extractSystemBlocks(msgs)

	require.Len(t, blocks, 1)
	assert.Equal(t, "Valid system prompt", blocks[0].Text)
}

// TestExtractSystemBlocks_MultiContent tests extracting from multi-content system messages
func TestExtractSystemBlocks_MultiContent(t *testing.T) {
	msgs := []chat.Message{
		{
			Role: chat.MessageRoleSystem,
			MultiContent: []chat.MessagePart{
				{Type: chat.MessagePartTypeText, Text: "Part 1"},
				{Type: chat.MessagePartTypeText, Text: "Part 2"},
			},
		},
	}

	blocks := extractSystemBlocks(msgs)

	require.Len(t, blocks, 2)
	assert.Equal(t, "Part 1", blocks[0].Text)
	assert.Equal(t, "Part 2", blocks[1].Text)
}
