package latest

import (
	"github.com/docker/cagent/pkg/config/types"
	previous "github.com/docker/cagent/pkg/config/v5"
)

func Register(parsers map[string]func([]byte) (any, error), upgraders *[]func(any, []byte) (any, error)) {
	parsers[Version] = func(d []byte) (any, error) { return Parse(d) }
	*upgraders = append(*upgraders, UpgradeIfNeeded)
}

func UpgradeIfNeeded(c any, _ []byte) (any, error) {
	old, ok := c.(previous.Config)
	if !ok {
		return c, nil
	}

	var config Config
	types.CloneThroughJSON(old, &config)
	return config, nil
}
