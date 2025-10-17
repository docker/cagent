package creator

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/cagent/pkg/agent"
	"github.com/docker/cagent/pkg/config"
	latest "github.com/docker/cagent/pkg/config/v2"
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/model/provider"
	"github.com/docker/cagent/pkg/model/provider/anthropic"
	"github.com/docker/cagent/pkg/model/provider/options"
	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/team"
	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/tools/builtin"
)

//go:embed instructions.txt
var agentBuilderInstructions string

type fsToolset struct {
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

func (f *fsToolset) SetElicitationHandler(tools.ElicitationHandler) {
	// No-op, this tool does not use elicitation
}

func (f *fsToolset) SetOAuthSuccessHandler(func()) {
	// No-op, this tool does not use OAuth
}

func (f *fsToolset) Tools(ctx context.Context) ([]tools.Tool, error) {
	innerTools, err := f.inner.Tools(ctx)
	if err != nil {
		return nil, err
	}

	for i, tool := range innerTools {
		if tool.Name == "write_file" {
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

func CreateAgent(ctx context.Context, baseDir, prompt string, runConfig config.RuntimeConfig) (out, path string, err error) {
	llm, err := anthropic.NewClient(
		ctx,
		&latest.ModelConfig{
			Provider:  "anthropic",
			Model:     "claude-sonnet-4-0",
			MaxTokens: 64000,
		},
		environment.NewDefaultProvider(),
		options.WithGateway(runConfig.ModelsGateway),
	)
	if err != nil {
		return "", "", fmt.Errorf("failed to create LLM client: %w", err)
	}

	fmt.Println("Generating agent configuration....")

	fsToolset := fsToolset{inner: builtin.NewFilesystemTool([]string{baseDir})}
	fileName := filepath.Base(fsToolset.path)
	newTeam := team.New(
		team.WithID(fileName),
		team.WithAgents(
			agent.New(
				"root",
				agentBuilderInstructions,
				agent.WithModel(llm),
				agent.WithToolSets(
					builtin.NewShellTool(os.Environ()),
					&fsToolset,
				),
			)))
	rt, err := runtime.New(newTeam)
	if err != nil {
		return "", "", fmt.Errorf("failed to create runtime: %w", err)
	}

	sess := session.New(session.WithUserMessage("", prompt))
	sess.ToolsApproved = true

	messages, err := rt.Run(ctx, sess)
	if err != nil {
		return "", "", fmt.Errorf("failed to run session: %w", err)
	}

	return messages[len(messages)-1].Message.Content, fsToolset.path, nil
}

func StreamCreateAgent(ctx context.Context, baseDir, prompt string, runConfig config.RuntimeConfig, providerName, modelNameOverride string, maxTokensOverride, maxIterations int) (<-chan runtime.Event, runtime.Runtime, error) {
	// Apply default max iterations if not specified (0 means use defaults)
	if maxIterations == 0 {
		// Only when using DMR we set a default limit. Local models are more prone to loops
		if providerName == "dmr" {
			maxIterations = 20
		}
	}
	defaultModels := map[string]string{
		"openai":    "gpt-5-mini",
		"anthropic": "claude-sonnet-4-0",
		"google":    "gemini-2.5-flash",
		"dmr":       "ai/qwen3:latest",
	}

	var modelName string
	if _, ok := defaultModels[providerName]; ok {
		modelName = defaultModels[providerName]
	} else {
		modelName = defaultModels["anthropic"]
	}

	if modelNameOverride != "" {
		modelName = modelNameOverride
	} else {
		fmt.Printf("Using default model: %s\n", modelName)
	}

	// if the user provided a model override, let's use that by default for DMR
	// in the generated agentfile
	if providerName == "dmr" && modelName == "" {
		defaultModels["dmr"] = modelName
	}

	// If not using a model gateway, avoid selecting a provider the user can't run
	var usableProviders []string
	if runConfig.ModelsGateway == "" {
		if os.Getenv("OPENAI_API_KEY") != "" {
			usableProviders = append(usableProviders, "openai")
		}
		if os.Getenv("ANTHROPIC_API_KEY") != "" {
			usableProviders = append(usableProviders, "anthropic")
		}
		if os.Getenv("GOOGLE_API_KEY") != "" {
			usableProviders = append(usableProviders, "google")
		}
		// DMR runs locally by default; include it when not using a gateway
		usableProviders = append(usableProviders, "dmr")
	}

	// Use 16k for DMR to limit memory costs
	maxTokens := 64000
	if providerName == "dmr" {
		maxTokens = 16000
	}
	if maxTokensOverride > 0 {
		maxTokens = maxTokensOverride
	}

	llm, err := provider.New(
		ctx,
		&latest.ModelConfig{
			Provider:  providerName,
			Model:     modelName,
			MaxTokens: maxTokens,
		},
		environment.NewDefaultProvider(),
		options.WithGateway(runConfig.ModelsGateway),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create LLM client: %w", err)
	}

	fmt.Println("Generating agent configuration....")

	fsToolset := fsToolset{inner: builtin.NewFilesystemTool([]string{baseDir})}
	fileName := filepath.Base(fsToolset.path)

	// Provide soft guidance to prefer the selected providers
	instructions := agentBuilderInstructions + "\n\nPreferred model providers to use: " + strings.Join(usableProviders, ", ") + ". You must always use one or more of the following model configurations: \n"
	for _, provider := range usableProviders {
		suggestedMaxTokens := 64000
		if provider == "dmr" {
			suggestedMaxTokens = 16000
		}
		instructions += fmt.Sprintf(`
		version: "2"
		models:
			%s:
				provider: %s
				model: %s
				max_tokens: %d\n`, provider, provider, defaultModels[provider], suggestedMaxTokens)
	}

	newTeam := team.New(
		team.WithID(fileName),
		team.WithAgents(
			agent.New(
				"root",
				instructions,
				agent.WithModel(llm),
				agent.WithToolSets(
					builtin.NewShellTool(os.Environ()),
					&fsToolset,
				),
			)))
	rt, err := runtime.New(newTeam)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create runtime: %w", err)
	}

	sess := session.New(
		session.WithUserMessage("", prompt),
		session.WithMaxIterations(maxIterations),
	)
	sess.ToolsApproved = true

	return rt.RunStream(ctx, sess), rt, nil
}
