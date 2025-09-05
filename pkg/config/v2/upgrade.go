package v2

import (
	"github.com/docker/cagent/pkg/config/types"
	v1 "github.com/docker/cagent/pkg/config/v1"
)

func UpgradeFrom(old v1.Config) Config {
	var config Config
	types.CloneThroughJSON(old, &config)
	return config
}
