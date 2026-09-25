package vm_test

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestVerifyAF1SingleGoroutineScheduler verifies A-F1 (AUDIT_REPORT):
// the scheduler is a cooperative single-goroutine run-loop, not
// "1 actor = 1 goroutine" as stated in §15.2 / architecture.md.
//
// Probe: spawn 100 blocked actors and require that runtime.NumGoroutine
// grows by strictly less than 100 while they are live.
func TestVerifyAF1SingleGoroutineScheduler(t *testing.T) {
	const nActors = 100

	before := runtime.NumGoroutine()
	var maxSeen atomic.Int64
	maxSeen.Store(int64(before))

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go sampleMaxGoroutines(&wg, stop, &maxSeen)

	src := fmt.Sprintf(`module Main
fn worker() ->
    recv
        :never -> :ok
    after 500 -> :ok

fn spawn_many(n) ->
    if n == 0 then () else spawn_one(n)

fn spawn_one(n) ->
    pid = spawn(worker)
    spawn_many(n - 1)

fn main() ->
    spawn_many(%d)
    recv
        :never -> :ok
    after 100 -> :ok
`, nActors)
	runModuleSync(t, src)

	close(stop)
	wg.Wait()

	peak := int(maxSeen.Load())
	growth := peak - before
	// Sampler adds at most one goroutine; GC may add a few more.
	// Spec "1 actor = 1 goroutine" would grow by ~nActors.
	if growth >= nActors {
		t.Fatalf("NumGoroutine grew by %d (before=%d peak=%d); want growth < %d (cooperative scheduler)",
			growth, before, peak, nActors)
	}
	t.Logf("A-F1 confirmed: NumGoroutine grew by %d (before=%d peak=%d) with %d live actors",
		growth, before, peak, nActors)
}

func sampleMaxGoroutines(wg *sync.WaitGroup, stop <-chan struct{}, maxSeen *atomic.Int64) {
	defer wg.Done()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			n := int64(runtime.NumGoroutine())
			for {
				old := maxSeen.Load()
				if n <= old || maxSeen.CompareAndSwap(old, n) {
					break
				}
			}
		}
	}
}
