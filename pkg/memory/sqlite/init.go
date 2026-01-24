package sqlite

import "github.com/docker/cagent/pkg/memory"

// registerSQLite registers the sqlite driver factory via package side-effects.
//
//nolint:unparam // Return value exists only to allow calling from a var initializer.
func registerSQLite() struct{} {
	memory.RegisterFactory("sqlite", &Factory{})
	return struct{}{}
}

var _ = registerSQLite()
