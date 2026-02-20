package config

import (
	"github.com/docker/cagent/pkg/config/latest"
	v0 "github.com/docker/cagent/pkg/config/v0"
	v1 "github.com/docker/cagent/pkg/config/v1"
	v2 "github.com/docker/cagent/pkg/config/v2"
	v3 "github.com/docker/cagent/pkg/config/v3"
	v4 "github.com/docker/cagent/pkg/config/v4"
	v5 "github.com/docker/cagent/pkg/config/v5"
)

func versions() (map[string]func([]byte) (any, error), []func(any, []byte) (any, error)) {
	parsers := map[string]func([]byte) (any, error){}
	var upgraders []func(any, []byte) (any, error)

	v0.Register(parsers, &upgraders)
	v1.Register(parsers, &upgraders)
	v2.Register(parsers, &upgraders)
	v3.Register(parsers, &upgraders)
	v4.Register(parsers, &upgraders)
	v5.Register(parsers, &upgraders)
	latest.Register(parsers, &upgraders)

	return parsers, upgraders
}
