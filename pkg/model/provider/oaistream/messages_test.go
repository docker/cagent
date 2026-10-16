package oaistream

import (
	"encoding/json"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/modelsdev"
	"github.com/docker/docker-agent/pkg/tools"
)

func TestConvertMultiContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		multiContent []chat.MessagePart
		wantCount    int
	}{
		{
			name:         "empty",
			multiContent: []chat.MessagePart{},
			wantCount:    0,
		},
		{
			name: "text only",
			multiContent: []chat.MessagePart{
				{Type: chat.MessagePartTypeText, Text: "Hello"},
				{Type: chat.MessagePartTypeText, Text: "World"},
			},
			wantCount: 2,
		},
		{
			name: "with image",
			multiContent: []chat.MessagePart{
				{Type: chat.MessagePartTypeText, Text: "Check this out"},
				{Type: chat.MessagePartTypeImageURL, ImageURL: &chat.MessageImageURL{URL: "http://example.com/img.png"}},
			},
			wantCount: 2,
		},
		{
			name: "image with nil URL produces no part",
			multiContent: []chat.MessagePart{
				{Type: chat.MessagePartTypeImageURL, ImageURL: nil},
			},
			wantCount: 0,
		},
		{
			// The converter forwards all parts as-is; normalizeMessageContent in the
			// session layer strips whitespace-only text parts before real usage.
			name: "whitespace-only text part forwarded as-is",
			multiContent: []chat.MessagePart{
				{Type: chat.MessagePartTypeText, Text: "   "},
				{Type: chat.MessagePartTypeText, Text: "hello"},
			},
			wantCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ConvertMultiContent(t.Context(), tt.multiContent, modelsdev.ID{}, modelsdev.NewDatabaseStore(&modelsdev.Database{}), nil)
			assert.Len(t, result, tt.wantCount)
		})
	}
}

func TestConvertMessages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		messages []chat.Message
		want     int // expected number of converted messages
	}{
		{
			name:     "empty",
			messages: []chat.Message{},
			want:     0,
		},
		{
			name: "system message",
			messages: []chat.Message{
				{Role: chat.MessageRoleSystem, Content: "You are helpful"},
			},
			want: 1,
		},
		{
			name: "user message",
			messages: []chat.Message{
				{Role: chat.MessageRoleUser, Content: "Hello"},
			},
			want: 1,
		},
		{
			name: "assistant message",
			messages: []chat.Message{
				{Role: chat.MessageRoleAssistant, Content: "Hi there!"},
			},
			want: 1,
		},
		{
			name: "skip empty assistant message",
			messages: []chat.Message{
				{Role: chat.MessageRoleAssistant, Content: "   "},
			},
			want: 0,
		},
		{
			name: "assistant with tool calls not skipped",
			messages: []chat.Message{
				{Role: chat.MessageRoleAssistant, Content: "", ToolCalls: []tools.ToolCall{{ID: "1"}}},
			},
			want: 1,
		},
		{
			name: "tool message",
			messages: []chat.Message{
				{Role: chat.MessageRoleTool, Content: "Result", ToolCallID: "call_123"},
			},
			want: 1,
		},
		{
			name: "full conversation",
			messages: []chat.Message{
				{Role: chat.MessageRoleSystem, Content: "You are helpful"},
				{Role: chat.MessageRoleUser, Content: "Hello"},
				{Role: chat.MessageRoleAssistant, Content: "Hi!"},
			},
			want: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ConvertMessages(t.Context(), tt.messages, modelsdev.ID{}, modelsdev.NewDatabaseStore(&modelsdev.Database{}), nil)
			assert.Len(t, result, tt.want)
		})
	}
}

func TestMergeConsecutiveMessages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		messages []openai.ChatCompletionMessageParamUnion
		want     int
	}{
		{
			name:     "empty",
			messages: []openai.ChatCompletionMessageParamUnion{},
			want:     0,
		},
		{
			name: "single message",
			messages: []openai.ChatCompletionMessageParamUnion{
				openai.SystemMessage("Hello"),
			},
			want: 1,
		},
		{
			name: "two consecutive system messages",
			messages: []openai.ChatCompletionMessageParamUnion{
				openai.SystemMessage("You are helpful"),
				openai.SystemMessage("Be concise"),
			},
			want: 1,
		},
		{
			name: "two consecutive user messages",
			messages: []openai.ChatCompletionMessageParamUnion{
				openai.UserMessage("Hello"),
				openai.UserMessage("World"),
			},
			want: 1,
		},
		{
			name: "alternating roles",
			messages: []openai.ChatCompletionMessageParamUnion{
				openai.SystemMessage("System"),
				openai.UserMessage("User"),
				openai.UserMessage("User2"),
			},
			want: 2, // System merged alone, two users merged
		},
		{
			name: "no merging needed",
			messages: []openai.ChatCompletionMessageParamUnion{
				openai.SystemMessage("System"),
				openai.UserMessage("User"),
			},
			want: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := MergeConsecutiveMessages(tt.messages)
			assert.Len(t, result, tt.want)
		})
	}
}

func TestJSONSchema_MarshalJSON(t *testing.T) {
	t.Parallel()

	schema := JSONSchema{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type": "string",
			},
		},
	}

	data, err := schema.MarshalJSON()
	require.NoError(t, err)
	assert.Contains(t, string(data), `"type":"object"`)
	assert.Contains(t, string(data), `"properties"`)
}

func TestToolResultAttachmentsFollowTheWholeGroup(t *testing.T) {
	t.Parallel()

	image := func(id, url, text string) chat.Message {
		return chat.Message{
			Role:       chat.MessageRoleTool,
			ToolCallID: id,
			MultiContent: []chat.MessagePart{
				{Type: chat.MessagePartTypeText, Text: text},
				{Type: chat.MessagePartTypeImageURL, ImageURL: &chat.MessageImageURL{URL: url}},
			},
		}
	}
	textOnly := func(id, text string) chat.Message {
		return chat.Message{Role: chat.MessageRoleTool, ToolCallID: id, Content: text}
	}
	assistant := chat.Message{
		Role: chat.MessageRoleAssistant,
		ToolCalls: []tools.ToolCall{
			{ID: "call_0", Function: tools.FunctionCall{Name: "read_file"}},
			{ID: "call_1", Function: tools.FunctionCall{Name: "read_file"}},
		},
	}

	convert := func(t *testing.T, messages []chat.Message) []openai.ChatCompletionMessageParamUnion {
		t.Helper()
		return ConvertMessages(t.Context(), messages, modelsdev.ID{}, modelsdev.NewDatabaseStore(&modelsdev.Database{}), nil)
	}
	roles := func(msgs []openai.ChatCompletionMessageParamUnion) []string {
		out := make([]string, len(msgs))
		for i, msg := range msgs {
			out[i] = getMessageRole(msg)
		}
		return out
	}
	imageURL := func(t *testing.T, msg openai.ChatCompletionMessageParamUnion) string {
		t.Helper()
		require.NotNil(t, msg.OfUser)
		raw, err := json.Marshal(msg.OfUser.Content)
		require.NoError(t, err)
		var parts []struct {
			Type     string `json:"type"`
			ImageURL struct {
				URL string `json:"url"`
			} `json:"image_url"`
		}
		require.NoError(t, json.Unmarshal(raw, &parts))
		for _, part := range parts {
			if part.Type == "image_url" && part.ImageURL.URL != "" {
				return part.ImageURL.URL
			}
		}
		t.Fatalf("no image url in %s", raw)
		return ""
	}

	t.Run("two images stay after both tool results", func(t *testing.T) {
		t.Parallel()
		got := convert(t, []chat.Message{
			assistant,
			image("call_1", "https://example.test/1.png", "one"),
			image("call_0", "https://example.test/0.png", "zero"),
		})
		assert.Equal(t, []string{"assistant", "tool", "tool", "user", "user"}, roles(got))
		assert.Equal(t, "call_1", got[1].OfTool.ToolCallID)
		assert.Equal(t, "call_0", got[2].OfTool.ToolCallID)
		assert.Equal(t, "https://example.test/1.png", imageURL(t, got[3]))
		assert.Equal(t, "https://example.test/0.png", imageURL(t, got[4]))
	})

	t.Run("text result does not split the image follow-up", func(t *testing.T) {
		t.Parallel()
		got := convert(t, []chat.Message{
			assistant,
			image("call_1", "https://example.test/1.png", "one"),
			textOnly("call_0", "plain"),
		})
		assert.Equal(t, []string{"assistant", "tool", "tool", "user"}, roles(got))
		assert.Equal(t, "call_0", got[2].OfTool.ToolCallID)
		assert.Equal(t, "https://example.test/1.png", imageURL(t, got[3]))
	})

	t.Run("single image is still tool then user", func(t *testing.T) {
		t.Parallel()
		got := convert(t, []chat.Message{
			{Role: chat.MessageRoleAssistant, ToolCalls: []tools.ToolCall{{ID: "call_0"}}},
			image("call_0", "https://example.test/only.png", "only"),
		})
		assert.Equal(t, []string{"assistant", "tool", "user"}, roles(got))
		assert.Equal(t, "https://example.test/only.png", imageURL(t, got[2]))
	})

	t.Run("a later user message stays after the attachments", func(t *testing.T) {
		t.Parallel()
		got := convert(t, []chat.Message{
			assistant,
			image("call_1", "https://example.test/1.png", "one"),
			textOnly("call_0", "plain"),
			{Role: chat.MessageRoleUser, Content: "thanks"},
		})
		assert.Equal(t, []string{"assistant", "tool", "tool", "user", "user"}, roles(got))
		assert.Equal(t, "https://example.test/1.png", imageURL(t, got[3]))
		text, ok := getStringContent(got[4])
		require.True(t, ok)
		assert.Equal(t, "thanks", text)
	})
}
