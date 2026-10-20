package anthropic

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/tools"
)

// providerStateName tags chat.ProviderState recorded by this provider. The
// standard and Beta Messages APIs share the content block wire format, so
// both adapters record under the same name and either converter can replay.
const providerStateName = "anthropic"

// newProviderState captures the response content blocks of an accumulated
// Message or BetaMessage in wire order. Block JSON is only final once the
// SDK refreshed it on message_stop, so call this from the stop event. Returns
// nil for a response without content.
func newProviderState[T interface{ RawJSON() string }](messageID, model string, blocks []T) *chat.ProviderState {
	raws := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if raw := block.RawJSON(); raw != "" {
			raws = append(raws, raw)
		}
	}
	if len(raws) == 0 {
		return nil
	}
	return &chat.ProviderState{
		Provider:  providerStateName,
		Version:   chat.ProviderStateVersion,
		Model:     model,
		MessageID: messageID,
		Content:   json.RawMessage("[" + strings.Join(raws, ",") + "]"),
	}
}

// replayContent rebuilds the assistant content of msg from its raw Anthropic
// response, preserving every thinking, redacted_thinking, text and tool_use
// block in wire order. ok is false when msg has nothing replayable: no state,
// state from another provider, visible fields edited since ingestion, or raw
// blocks that no longer describe the flattened message (see describes).
// Callers then fall back to the flattened fields. A non-nil error means the
// state is ours but malformed.
func replayContent(msg *chat.Message) ([]anthropic.ContentBlockParamUnion, bool, error) {
	state := msg.ReplayableProviderState(providerStateName)
	if state == nil {
		return nil, false, nil
	}
	var blocks []anthropic.ContentBlockUnion
	if err := json.Unmarshal(state.Content, &blocks); err != nil {
		return nil, false, fmt.Errorf("anthropic: decoding raw response content: %w", err)
	}
	views := make([]rawBlock, len(blocks))
	for i, b := range blocks {
		views[i] = rawBlock{Type: b.Type, Text: b.Text, Thinking: b.Thinking, Signature: b.Signature, ID: b.ID, Name: b.Name, Input: b.Input}
	}
	if !describes(msg, views) {
		return nil, false, nil
	}
	params := make([]anthropic.ContentBlockParamUnion, 0, len(blocks))
	for i, b := range blocks {
		if views[i].skip() {
			continue
		}
		if b.AsAny() == nil {
			return nil, false, fmt.Errorf("anthropic: unsupported content block type %q", b.Type)
		}
		params = append(params, b.ToParam())
	}
	if len(params) == 0 {
		return nil, false, nil
	}
	return params, true, nil
}

// betaReplayContent is replayContent for the Beta Messages API.
func betaReplayContent(msg *chat.Message) ([]anthropic.BetaContentBlockParamUnion, bool, error) {
	state := msg.ReplayableProviderState(providerStateName)
	if state == nil {
		return nil, false, nil
	}
	var blocks []anthropic.BetaContentBlockUnion
	if err := json.Unmarshal(state.Content, &blocks); err != nil {
		return nil, false, fmt.Errorf("anthropic: decoding raw response content: %w", err)
	}
	views := make([]rawBlock, len(blocks))
	for i, b := range blocks {
		views[i] = rawBlock{Type: b.Type, Text: b.Text, Thinking: b.Thinking, Signature: b.Signature, ID: b.ID, Name: b.Name, Input: b.Input}
	}
	if !describes(msg, views) {
		return nil, false, nil
	}
	params := make([]anthropic.BetaContentBlockParamUnion, 0, len(blocks))
	for i, b := range blocks {
		if views[i].skip() {
			continue
		}
		if b.AsAny() == nil {
			return nil, false, fmt.Errorf("anthropic: unsupported content block type %q", b.Type)
		}
		params = append(params, b.ToParam())
	}
	if len(params) == 0 {
		return nil, false, nil
	}
	return params, true, nil
}

// rawBlock is the API-agnostic view of a content block the replay guards need.
type rawBlock struct {
	Type, Text, Thinking, Signature, ID, Name string
	Input                                     json.RawMessage
}

// skip reports blocks the API rejects on replay: empty text blocks and
// thinking blocks that never received a signature (truncated turn).
func (b rawBlock) skip() bool {
	switch b.Type {
	case "text":
		return b.Text == ""
	case "thinking":
		return b.Signature == ""
	}
	return false
}

// describes reports whether the raw blocks still describe the flattened
// message: text and thinking blocks concatenate to Content and
// ReasoningContent, and the tool_use blocks are exactly ToolCalls in order,
// with the same names and arguments. ContentHash only catches edits after
// ingestion; this catches what the runtime changed before sealing (media
// markers stripped from the text, sanitized tool names) and calls a refusal
// dropped or the XML fallback added, none of which may be replayed raw.
func describes(msg *chat.Message, blocks []rawBlock) bool {
	var text, thinking strings.Builder
	calls := 0
	for _, b := range blocks {
		switch b.Type {
		case "text":
			text.WriteString(b.Text)
		case "thinking":
			thinking.WriteString(b.Thinking)
		case "tool_use":
			if calls >= len(msg.ToolCalls) || !toolUseMatches(msg.ToolCalls[calls], b) {
				return false
			}
			calls++
		}
	}
	return calls == len(msg.ToolCalls) && text.String() == msg.Content && thinking.String() == msg.ReasoningContent
}

func toolUseMatches(call tools.ToolCall, b rawBlock) bool {
	return call.ID == b.ID && call.Function.Name == b.Name && reflect.DeepEqual(toolInput(call.Function.Arguments), toolInput(string(b.Input)))
}

// toolInput decodes tool arguments the way the converters send them: invalid
// or empty JSON becomes an empty object.
func toolInput(raw string) any {
	var v any
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&v); err != nil || v == nil {
		return map[string]any{}
	}
	return v
}

// thinkingPrefix returns the thinking blocks that open msg's own recorded
// response when they still match its flattened thinking. A runtime transform
// (sanitized tool names, redacted arguments) changes the rest of the turn but
// not its signed thinking, which can then be replayed before the current text
// and tool calls. ok is false when the thinking changed or is interleaved with
// other blocks.
func thinkingPrefix(msg *chat.Message) ([]anthropic.ContentBlockParamUnion, bool) {
	if !msg.HasOwnRawContent(providerStateName) {
		return nil, false
	}
	var blocks []anthropic.ContentBlockUnion
	if json.Unmarshal(msg.ProviderState.Content, &blocks) != nil {
		return nil, false
	}
	views := make([]rawBlock, len(blocks))
	for i, b := range blocks {
		views[i] = rawBlock{Type: b.Type, Thinking: b.Thinking, Signature: b.Signature}
	}
	n, ok := intactLeadingThinking(msg, views)
	if !ok {
		return nil, false
	}
	params := make([]anthropic.ContentBlockParamUnion, 0, n)
	for i := range n {
		if views[i].skip() {
			continue
		}
		if blocks[i].AsAny() == nil {
			return nil, false
		}
		params = append(params, blocks[i].ToParam())
	}
	return params, len(params) > 0
}

// betaThinkingPrefix is thinkingPrefix for the Beta Messages API.
func betaThinkingPrefix(msg *chat.Message) ([]anthropic.BetaContentBlockParamUnion, bool) {
	if !msg.HasOwnRawContent(providerStateName) {
		return nil, false
	}
	var blocks []anthropic.BetaContentBlockUnion
	if json.Unmarshal(msg.ProviderState.Content, &blocks) != nil {
		return nil, false
	}
	views := make([]rawBlock, len(blocks))
	for i, b := range blocks {
		views[i] = rawBlock{Type: b.Type, Thinking: b.Thinking, Signature: b.Signature}
	}
	n, ok := intactLeadingThinking(msg, views)
	if !ok {
		return nil, false
	}
	params := make([]anthropic.BetaContentBlockParamUnion, 0, n)
	for i := range n {
		if views[i].skip() {
			continue
		}
		if blocks[i].AsAny() == nil {
			return nil, false
		}
		params = append(params, blocks[i].ToParam())
	}
	return params, len(params) > 0
}

// intactLeadingThinking returns the number of thinking and redacted_thinking
// blocks that open the recorded response. ok is true when they concatenate to
// msg's flattened thinking, end with its signature, and no thinking follows
// the other blocks.
func intactLeadingThinking(msg *chat.Message, views []rawBlock) (int, bool) {
	isThinking := func(b rawBlock) bool { return b.Type == "thinking" || b.Type == "redacted_thinking" }
	n := 0
	for n < len(views) && isThinking(views[n]) {
		n++
	}
	if n == 0 || slices.ContainsFunc(views[n:], isThinking) {
		return 0, false
	}
	var thinking strings.Builder
	var signature string
	for _, b := range views[:n] {
		if b.Type == "thinking" {
			thinking.WriteString(b.Thinking)
			if b.Signature != "" {
				signature = b.Signature
			}
		}
	}
	return n, thinking.String() == msg.ReasoningContent && signature == msg.ThinkingSignature
}

// flattenedThinking reports whether a non-replayable assistant turn may carry
// its flattened thinking. A turn with its own raw content whose thinking
// changed since it was recorded loses that thinking, except inside the active
// tool loop of a thinking request, where the API requires it and the request
// is refused.
func (c *Client) flattenedThinking(i, loopStart int, msg *chat.Message) (bool, error) {
	if !msg.HasOwnRawContent(providerStateName) {
		return msg.ThinkingSignature != "", nil
	}
	if msg.ThinkingSignature == "" && msg.ReasoningContent == "" {
		return false, nil
	}
	if i >= loopStart && msg.ThinkingSignature != "" && c.requestThinks() {
		return false, fmt.Errorf("%s: the thinking of an assistant turn in the current tool loop changed after it was recorded and cannot be replayed; start a new user turn to continue", c.ModelConfig.Model)
	}
	slog.Warn("Anthropic: omitting thinking that changed after it was recorded", "index", i)
	return false, nil
}
