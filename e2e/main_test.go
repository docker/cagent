package e2e_test

import (
	"os"
	"testing"

	"github.com/docker/cagent/pkg/modelsdev"
)

// TestMain sets up the test environment for all e2e tests.
func TestMain(m *testing.M) {
	store, err := modelsdev.NewStore()
	if err != nil {
		os.Exit(1)
	}
	store.SetDatabaseForTesting(&modelsdev.Database{
		Providers: make(map[string]modelsdev.Provider),
	})
	os.Exit(m.Run())
}
