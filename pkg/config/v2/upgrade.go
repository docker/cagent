package v2

import (
	"errors"

	"github.com/docker/cagent/pkg/config/types"
	v1 "github.com/docker/cagent/pkg/config/v1"
)

func UpgradeFrom(old v1.Config) (Config, error) {
	if len(old.Env) > 0 {
		return Config{}, errors.New("top-level Env is not supported anymore")
	}

	for i := range old.Models {
		model := old.Models[i]

		if len(model.Env) > 0 {
			return Config{}, errors.New("model Env is not supported anymore")
		}
	}

	for agentName := range old.Agents {
		agent := old.Agents[agentName]
		for i := range agent.Toolsets {
			if len(agent.Toolsets[i].Envfiles) > 0 {
				return Config{}, errors.New("toolset Envfiles is not supported anymore")
			}
		}
	}

	var config Config
	types.CloneThroughJSON(old, &config)
	return config, nil
}
