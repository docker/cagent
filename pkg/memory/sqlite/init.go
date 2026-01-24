package sqlite

import "github.com/docker/cagent/pkg/memory"

var _ = func() struct{} {
	memory.RegisterFactory("sqlite", &Factory{})
	return struct{}{}
}()
