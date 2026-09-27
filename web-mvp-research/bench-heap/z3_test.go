//go:build heapbench

package benchheap

import (
	"math"
	"runtime/metrics"
	"testing"
	"time"

	brt "github.com/it1ro/brig-lang/internal/runtime"
)

var z3Metrics = []string{
	"/sched/pauses/total/gc:seconds",
	"/cpu/classes/gc/total:cpu-seconds",
	"/cpu/classes/user:cpu-seconds",
	"/gc/cycles/total:gc-cycles",
	"/gc/heap/allocs:bytes",
}

func z3Read() []metrics.Sample {
	ss := make([]metrics.Sample, len(z3Metrics))
	for i, n := range z3Metrics {
		ss[i].Name = n
	}
	metrics.Read(ss)
	return ss
}

// pctl — перцентиль по разности двух снимков гистограммы (верхняя граница бакета).
func pctl(a, b *metrics.Float64Histogram, q float64) float64 {
	var total uint64
	d := make([]uint64, len(b.Counts))
	for i := range b.Counts {
		d[i] = b.Counts[i] - a.Counts[i]
		total += d[i]
	}
	if total == 0 {
		return 0
	}
	want := uint64(math.Ceil(q * float64(total)))
	var acc uint64
	for i, c := range d {
		acc += c
		if acc >= want {
			hi := b.Buckets[i+1]
			if math.IsInf(hi, 1) {
				hi = b.Buckets[i]
			}
			return hi
		}
	}
	return 0
}

// TestZ3GCUnderLoad — паузы GC и доля CPU на GC при живом heap
// 0 / 0.5 / 2 ГиБ и одинаковом потоке аллокаций (Z3, прокси без HTTP).
func TestZ3GCUnderLoad(t *testing.T) {
	// s — кортежей на держателя (20 держателей); ~1.2 КиБ на кортеж в списке.
	for _, s := range []int{0, 22_000, 88_000} {
		var live uint64
		var before []metrics.Sample
		var t0 time.Time
		ns := natives{
			"bench_s": func(_ brt.Caller, _ []brt.Value) (brt.Value, error) { return brt.Int(int64(s)), nil },
			"bench_k": func(_ brt.Caller, _ []brt.Value) (brt.Value, error) { return brt.Int(1_500), nil },
			"mark": func(_ brt.Caller, a []brt.Value) (brt.Value, error) {
				switch a[0].Atom {
				case "loaded":
					live = liveHeap()
					before, t0 = z3Read(), time.Now()
				case "churned":
					wall := time.Since(t0)
					after := z3Read()
					ha := before[0].Value.Float64Histogram()
					hb := after[0].Value.Float64Histogram()
					gc := after[1].Value.Float64() - before[1].Value.Float64()
					user := after[2].Value.Float64() - before[2].Value.Float64()
					cycles := after[3].Value.Uint64() - before[3].Value.Uint64()
					alloc := after[4].Value.Uint64() - before[4].Value.Uint64()
					t.Logf("Z3 live=%-10s alloc=%-10s wall=%-8s gc_cycles=%-3d pause_p50=%-8s pause_p99=%-8s pause_max=%-8s gc_cpu=%.0f%% of (gc+user)",
						mib(live), mib(alloc), wall.Round(time.Millisecond), cycles,
						sec(pctl(ha, hb, 0.5)), sec(pctl(ha, hb, 0.99)), sec(pctl(ha, hb, 1)),
						100*gc/(gc+user))
				}
				return brt.Unit, nil
			},
		}
		runBrig(t, "gcload", ns)
	}
}

func sec(s float64) string {
	return time.Duration(s * float64(time.Second)).Round(time.Microsecond).String()
}
