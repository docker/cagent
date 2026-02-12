package root

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/docker/cagent/pkg/paths"
)

// TestIsFirstRun_AtomicMarker verifies that concurrent callers racing to
// create the first-run marker produce exactly one winner. This test
// overrides `paths.GetConfigDir`; do not run it in parallel with other
// tests that rely on the real config dir.
func TestIsFirstRun_AtomicMarker(t *testing.T) {
	tmp := t.TempDir()

	// Override GetConfigDir for isolation
	old := paths.GetConfigDir
	paths.GetConfigDir = func() string { return tmp }
	t.Cleanup(func() { paths.GetConfigDir = old })

	const tries = 20
	var wg sync.WaitGroup
	wg.Add(tries)

	var trues int32

	start := make(chan struct{})

	for i := 0; i < tries; i++ {
		go func() {
			defer wg.Done()
			<-start
			if isFirstRun() {
				atomic.AddInt32(&trues, 1)
			}
		}()
	}

	// Release all goroutines simultaneously to maximize contention.
	close(start)

	wg.Wait()

	if got := atomic.LoadInt32(&trues); got != 1 {
		t.Fatalf("expected exactly 1 true, got %d", got)
	}

	// Subsequent call should be false
	if isFirstRun() {
		t.Fatalf("expected false on subsequent call after marker exists")
	}
}
