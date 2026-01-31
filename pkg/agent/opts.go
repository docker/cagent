package agent

import (
	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/config/types"
	"github.com/docker/cagent/pkg/model/provider"
	"github.com/docker/cagent/pkg/tools"
)

type Opt func(a *Agent)

func WithInstruction(instruction string) Opt {
	return func(a *Agent) {
		a.instruction = instruction
	}
}

func WithToolSets(toolSet ...tools.ToolSet) Opt {
	var startableToolSet []*tools.StartableToolSet
	for _, ts := range toolSet {
		startableToolSet = append(startableToolSet, tools.NewStartable(ts))
	}

	return func(a *Agent) {
		a.toolsets = startableToolSet
	}
}

func WithTools(allTools ...tools.Tool) Opt {
	return func(a *Agent) {
		a.tools = allTools
	}
}

func WithDescription(description string) Opt {
	return func(a *Agent) {
		a.description = description
	}
}

func WithWelcomeMessage(welcomeMessage string) Opt {
	return func(a *Agent) {
		a.welcomeMessage = welcomeMessage
	}
}

func WithName(name string) Opt {
	return func(a *Agent) {
		a.name = name
	}
}

func WithModel(model provider.Provider) Opt {
	return func(a *Agent) {
		a.models = append(a.models, model)
	}
}

func WithSubAgents(subAgents ...*Agent) Opt {
	return func(a *Agent) {
		a.subAgents = subAgents
		for _, subAgent := range subAgents {
			subAgent.parents = append(subAgent.parents, a)
		}
	}
}

func WithHandoffs(handoffs ...*Agent) Opt {
	return func(a *Agent) {
		a.handoffs = handoffs
	}
}

func WithAddDate(addDate bool) Opt {
	return func(a *Agent) {
		a.addDate = addDate
	}
}

func WithAddEnvironmentInfo(addEnvironmentInfo bool) Opt {
	return func(a *Agent) {
		a.addEnvironmentInfo = addEnvironmentInfo
	}
}

func WithAddDescriptionParameter(addDescriptionParameter bool) Opt {
	return func(a *Agent) {
		a.addDescriptionParameter = addDescriptionParameter
	}
}

func WithAddPromptFiles(addPromptFiles []string) Opt {
	return func(a *Agent) {
		a.addPromptFiles = addPromptFiles
	}
}

func WithMaxIterations(maxIterations int) Opt {
	return func(a *Agent) {
		a.maxIterations = maxIterations
	}
}

func WithNumHistoryItems(numHistoryItems int) Opt {
	return func(a *Agent) {
		a.numHistoryItems = numHistoryItems
	}
}

func WithCommands(commands types.Commands) Opt {
	return func(a *Agent) {
		a.commands = commands
	}
}

func WithLoadTimeWarnings(warnings []string) Opt {
	return func(a *Agent) {
		for _, w := range warnings {
			a.addToolWarning(w)
		}
	}
}

func WithSkillsEnabled(enabled bool) Opt {
	return func(a *Agent) {
		a.skillsEnabled = enabled
	}
}

func WithHooks(hooks *latest.HooksConfig) Opt {
	return func(a *Agent) {
		a.hooks = hooks
	}
}

// WithThinkingConfigured sets whether thinking_budget was explicitly configured in the agent's YAML.
// When true, the session will initialize with thinking enabled.
func WithThinkingConfigured(configured bool) Opt {
	return func(a *Agent) {
		a.thinkingConfigured = configured
	}
}
