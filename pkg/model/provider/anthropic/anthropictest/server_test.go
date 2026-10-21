package anthropictest_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/model/provider/anthropic/anthropictest"
)

func TestServerRepliesAndRecords(t *testing.T) {
	t.Parallel()

	srv := anthropictest.NewServer(t, anthropictest.TextReply("msg_1", "hi", 42))
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL()+"/v1/messages", strings.NewReader(`{"model":"m"}`))
	require.NoError(t, err)
	req.Header.Set("anthropic-beta", "a, b")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Contains(t, string(body), `"input_tokens":42`)
	assert.Contains(t, string(body), "event: message_stop")
	got := srv.Requests()
	require.Len(t, got, 1)
	assert.Equal(t, []string{"a", "b"}, got[0].Betas)
	assert.Equal(t, "m", got[0].Body["model"])
}
