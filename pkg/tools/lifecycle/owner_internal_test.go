package lifecycle

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAbandonedStartBeforeFirstReady(t *testing.T) {
	t.Parallel()
	for _, adopted := range []bool{false, true} {
		owner := NewOwner("initial-ready", nil, Policy{})
		owner.state.State = StateStarting
		run := &ownedRun{adopted: adopted}
		changed := owner.changed
		owner.mu.Lock()
		owner.abandonStartLocked(run, context.Canceled)
		owner.mu.Unlock()
		assert.Equal(t, StateStopped, owner.State().State, "adopted=%v", adopted)
		require.ErrorIs(t, owner.State().LastError, context.Canceled)
		select {
		case <-changed:
		default:
			t.Error("abandoned start did not notify waiters")
		}
	}
}

func TestAbandonedStartDoesNotOverridePublishedReadiness(t *testing.T) {
	t.Parallel()
	owner := NewOwner("ready", nil, Policy{})
	owner.state.State = StateReady
	owner.mu.Lock()
	owner.abandonStartLocked(&ownedRun{adopted: true, active: true}, context.Canceled)
	owner.mu.Unlock()
	assert.Equal(t, StateReady, owner.State().State)
	assert.NoError(t, owner.State().LastError)
}
