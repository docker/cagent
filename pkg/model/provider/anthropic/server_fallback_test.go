package anthropic

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/config/latest"
	"github.com/docker/docker-agent/pkg/model/provider/anthropic/anthropictest"
	"github.com/docker/docker-agent/pkg/model/provider/contracts"
)

func fallbackClient(srv *scriptedServer) *Client {
	return newScriptedClient(srv, latest.ModelConfig{
		Provider: "anthropic", Model: "claude-opus-4-8",
		ProviderOpts: map[string]any{"fallbacks": []any{"claude-sonnet-4-6"}},
	})
}

// drainMarkers reads a stream to its end and returns its server fallback
// markers.
func drainMarkers(t *testing.T, stream chat.MessageStream) []chat.ServerFallback {
	t.Helper()
	var markers []chat.ServerFallback
	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return markers
		}
		require.NoError(t, err)
		for _, c := range resp.Choices {
			if c.Delta.ServerFallback != nil {
				markers = append(markers, *c.Delta.ServerFallback)
			}
		}
	}
}

func TestServerFallbackStream(t *testing.T) {
	t.Parallel()

	const from, to = "claude-opus-4-8", "claude-sonnet-4-6"
	for name, tc := range map[string]struct {
		reply   string
		markers []chat.ServerFallback
		served  string
	}{
		"before output": {
			reply:   anthropictest.FallbackBeforeOutputReply("m", from, to, "hi"),
			markers: []chat.ServerFallback{{From: from, To: to}}, served: to,
		},
		"mid output": {
			reply:   anthropictest.FallbackMidOutputReply("m", from, to, "before", "after", "t1"),
			markers: []chat.ServerFallback{{From: from, To: to}}, served: to,
		},
		"two hops": {
			reply:   anthropictest.FallbackChainReply("m", from, "claude-opus-4-7", to),
			markers: []chat.ServerFallback{{From: from, To: "claude-opus-4-7"}, {From: "claude-opus-4-7", To: to}}, served: to,
		},
		"sticky routing": {reply: anthropictest.StickyReply("m", to, "hi"), served: to},
		// message_start names "claude-test", not the dispatched model: a
		// response model alone is no fallback.
		"no fallback": {reply: anthropictest.TextReply("m", "hi", 10)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			stream, err := fallbackClient(newScriptedServer(t, tc.reply)).CreateChatCompletionStream(t.Context(), []chat.Message{user("q")}, nil)
			require.NoError(t, err)
			defer stream.Close()

			assert.Equal(t, tc.markers, drainMarkers(t, stream))
			served, ok := stream.(contracts.ServedStream)
			require.True(t, ok)
			model, ok := served.ServedBy()
			if tc.served == "" {
				assert.False(t, ok)
				return
			}
			require.True(t, ok)
			assert.Equal(t, "anthropic/"+tc.served, model.ID.String())
			assert.Equal(t, tc.served, model.Config.ModelConfig.Model)
		})
	}

	t.Run("stream cut after the marker", func(t *testing.T) {
		t.Parallel()
		reply := anthropictest.FallbackMidOutputReply("m", from, to, "before", "after", "t1")
		marker := strings.Index(reply, `"type":"fallback"`)
		reply = reply[:marker+strings.Index(reply[marker:], "event: content_block_start")]
		stream, err := fallbackClient(newScriptedServer(t, reply)).CreateChatCompletionStream(t.Context(), []chat.Message{user("q")}, nil)
		require.NoError(t, err)
		defer stream.Close()
		assert.Equal(t, []chat.ServerFallback{{From: from, To: to}}, drainMarkers(t, stream))
		model, ok := stream.(contracts.ServedStream).ServedBy()
		require.True(t, ok, "the handoff happened before the cut")
		assert.Equal(t, to, model.Config.ModelConfig.Model)
	})
}
