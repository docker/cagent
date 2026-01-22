package runtime_test

import (
	"context"

	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/tools"
)

// ------------------------
// fakeRuntime implementa runtime.Runtime para testes
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
func (f *fakeRuntime) EmitStartupInfo(ctx context.Context) {}
