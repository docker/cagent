package tests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/teamloader"
)

func TestOpenAI_SimpleResponse(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	svr := startFakeOpenAIServer(t,
		WithResponseForQuestion("How are you doing?", "Good!"),
	)

	team, err := teamloader.Load(ctx, "testdata/basic.yaml", config.RuntimeConfig{
		ModelsGateway: svr.URL,
		DefaultEnvProvider: &testEnvProvider{
			environment.DockerDesktopTokenEnv: "DUMMY",
		},
	})
	require.NoError(t, err)

	rt, err := runtime.New(team)
	require.NoError(t, err)

	sess := session.New(session.WithUserMessage("", "How are you doing?"))
	messages, err := rt.Run(ctx, sess)
	require.NoError(t, err)

	response := messages[len(messages)-1].Message.Content
	require.NoError(t, err)
	assert.Equal(t, "Good!", response)
}
