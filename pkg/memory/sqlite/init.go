package sqlite

import "github.com/docker/cagent/pkg/memory"

func init() {
	memory.RegisterFactory("sqlite", &Factory{})
}
