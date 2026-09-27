//go:build heapbench

package benchheap

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
	"unsafe"

	brt "github.com/it1ro/brig-lang/internal/runtime"
)

// TestZ0Sizes — размеры базовых структур: от них зависит всё остальное.
func TestZ0Sizes(t *testing.T) {
	t.Logf("Z0 sizeof(runtime.Value) = %d B", unsafe.Sizeof(brt.Value{}))
	t.Logf("Z0 sizeof(runtime.MapEntry) = %d B", unsafe.Sizeof(brt.MapEntry{}))
	t.Logf("Z0 sizeof(runtime.RecordField) = %d B", unsafe.Sizeof(brt.RecordField{}))
}

// TestZ2IdleActors — живой heap на простаивающего актора (Z2).
func TestZ2IdleActors(t *testing.T) {
	for _, n := range []int{1_000, 10_000, 100_000} {
		heap := map[string]uint64{}
		ns := natives{
			"bench_n": func(_ brt.Caller, _ []brt.Value) (brt.Value, error) { return brt.Int(int64(n)), nil },
			"probe": func(_ brt.Caller, args []brt.Value) (brt.Value, error) {
				heap[args[0].Atom] = liveHeap()
				if n == 100_000 && args[0].Atom == "after" && *profDir != "" {
					writeHeapProfile(t, filepath.Join(*profDir, "z2-idle-100k.pprof"))
				}
				return brt.Unit, nil
			},
		}
		start := time.Now()
		runBrig(t, "idle", ns)
		el := time.Since(start)
		per := float64(heap["after"]-heap["before"]) / float64(n)
		t.Logf("Z2 n=%-7d live_delta=%s per_actor=%.0f B spawn+block=%.2f us/actor",
			n, mib(heap["after"]-heap["before"]), per, float64(el.Microseconds())/float64(n))
	}
}

func mib(b uint64) string { return fmt.Sprintf("%.1f MiB", float64(b)/(1<<20)) }

// TestZ2bTimerWithIdleActors — время одного `recv … after 1` сверх 1 мс, когда рядом
// простаивают n акторов: nextDeadline и wakeExpired обходят всю таблицу.
func TestZ2bTimerWithIdleActors(t *testing.T) {
	for _, n := range []int{0, 1_000, 10_000, 100_000} {
		const k = 300
		var elapsed int64
		ns := natives{
			"bench_n":  func(_ brt.Caller, _ []brt.Value) (brt.Value, error) { return brt.Int(int64(n)), nil },
			"bench_k":  func(_ brt.Caller, _ []brt.Value) (brt.Value, error) { return brt.Int(k), nil },
			"report":   func(_ brt.Caller, a []brt.Value) (brt.Value, error) { elapsed = a[0].SmallInt; return brt.Unit, nil },
			"clock_ns": clockNative(time.Now()),
		}
		runBrig(t, "idle_timer", ns)
		t.Logf("Z2b idle=%-7d recv_after_1_overhead=%8.1f us", n, float64(elapsed)/k/1000-1000)
	}
}
