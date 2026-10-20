package bedrock

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/tools"
)

// sealedMessage builds the message the runtime accumulates from these blocks
// (concatenated text and reasoning, last signature) and seals the raw state.
func sealedMessage(t *testing.T, blocks []nativeBlock) chat.Message {
	t.Helper()
	msg := chat.Message{Role: chat.MessageRoleAssistant}
	for _, b := range blocks {
		switch b.Type {
		case "text":
			msg.Content += b.Text
		case "reasoning":
			msg.ReasoningContent += b.Text
			msg.ThinkingSignature = b.Signature
		case "tool_use":
			msg.ToolCalls = append(msg.ToolCalls, tools.ToolCall{ID: b.ID, Type: "function", Function: tools.FunctionCall{Name: b.Name, Arguments: string(b.Input)}})
		}
	}
	content, err := json.Marshal(blocks)
	require.NoError(t, err)
	msg.AttachProviderState(&chat.ProviderState{Provider: providerName, Content: content})
	return msg
}

var interleaved = []nativeBlock{
	{Type: "reasoning", Text: "a", Signature: "s1"},
	{Type: "text", Text: "hi"},
	{Type: "tool_use", ID: "t1", Name: "f", Input: json.RawMessage(`{"x":1}`)},
	{Type: "reasoning", Signature: "s2"},
}

func reasoningOf(t *testing.T, b types.ContentBlock) types.ReasoningContentBlock {
	t.Helper()
	r, ok := b.(*types.ContentBlockMemberReasoningContent)
	require.True(t, ok, "expected a reasoning block, got %T", b)
	return r.Value
}

func TestConvertAssistantContent_ReplaysNativeBlocks(t *testing.T) {
	t.Parallel()

	msg := sealedMessage(t, interleaved)
	blocks := convertAssistantContent(&msg)

	require.Len(t, blocks, 4)
	first, ok := reasoningOf(t, blocks[0]).(*types.ReasoningContentBlockMemberReasoningText)
	require.True(t, ok)
	assert.Equal(t, "a", *first.Value.Text)
	assert.Equal(t, "s1", *first.Value.Signature)

	text, ok := blocks[1].(*types.ContentBlockMemberText)
	require.True(t, ok)
	assert.Equal(t, "hi", text.Value)

	tool, ok := blocks[2].(*types.ContentBlockMemberToolUse)
	require.True(t, ok)
	assert.Equal(t, "t1", *tool.Value.ToolUseId)
	assert.Equal(t, "f", *tool.Value.Name)

	last, ok := reasoningOf(t, blocks[3]).(*types.ReasoningContentBlockMemberReasoningText)
	require.True(t, ok)
	assert.Empty(t, *last.Value.Text)
	assert.Equal(t, "s2", *last.Value.Signature)
}

func TestConvertAssistantContent_RedactedBytesSurvive(t *testing.T) {
	t.Parallel()

	data := []byte{0xff, 0x00, 0x80}
	msg := sealedMessage(t, []nativeBlock{{Type: "redacted_reasoning", Data: data}, {Type: "text", Text: "ok"}})

	raw, err := json.Marshal(msg)
	require.NoError(t, err)
	var reloaded chat.Message
	require.NoError(t, json.Unmarshal(raw, &reloaded))

	blocks := convertAssistantContent(&reloaded)
	require.Len(t, blocks, 2)
	redacted, ok := reasoningOf(t, blocks[0]).(*types.ReasoningContentBlockMemberRedactedContent)
	require.True(t, ok)
	assert.Equal(t, data, redacted.Value)
}

func TestConvertAssistantContent_FallsBackWhenEdited(t *testing.T) {
	t.Parallel()

	msg := sealedMessage(t, interleaved)
	msg.Content = "edited"

	flattened := msg
	flattened.ProviderState = nil
	assert.Equal(t, convertAssistantContent(&flattened), convertAssistantContent(&msg))
}

// Unsigned reasoning (thinking cut off by max_tokens, or another vendor's
// reasoning on Bedrock) cannot be verified, so it is never replayed. It still
// counts toward the message match, as on the Anthropic replay path.
func TestConvertAssistantContent_SkipsUnsignedReasoning(t *testing.T) {
	t.Parallel()

	msg := sealedMessage(t, []nativeBlock{
		{Type: "reasoning", Text: "a", Signature: "s1"},
		{Type: "reasoning", Text: "cut"},
		{Type: "text", Text: "hi"},
	})
	blocks := convertAssistantContent(&msg)

	require.Len(t, blocks, 2)
	signed, ok := reasoningOf(t, blocks[0]).(*types.ReasoningContentBlockMemberReasoningText)
	require.True(t, ok)
	assert.Equal(t, "s1", *signed.Value.Signature)
	_, ok = blocks[1].(*types.ContentBlockMemberText)
	assert.True(t, ok)
}

// Not parallel: AllocsPerRun counts process-wide allocations.
func TestBlockRecorder_AccumulatesDeltasWithoutPerDeltaCopies(t *testing.T) {
	const n = 1000
	textDelta := &types.ContentBlockDeltaMemberText{Value: "x"}
	reasoningDelta := &types.ContentBlockDeltaMemberReasoningContent{Value: &types.ReasoningContentBlockDeltaMemberText{Value: "y"}}

	var r blockRecorder
	allocs := testing.AllocsPerRun(1, func() {
		r = blockRecorder{}
		for range n {
			r.delta(0, reasoningDelta)
			r.delta(1, textDelta)
		}
	})
	assert.Less(t, allocs, float64(n/10), "deltas must not copy the accumulated block")

	r.delta(0, &types.ContentBlockDeltaMemberReasoningContent{Value: &types.ReasoningContentBlockDeltaMemberSignature{Value: "sig"}})
	r.stop(0)
	r.stop(1)
	state := r.state()
	require.NotNil(t, state)
	var blocks []nativeBlock
	require.NoError(t, json.Unmarshal(state.Content, &blocks))
	require.Len(t, blocks, 2)
	assert.Equal(t, nativeBlock{Type: "reasoning", Text: strings.Repeat("y", n), Signature: "sig"}, blocks[0])
	assert.Equal(t, nativeBlock{Type: "text", Text: strings.Repeat("x", n)}, blocks[1])
}
