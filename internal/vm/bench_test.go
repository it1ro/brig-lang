package vm_test

import (
	"testing"
	"time"
)

// Асимптотика коллекций (T-247, F-11/X-1): BenchmarkScaling строит список,
// Map и Vec из n элементов хвостовой рекурсией на Brig-коде. `make
// bench-scaling` сравнивает t(8k)/t(1k) по каждой операции: линейный рост
// даёт ~8, квадратичный ~64 (на практике ~17 и выше). `make bench` его
// пропускает (-skip Scaling): порог T-152 ловит регрессии горячих путей,
// а не асимптотику.
//
// Замер парный (X-1, T-289): обе сборки в каждой итерации идут подряд, с
// разницей в миллисекунды, поэтому дрейф раннера (троттлинг, чужая
// нагрузка) делится поровну между 1k и 8k и не искажает отношение;
// отдельные замеры каждого размера это не спасало — нестабильный job
// scaling в CI. Отношение — метрика t8_over_t1: среднее по итерациям
// (benchtime усредняет циклы GC в сэмпл), `make bench-scaling` берёт
// медиану по сэмплам.

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

// scalingSizes — пара размеров замера: меньший и больший.
var scalingSizes = []struct {
	name string
	n    int
}{{"1k", 1000}, {"8k", 8000}}

func BenchmarkScaling(b *testing.B) {
	for _, op := range []string{"list_prepend", "map_put", "vec_push"} {
		b.Run(op, func(b *testing.B) {
			small := newCallAllocRun(b, scalingProgs[op], scalingSizes[0].n)
			large := newCallAllocRun(b, scalingProgs[op], scalingSizes[1].n)
			var sumSmall, sumLarge int64
			b.ResetTimer()
			for b.Loop() {
				t0 := time.Now()
				small()
				t1 := time.Now()
				large()
				t2 := time.Now()
				sumSmall += t1.Sub(t0).Nanoseconds()
				sumLarge += t2.Sub(t1).Nanoseconds()
			}
			b.StopTimer()
			if sumSmall <= 0 {
				b.Fatal("нет замера 1k")
			}
			b.ReportMetric(float64(sumLarge)/float64(sumSmall), "t8_over_t1")
		})
	}
}
