//go:build heapbench

package benchheap

import (
	"testing"
	"time"

	brt "github.com/it1ro/brig-lang/internal/runtime"
)

// TestZ4SendCopy — цена send сейчас (разделение) и оценка цены
// `--copy-on-send`: время глубокого копирования того же сообщения,
// добавленное к одному обмену. Флага в VM нет (НЕ делать в T-101), поэтому
// копирование меряется отдельно на том же значении.
func TestZ4SendCopy(t *testing.T) {
	for _, n := range []int{1, 10, 100, 1_000, 10_000} {
		k := max(2_000, 2_000_000/n)
		if k > 200_000 {
			k = 200_000
		}
		var msg brt.Value
		var elapsed int64
		ns := natives{
			"bench_n":  func(_ brt.Caller, _ []brt.Value) (brt.Value, error) { return brt.Int(int64(n)), nil },
			"bench_k":  func(_ brt.Caller, _ []brt.Value) (brt.Value, error) { return brt.Int(int64(k)), nil },
			"capture":  func(_ brt.Caller, a []brt.Value) (brt.Value, error) { msg = a[0]; return brt.Unit, nil },
			"report":   func(_ brt.Caller, a []brt.Value) (brt.Value, error) { elapsed = a[0].SmallInt; return brt.Unit, nil },
			"clock_ns": clockNative(time.Now()),
		}
		runBrig(t, "sendpp", ns)
		rt := float64(elapsed) / float64(k)

		// Сообщение, которое реально уходит эхо-актору: (from, msg).
		wire := brt.Tuple(brt.Value{Kind: brt.KindPid, Pid: 1}, msg)
		res := testing.Benchmark(func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = deepCopy(wire)
			}
		})
		cp := float64(res.NsPerOp())
		cw := compactOf(wire)
		cres := testing.Benchmark(func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = cdeepCopy(cw)
			}
		})
		ccp := float64(cres.NsPerOp())
		t.Logf("Z4 n=%-6d roundtrip=%7.0f ns | Value: copy=%9.0f ns %9d B %7.2fx | compact: copy=%8.0f ns %8d B %6.2fx",
			n, rt, cp, res.AllocedBytesPerOp(), cp/rt, ccp, cres.AllocedBytesPerOp(), ccp/rt)
	}
}
