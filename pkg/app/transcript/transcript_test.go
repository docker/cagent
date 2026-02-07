package transcript

import (
	"testing"

	"gotest.tools/v3/golden"

	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/tools"
)

func TestSimple(t *testing.T) {
	sess := session.New(session.WithUserMessage("Hello"))
	content := PlainText(sess)
	golden.Assert(t, content, "simple.golden")
}

func TestAssistantMessage(t *testing.T) {
	sess := session.New(
		session.WithUserMessage("Hello"),
	)
	sess.AddMessage(&session.Message{
		AgentName: "root",
		Message: chat.Message{
			Role:    chat.MessageRoleAssistant,
			Content: "Hello to you too",
		},
	})
	content := PlainText(sess)
	golden.Assert(t, content, "assistant_message.golden")
}

func TestAssistantMessageWithReasoning(t *testing.T) {
	sess := session.New(
		session.WithUserMessage("Hello"),
	)
	sess.AddMessage(&session.Message{
		AgentName: "root",
		Message: chat.Message{
			Role:             chat.MessageRoleAssistant,
			Content:          "Hello to you too",
			ReasoningContent: "Hm....",
		},
	})
	content := PlainText(sess)
	golden.Assert(t, content, "assistant_message_with_reasoning.golden")
}

func TestToolCalls(t *testing.T) {
	sess := session.New(
		session.WithUserMessage("Hello"),
	)
	sess.AddMessage(&session.Message{
		AgentName: "root",
		Message: chat.Message{
			Role:    chat.MessageRoleAssistant,
			Content: "Hello to you too",
			ToolCalls: []tools.ToolCall{
				{
					Function: tools.FunctionCall{Name: "shell", Arguments: `{"cmd":"ls"}`},
				},
			},
		},
	})

	sess.AddMessage(&session.Message{
		AgentName: "",
		Message: chat.Message{
			Role:    chat.MessageRoleTool,
			Content: ".\n..",
		},
	})
	content := PlainText(sess)

	golden.Assert(t, content, "tool_calls.golden")
}
