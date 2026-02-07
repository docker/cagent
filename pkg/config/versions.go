package config

import (
	"github.com/docker/cagent/pkg/config/latest"
	v0 "github.com/docker/cagent/pkg/config/v0"
	v1 "github.com/docker/cagent/pkg/config/v1"
	v2 "github.com/docker/cagent/pkg/config/v2"
	v3 "github.com/docker/cagent/pkg/config/v3"
	v4 "github.com/docker/cagent/pkg/config/v4"
)

func Parsers() map[string]func([]byte) (any, error) {
	return map[string]func([]byte) (any, error){
		v0.Version: func(d []byte) (any, error) { return v0.Parse(d) },
		v1.Version: func(d []byte) (any, error) { return v1.Parse(d) },
		v2.Version: func(d []byte) (any, error) { return v2.Parse(d) },
		v3.Version: func(d []byte) (any, error) { return v3.Parse(d) },
		v4.Version: func(d []byte) (any, error) { return v4.Parse(d) },

		latest.Version: func(d []byte) (any, error) { return latest.Parse(d) },
	}
}

func Upgrades() []func(any, []byte) (any, error) {
	return []func(any, []byte) (any, error){
		v0.UpgradeIfNeeded,
		v1.UpgradeIfNeeded,
		v2.UpgradeIfNeeded,
		v3.UpgradeIfNeeded,
		v4.UpgradeIfNeeded,

		latest.UpgradeIfNeeded,
	}
}
