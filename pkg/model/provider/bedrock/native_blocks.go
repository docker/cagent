package bedrock

import (
	"encoding/json"
	"maps"
	"slices"

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
	input   []byte
	stopped bool
}

// blockRecorder accumulates content blocks by ContentBlockIndex. Reasoning
// blocks may start with a delta, so any event creates its block.
type blockRecorder struct {
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
		b.block.Text += d.Value
	case *types.ContentBlockDeltaMemberToolUse:
		b := r.get(idx, "tool_use")
		if d.Value.Input != nil {
			b.input = append(b.input, *d.Value.Input...)
		}
	case *types.ContentBlockDeltaMemberReasoningContent:
		switch rd := d.Value.(type) {
		case *types.ReasoningContentBlockDeltaMemberText:
			b := r.get(idx, "reasoning")
			b.block.Text += rd.Value
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
	return &chat.ProviderState{Provider: providerName, Content: content}
}
