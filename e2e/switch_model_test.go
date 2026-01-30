package e2e_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/agent"
	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/teamloader"
)

// setupSwitchModelTest creates a runtime with model switching support.
func setupSwitchModelTest(t *testing.T) (runtime.Runtime, *agent.Agent) {
	t.Helper()

	ctx := t.Context()
	agentSource, err := config.Resolve("testdata/switch_model.yaml")
	require.NoError(t, err)

	_, runConfig := startRecordingAIProxy(t)
	loadResult, err := teamloader.LoadWithConfig(ctx, agentSource, runConfig)
	require.NoError(t, err)

	modelSwitcherCfg := &runtime.ModelSwitcherConfig{
		Models:             loadResult.Models,
		Providers:          loadResult.Providers,
		ModelsGateway:      runConfig.ModelsGateway,
		EnvProvider:        runConfig.EnvProvider(),
		AgentDefaultModels: loadResult.AgentDefaultModels,
	}

	rt, err := runtime.New(loadResult.Team, runtime.WithModelSwitcherConfig(modelSwitcherCfg))
	require.NoError(t, err)

	rootAgent, err := loadResult.Team.Agent("root")
	require.NoError(t, err)

	return rt, rootAgent
}

// findSwitchModelCall searches session messages for a switch_model tool call containing the given model name.
func findSwitchModelCall(sess *session.Session, modelName string) bool {
	for _, msg := range sess.GetAllMessages() {
		if msg.Message.Role != chat.MessageRoleAssistant || msg.Message.ToolCalls == nil {
			continue
		}
		for _, tc := range msg.Message.ToolCalls {
			if tc.Function.Name == "switch_model" && strings.Contains(tc.Function.Arguments, modelName) {
				return true
			}
		}
	}
	return false
}

// TestSwitchModel_AgentCanSwitchModels verifies that an agent can use the switch_model tool
// to change between models during a conversation.
func TestSwitchModel_AgentCanSwitchModels(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	rt, _ := setupSwitchModelTest(t)

	// Switch to smart model
	sess := session.New(session.WithUserMessage("Switch to the smart model, then say hi"))
	_, err := rt.Run(ctx, sess)
	require.NoError(t, err)

	assert.True(t, findSwitchModelCall(sess, "smart"), "Expected switch_model tool call with 'smart' model")
	assert.NotEmpty(t, sess.GetLastAssistantMessageContent(), "Expected a response after switching")

	// Switch back to fast model
	sess.AddMessage(session.UserMessage("Now switch back to the fast model and say goodbye"))
	_, err = rt.Run(ctx, sess)
	require.NoError(t, err)

	assert.True(t, findSwitchModelCall(sess, "fast"), "Expected switch_model tool call with 'fast' model")
	assert.NotEmpty(t, sess.GetLastAssistantMessageContent(), "Expected a response after switching back")
}

// TestSwitchModel_ModelActuallyChanges verifies that after calling switch_model,
// the agent's model object is updated to the new model.
func TestSwitchModel_ModelActuallyChanges(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	rt, rootAgent := setupSwitchModelTest(t)

	assert.Contains(t, rootAgent.Model().ID(), "gpt-4o-mini", "Should start with gpt-4o-mini")

	// Switch to smart model
	sess := session.New(session.WithUserMessage("Use the switch_model tool to switch to smart model, then just say 'done'"))
	_, err := rt.Run(ctx, sess)
	require.NoError(t, err)

	assert.Contains(t, rootAgent.Model().ID(), "claude", "Model should have changed to claude")

	// Verify the new model works
	sess.AddMessage(session.UserMessage("What is 2+2? Answer with just the number."))
	_, err = rt.Run(ctx, sess)
	require.NoError(t, err)

	assert.NotEmpty(t, sess.GetLastAssistantMessageContent())
}
