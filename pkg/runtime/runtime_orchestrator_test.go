package runtime_test

import (
	"context"
	"testing"

	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/tools"
)

// ------------------------
// fakeRuntime implements runtime.Runtime for testing.
// ------------------------
type fakeRuntime struct {
	events []runtime.Event
}

func (f *fakeRuntime) RunStream(ctx context.Context, _ *session.Session) <-chan runtime.Event {
	ch := make(chan runtime.Event)
	go func() {
		defer close(ch)
		for _, ev := range f.events {
			select {
			case ch <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch
}

func (f *fakeRuntime) Resume(ctx context.Context, _ runtime.ResumeType) {}

func (f *fakeRuntime) CurrentAgentInfo(ctx context.Context) runtime.CurrentAgentInfo {
	return runtime.CurrentAgentInfo{Name: "test-agent"}
}

func (f *fakeRuntime) CurrentAgentName() string { return "test-agent" }

func (f *fakeRuntime) CurrentAgentTools(ctx context.Context) ([]tools.Tool, error) {
	return nil, nil
}

func (f *fakeRuntime) EmitStartupInfo(ctx context.Context, ch chan runtime.Event) {}

func (f *fakeRuntime) ResetStartupInfo() {}

func TestFakeRuntime_RunStream_smoke(t *testing.T) {
	ctx := t.Context()

	fr := &fakeRuntime{
		events: []runtime.Event{},
	}

	ch := fr.RunStream(ctx, nil)

	// drain channel to avoid goroutine leak
	for range ch {
	}
}
