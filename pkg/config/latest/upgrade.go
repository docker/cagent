package latest

import (
	"github.com/docker/cagent/pkg/config/types"
	previous "github.com/docker/cagent/pkg/config/v4"
)

func UpgradeIfNeeded(c any, _ []byte) (any, error) {
	old, ok := c.(previous.Config)
	if !ok {
		return c, nil
	}

	var config Config
	types.CloneThroughJSON(old, &config)
	return config, nil
}
