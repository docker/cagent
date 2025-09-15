package dmr

import (
	"github.com/sashabaranov/go-openai"

	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/model/provider/oaistream"
)

// newStreamAdapter returns the shared OpenAI stream adapter implementation
func newStreamAdapter(stream *openai.ChatCompletionStream, trackUsage bool) chat.MessageStream {
	return oaistream.NewStreamAdapter(stream, trackUsage)
}
