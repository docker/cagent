package tests

import (
	"context"
)

type testEnvProvider map[string]string

func (p *testEnvProvider) Get(_ context.Context, name string) string {
	return (*p)[name]
}
