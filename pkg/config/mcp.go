package config

import (
	"sort"

	latest "github.com/docker/cagent/pkg/config/v2"
)

func GatherMCPServerReferences(cfg *latest.Config) []string {
	servers := map[string]bool{}

	for _, agent := range cfg.Agents {
		for i := range agent.Toolsets {
			toolSet := agent.Toolsets[i]

			if toolSet.Type == "mcp" && toolSet.Ref != "" {
				servers[toolSet.Ref] = true
			}
		}
	}

	var list []string
	for e := range servers {
		list = append(list, e)
	}
	sort.Strings(list)

	return list
}
