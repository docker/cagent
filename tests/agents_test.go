package tests

import (
	"bytes"
	"io"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/cassette"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/recorder"

	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/teamloader"
)

func removeHeadersHook(i *cassette.Interaction) error {
	i.Request.Headers = map[string][]string{}
	i.Response.Headers = map[string][]string{}
	return nil
}

func customMatcher(t *testing.T) recorder.MatcherFunc {
	t.Helper()
	return func(r *http.Request, i cassette.Request) bool {
		if r.Body == nil || r.Body == http.NoBody {
			return cassette.DefaultMatcher(r, i)
		}

		var reqBody []byte
		var err error
		reqBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Fatal("failed to read request body")
		}
		r.Body.Close()
		r.Body = io.NopCloser(bytes.NewBuffer(reqBody))

		return r.Method == i.Method && r.URL.String() == i.URL && string(reqBody) == i.Body
	}
}

func TestBasicAgent(t *testing.T) {
	r, err := recorder.New(filepath.Join("testdata", "cassettes", t.Name()),
		recorder.WithMode(recorder.ModeRecordOnce),
		recorder.WithMatcher(customMatcher(t)),
		recorder.WithSkipRequestLatency(true),
		recorder.WithHook(removeHeadersHook, recorder.AfterCaptureHook),
	)
	require.NoError(t, err)

	t.Cleanup(func() {
		err := r.Stop()
		require.NoError(t, err)
	})

	team, err := teamloader.Load(t.Context(), "testdata/basic.yaml", config.RuntimeConfig{
		HTTPClient: r,
		DefaultEnvProvider: &testEnvProvider{
			"OPENAI_API_KEY": "DUMMY",
		},
	})
	require.NoError(t, err)

	rt, err := runtime.New(team)
	require.NoError(t, err)

	sess := session.New(session.WithUserMessage("", "How are you doing?"))
	messages, err := rt.Run(t.Context(), sess)
	require.NoError(t, err)

	response := messages[len(messages)-1].Message.Content
	require.NoError(t, err)
	assert.Equal(t, "I'm here and ready to assist you with any questions or tasks you have. How can I help you today?", response)
}
