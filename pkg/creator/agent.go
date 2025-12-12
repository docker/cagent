package creator

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/team"
	"github.com/docker/cagent/pkg/teamloader"
	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/tools/builtin"
)

//go:embed instructions.txt
var agentBuilderInstructions string

type fsToolset struct {
	tools.ElicitationTool

	inner                    tools.ToolSet
	originalWriteFileHandler tools.ToolHandler
	path                     string
}

func (f *fsToolset) Instructions() string {
	return f.inner.Instructions()
}

func (f *fsToolset) Start(ctx context.Context) error {
	return f.inner.Start(ctx)
}

func (f *fsToolset) Stop(ctx context.Context) error {
	return f.inner.Stop(ctx)
}

func (f *fsToolset) Tools(ctx context.Context) ([]tools.Tool, error) {
	innerTools, err := f.inner.Tools(ctx)
	if err != nil {
		return nil, err
	}

	for i, tool := range innerTools {
		if tool.Name == builtin.ToolNameWriteFile {
			f.originalWriteFileHandler = tool.Handler
			innerTools[i].Handler = f.customWriteFileHandler
		}
	}

	return innerTools, nil
}

func (f *fsToolset) customWriteFileHandler(ctx context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		return nil, fmt.Errorf("failed to parse arguments: %w", err)
	}

	f.path = args.Path

	return f.originalWriteFileHandler(ctx, toolCall)
}

func Agent(ctx context.Context, runConfig *config.RuntimeConfig, modelNameOverride string) (*team.Team, error) {
	usableProviders := config.AvailableProviders(ctx, runConfig.ModelsGateway, runConfig.EnvProvider())

	// Provide soft guidance to prefer the selected providers
	instructions := agentBuilderInstructions
	instructions += "\n\nPreferred model providers to use: " + strings.Join(usableProviders, ", ")
	instructions += ". You must always use one or more of the following model configurations: \n"

	for _, provider := range usableProviders {
		model := config.DefaultModels[provider]
		maxTokens := config.PreferredMaxTokens(provider)
		instructions += fmt.Sprintf(`
		models:
			%s:
				provider: %s
				model: %s
				max_tokens: %d\n`, provider, provider, model, maxTokens)
	}

	// Define a new agent configuration
	newAgentConfig := latest.Config{
		Agents: map[string]latest.AgentConfig{
			"root": {
				WelcomeMessage: `Hello! I'm here to create agents for you.

Can you explain to me what the agent will be used for?`,
				Instruction: instructions,
				Model:       "auto",
				Toolsets: []latest.Toolset{
					{Type: "shell"},
					{Type: "filesystem"},
				},
			},
		},
	}

	configAsJSON, err := json.Marshal(newAgentConfig)
	if err != nil {
		return nil, fmt.Errorf("marshalling config: %w", err)
	}

	// Custom tool registry to include fsToolset
	fsToolset := fsToolset{
		inner: builtin.NewFilesystemTool(runConfig.WorkingDir),
	}

	registry := teamloader.NewDefaultToolsetRegistry()
	registry.Register("filesystem", func(context.Context, latest.Toolset, string, *config.RuntimeConfig) (tools.ToolSet, error) {
		return &fsToolset, nil
	})

	return teamloader.Load(
		ctx,
		config.NewBytesSource("creator", configAsJSON),
		runConfig,
		teamloader.WithModelOverrides([]string{modelNameOverride}),
		teamloader.WithToolsetRegistry(registry),
	)
}
