package v1

import (
	"github.com/docker/cagent/pkg/config/types"
	v0 "github.com/docker/cagent/pkg/config/v0"
)

func UpgradeFrom(old v0.Config) (Config, error) {
	var config Config
	types.CloneThroughJSON(old, &config)

	// model.Type --> model.Provider
	for name := range old.Models {
		oldModel := old.Models[name]
		newModel := config.Models[name]

		newModel.Provider = oldModel.Type
		config.Models[name] = newModel
	}

	// todo:true --> toolsets: [{type: todo}]
	// think:true --> toolsets: [{type: think}]
	// memory:{path: PATH} --> toolsets: [{type: memory, path: PATH}]
	for name := range old.Agents {
		oldAgent := old.Agents[name]
		newAgent := config.Agents[name]

		var toolsets []Toolset

		if oldAgent.Todo.Enabled {
			toolsets = append(toolsets, Toolset{
				Type:   "todo",
				Shared: oldAgent.Todo.Shared,
			})
		}
		if oldAgent.Think {
			toolsets = append(toolsets, Toolset{
				Type: "think",
			})
		}
		if oldAgent.MemoryConfig.Path != "" {
			toolsets = append(toolsets, Toolset{
				Type: "memory",
				Path: oldAgent.MemoryConfig.Path,
			})
		}

		toolsets = append(toolsets, newAgent.Toolsets...)
		newAgent.Toolsets = toolsets
		config.Agents[name] = newAgent
	}

	return config, nil
}
