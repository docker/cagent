package team_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/docker/cagent/pkg/memory"
	"github.com/docker/cagent/pkg/team"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type closeTrackingDriver struct {
	closed atomic.Bool
}

func (d *closeTrackingDriver) Store(context.Context, string, string) error { return nil }

func (d *closeTrackingDriver) Retrieve(context.Context, memory.Query) ([]memory.Entry, error) {
	return nil, nil
}

func (d *closeTrackingDriver) Delete(context.Context, string) error { return nil }

func (d *closeTrackingDriver) Close() error {
	d.closed.Store(true)
	return nil
}

func TestTeamStopToolSets_ClosesMemoryDrivers(t *testing.T) {
	t.Parallel()

	driver := &closeTrackingDriver{}

	tm := team.New(team.WithMemoryDrivers(map[string]memory.Driver{
		"test": driver,
	}))

	require.NoError(t, tm.StopToolSets(t.Context()))
	assert.True(t, driver.closed.Load())
}


