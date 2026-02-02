package agent

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/tools"
)

type flakyStartToolset struct {
	calls atomic.Int64
}

// Verify interface compliance
var (
	_ tools.ToolSet   = (*flakyStartToolset)(nil)
	_ tools.Startable = (*flakyStartToolset)(nil)
)

func (f *flakyStartToolset) Start(context.Context) error {
	if f.calls.Add(1) == 1 {
		return errors.New("no events channel available for elicitation")
	}
	return nil
}

func (f *flakyStartToolset) Stop(context.Context) error { return nil }

func (f *flakyStartToolset) Tools(context.Context) ([]tools.Tool, error) { return nil, nil }

func TestStartableToolSet_RetriesAfterFailure(t *testing.T) {
	ctx := t.Context()
	inner := &flakyStartToolset{}
	ts := tools.NewStartable(inner)

	err := ts.Start(ctx)
	require.Error(t, err)
	require.False(t, ts.IsStarted())

	err = ts.Start(ctx)
	require.NoError(t, err)
	require.True(t, ts.IsStarted())

	// Once started, subsequent calls should not call inner.Start again.
	err = ts.Start(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(2), inner.calls.Load())
}
