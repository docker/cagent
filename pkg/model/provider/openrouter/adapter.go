package openrouter

import (
	"github.com/sashabaranov/go-openai"

	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/model/provider/oaistream"
)

func newStreamAdapter(stream *openai.ChatCompletionStream) chat.MessageStream {
	return oaistream.NewStreamAdapter(stream)
}
