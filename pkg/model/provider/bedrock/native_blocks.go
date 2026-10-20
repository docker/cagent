package bedrock

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"

	"github.com/docker/docker-agent/pkg/chat"
)

// providerName tags the [chat.ProviderState] recorded from Converse responses.
const providerName = "amazon-bedrock"

// nativeBlock is one Converse content block, kept exactly as streamed so
// signed and redacted reasoning can be replayed verbatim.
type nativeBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Signature string          `json:"signature,omitempty"`
	Data      []byte          `json:"data,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
}

type recordedBlock struct {
	block   nativeBlock
	text    strings.Builder
	input   []byte
	stopped bool
}

// blockRecorder accumulates content blocks by ContentBlockIndex. Reasoning
// blocks may start with a delta, so any event creates its block.
type blockRecorder struct {
	model  string
	blocks map[int32]*recordedBlock
}

func (r *blockRecorder) get(idx int32, typ string) *recordedBlock {
	if r.blocks == nil {
		r.blocks = map[int32]*recordedBlock{}
	}
	b, ok := r.blocks[idx]
	if !ok {
		b = &recordedBlock{block: nativeBlock{Type: typ}}
		r.blocks[idx] = b
	}
	return b
}

func (r *blockRecorder) start(idx int32, toolID, toolName string) {
	b := r.get(idx, "tool_use")
	b.block.ID, b.block.Name = toolID, toolName
}

func (r *blockRecorder) delta(idx int32, d types.ContentBlockDelta) {
	switch d := d.(type) {
	case *types.ContentBlockDeltaMemberText:
		b := r.get(idx, "text")
		b.text.WriteString(d.Value)
	case *types.ContentBlockDeltaMemberToolUse:
		b := r.get(idx, "tool_use")
		if d.Value.Input != nil {
			b.input = append(b.input, *d.Value.Input...)
		}
	case *types.ContentBlockDeltaMemberReasoningContent:
		switch rd := d.Value.(type) {
		case *types.ReasoningContentBlockDeltaMemberText:
			b := r.get(idx, "reasoning")
			b.text.WriteString(rd.Value)
		case *types.ReasoningContentBlockDeltaMemberSignature:
			b := r.get(idx, "reasoning")
			b.block.Signature += rd.Value
		case *types.ReasoningContentBlockDeltaMemberRedactedContent:
			b := r.get(idx, "redacted_reasoning")
			b.block.Data = append(b.block.Data, rd.Value...)
		}
	}
}

func (r *blockRecorder) stop(idx int32) {
	if b, ok := r.blocks[idx]; ok {
		b.stopped = true
	}
}

// state returns the recorded blocks in index order, or nil when nothing was
// recorded or a block is incomplete: partial state is never replayed.
func (r *blockRecorder) state() *chat.ProviderState {
	if len(r.blocks) == 0 {
		return nil
	}
	blocks := make([]nativeBlock, 0, len(r.blocks))
	for _, idx := range slices.Sorted(maps.Keys(r.blocks)) {
		b := r.blocks[idx]
		if !b.stopped {
			return nil
		}
		b.block.Text = b.text.String()
		if b.block.Type == "tool_use" {
			input := b.input
			if len(input) == 0 {
				input = []byte("{}")
			}
			if !json.Valid(input) {
				return nil
			}
			b.block.Input = input
		}
		blocks = append(blocks, b.block)
	}
	content, err := json.Marshal(blocks)
	if err != nil {
		return nil
	}
	return &chat.ProviderState{Provider: providerName, Version: chat.ProviderStateVersion, Model: r.model, Content: content}
}

// replayNativeBlocks rebuilds the recorded Converse blocks when the message
// still matches them. ok is false when there is no replayable state, so the
// caller falls back to the flattened fields.
func replayNativeBlocks(msg *chat.Message) ([]types.ContentBlock, bool) {
	state := msg.ReplayableProviderState(providerName)
	if state == nil {
		return nil, false
	}
	var blocks []nativeBlock
	if err := json.Unmarshal(state.Content, &blocks); err != nil || !nativeBlocksDescribe(msg, blocks) {
		return nil, false
	}
	return toContentBlocks(blocks)
}

// leadingReasoning returns the reasoning blocks that open msg's own recorded
// response when they still match its flattened thinking. A runtime transform
// (sanitized tool names, redacted arguments) changes the rest of the turn but
// not its signed reasoning, which can then be replayed before the current text
// and tool calls. ok is false when the reasoning changed or is interleaved
// with other blocks.
func leadingReasoning(msg *chat.Message) ([]types.ContentBlock, bool) {
	if !msg.HasOwnRawContent(providerName) {
		return nil, false
	}
	var blocks []nativeBlock
	if json.Unmarshal(msg.ProviderState.Content, &blocks) != nil {
		return nil, false
	}
	isReasoning := func(b nativeBlock) bool { return b.Type == "reasoning" || b.Type == "redacted_reasoning" }
	n := 0
	for n < len(blocks) && isReasoning(blocks[n]) {
		n++
	}
	if n == 0 || slices.ContainsFunc(blocks[n:], isReasoning) {
		return nil, false
	}
	var text strings.Builder
	var signature string
	for _, b := range blocks[:n] {
		if b.Type == "reasoning" {
			text.WriteString(b.Text)
			if b.Signature != "" {
				signature = b.Signature
			}
		}
	}
	if text.String() != msg.ReasoningContent || signature != msg.ThinkingSignature {
		return nil, false
	}
	out, ok := toContentBlocks(blocks[:n])
	return out, ok && len(out) > 0
}

// toContentBlocks converts recorded blocks to request blocks. Unsigned
// reasoning (cut off by max_tokens, or another vendor's) cannot be verified
// by the API and is skipped. ok is false for an unknown or malformed block.
func toContentBlocks(blocks []nativeBlock) ([]types.ContentBlock, bool) {
	out := make([]types.ContentBlock, 0, len(blocks))
	for _, b := range blocks {
		switch b.Type {
		case "reasoning":
			if b.Signature == "" {
				continue
			}
			out = append(out, &types.ContentBlockMemberReasoningContent{
				Value: &types.ReasoningContentBlockMemberReasoningText{
					Value: types.ReasoningTextBlock{Text: new(b.Text), Signature: new(b.Signature)},
				},
			})
		case "redacted_reasoning":
			out = append(out, &types.ContentBlockMemberReasoningContent{
				Value: &types.ReasoningContentBlockMemberRedactedContent{Value: b.Data},
			})
		case "text":
			out = append(out, &types.ContentBlockMemberText{Value: b.Text})
		case "tool_use":
			var input map[string]any
			if err := json.Unmarshal(b.Input, &input); err != nil {
				return nil, false
			}
			if input == nil {
				input = map[string]any{}
			}
			out = append(out, &types.ContentBlockMemberToolUse{
				Value: types.ToolUseBlock{ToolUseId: new(b.ID), Name: new(b.Name), Input: mapToDocument(input)},
			})
		default:
			return nil, false
		}
	}
	return out, true
}

// nativeBlocksDescribe reports whether the blocks still match the message as
// the runtime accumulated it: concatenated text and reasoning, and the same
// tool calls in order with JSON-equal arguments.
func nativeBlocksDescribe(msg *chat.Message, blocks []nativeBlock) bool {
	var text, reasoning string
	var toolUses []nativeBlock
	for _, b := range blocks {
		switch b.Type {
		case "text":
			text += b.Text
		case "reasoning":
			reasoning += b.Text
		case "tool_use":
			toolUses = append(toolUses, b)
		}
	}
	if text != msg.Content || reasoning != msg.ReasoningContent || len(toolUses) != len(msg.ToolCalls) {
		return false
	}
	for i, tc := range msg.ToolCalls {
		if tc.ID != toolUses[i].ID || tc.Function.Name != toolUses[i].Name || !sameJSON(tc.Function.Arguments, toolUses[i].Input) {
			return false
		}
	}
	return true
}

func sameJSON(arguments string, input json.RawMessage) bool {
	if arguments == "" {
		arguments = "{}"
	}
	var a, b any
	if json.Unmarshal([]byte(arguments), &a) != nil || json.Unmarshal(input, &b) != nil {
		return false
	}
	return reflect.DeepEqual(a, b)
}

// checkReplayableThinking refuses a thinking request whose active tool loop
// holds an assistant turn whose reasoning changed since it was recorded: the
// API requires that turn's signed thinking, which can no longer be replayed.
func checkReplayableThinking(model string, thinks bool, messages []chat.Message) error {
	if !thinks {
		return nil
	}
	for i := chat.ActiveToolLoopStart(messages); i < len(messages); i++ {
		msg := &messages[i]
		if msg.Role != chat.MessageRoleAssistant || msg.ThinkingSignature == "" || !msg.HasOwnRawContent(providerName) {
			continue
		}
		if _, ok := replayNativeBlocks(msg); ok {
			continue
		}
		if _, ok := leadingReasoning(msg); !ok {
			return fmt.Errorf("%s: the thinking of an assistant turn in the current tool loop changed after it was recorded and cannot be replayed; start a new user turn to continue", model)
		}
	}
	return nil
}
