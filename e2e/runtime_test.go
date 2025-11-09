package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/teamloader"
)

func TestRuntime_OpenAI_Basic(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	_, runtimeConfig := startRecordingAIProxy(t)

	team, err := teamloader.Load(ctx, "testdata/basic.yaml", runtimeConfig)
	require.NoError(t, err)

	rt, err := runtime.New(team)
	require.NoError(t, err)

	sess := session.New(session.WithUserMessage("", "Who's djordje?"))
	_, err = rt.Run(ctx, sess)
	require.NoError(t, err)

	response := sess.GetLastAssistantMessageContent()
	assert.Equal(t, "Djordje is a popular given name in some Eastern European countries, such as Serbia. If you have more specific information or context, I'd be happy to help further.", response)
	assert.Equal(t, "Understanding identity: Who is Djordje?", sess.Title)
}

func TestRuntime_Mistral_Basic(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	_, runtimeConfig := startRecordingAIProxy(t)

	team, err := teamloader.Load(ctx, "testdata/basic.yaml", runtimeConfig, teamloader.WithModelOverrides([]string{"mistral/mistral-small"}))
	require.NoError(t, err)

	rt, err := runtime.New(team)
	require.NoError(t, err)

	sess := session.New(session.WithUserMessage("", "Who's djordje?"))
	_, err = rt.Run(ctx, sess)
	require.NoError(t, err)

	response := sess.GetLastAssistantMessageContent()
	assert.Equal(t, `It seems like "djordje" is a name, most likely of Slavic origin. It is commonly spelled as "Đorđe" in Serbian language, and it means "farmer" or "earthworker". It is a masculine given name, and it is quite popular in Serbia, Montenegro, and other countries in the region. Without more context, it's hard to say exactly who "djordje" is, as it could refer to any person by that name.`, response)
	assert.Equal(t, `"Inquiry About the Identity of 'Djordje'"`, sess.Title)
}
