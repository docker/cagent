package messages

import (
	"testing"

	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/tui/types"
)

func TestPlainTextTranscript(t *testing.T) {
	m := &model{
		messages: []types.Message{
			{Type: types.MessageTypeUser, Content: "Hello"},
			{Type: types.MessageTypeAssistant, Sender: "helper", Content: "Hi"},
			{Type: types.MessageTypeAssistantReasoning, Sender: "helper", Content: "Thinking"},
			{
				Type:   types.MessageTypeToolCall,
				Sender: "helper",
				ToolCall: tools.ToolCall{Function: tools.FunctionCall{
					Name:      "search",
					Arguments: `{"q":"test"}`,
				}},
			},
			{
				Type:     types.MessageTypeToolResult,
				ToolCall: tools.ToolCall{Function: tools.FunctionCall{Name: "search"}},
				Content:  "Result",
			},
			{Type: types.MessageTypeError, Content: "Oops"},
			{Type: types.MessageTypeSystem, Content: "Should be ignored"},
		},
	}

	expected := `User:
Hello

helper:
Hi

helper (thinking):
Thinking

Tool Call (search):
helper invoked search
Arguments:
{"q":"test"}

Tool Result (search):
Result

Error:
Oops`
	if got := m.PlainTextTranscript(); got != expected {
		t.Fatalf("unexpected transcript:\nexpected:\n%q\n\ngot:\n%q", expected, got)
	}
}
