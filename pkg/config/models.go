package config

import (
	"sort"

	latest "github.com/docker/cagent/pkg/config/v2"
)

func GatherModelNames(cfg *latest.Config) []string {
	modelNames := map[string]bool{}
	for _, model := range cfg.Models {
		modelNames[model.Provider+"/"+model.Model] = true
	}

	var names []string
	for e := range modelNames {
		names = append(names, e)
	}
	sort.Strings(names)

	return names
}
