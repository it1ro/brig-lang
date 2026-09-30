package vm_test

import "testing"

// Асимптотика коллекций (T-247, F-11/X-1): BenchmarkScaling строит список,
// Map и Vec из n элементов хвостовой рекурсией на Brig-коде. `make
// bench-scaling` сравнивает t(8k)/t(1k) по каждой операции: линейный рост
// даёт ~8, квадратичный ~64 (на практике ~17 и выше). `make bench` его
// пропускает (-skip Scaling): порог T-152 ловит регрессии горячих путей,
// а не асимптотику.

var scalingProgs = map[string]string{
	// [i, ..acc]: prepend должен быть O(1).
	"list_prepend": `module Main
fn build(i, acc) -> if i == 0 then acc else build(i - 1, [i, ..acc])

fn main() -> build(n(), [])
`,
	// Map.put: O(log32 n) на вставку.
	"map_put": `module Main
fn build(i, acc) -> if i == 0 then acc else build(i - 1, Map.put(acc, i, i))

fn main() -> build(n(), %{})
`,
	// Vec.push: O(1) amortized.
	"vec_push": `module Main
fn build(i, acc) -> if i == 0 then acc else build(i - 1, Vec.push(acc, i))

fn main() -> build(n(), %[])
`,
}

var scalingSizes = []struct {
	name string
	n    int
}{{"1k", 1000}, {"8k", 8000}}

func BenchmarkScaling(b *testing.B) {
	for _, op := range []string{"list_prepend", "map_put", "vec_push"} {
		b.Run(op, func(b *testing.B) {
			for _, sz := range scalingSizes {
				b.Run(sz.name, func(b *testing.B) {
					run := newCallAllocRun(b, scalingProgs[op], sz.n)
					b.ReportAllocs()
					b.ResetTimer()
					for b.Loop() {
						run()
					}
				})
			}
		})
	}
}
