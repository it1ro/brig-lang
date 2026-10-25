package vm_test

import (
	"testing"

	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/vm"
)

// TestListFromSliceAllocs — натив, который строит List из известного
// числа элементов, выделяет один блок ячеек, без промежуточного среза
// Value (T-294). Элементы — малые Int: сами они не аллоцируют.
func TestListFromSliceAllocs(t *testing.T) {
	m := vm.New()
	ents := make([]runtime.MapEntry, 100)
	for i := range ents {
		ents[i] = runtime.MapEntry{Key: runtime.Int(int64(i)), Val: runtime.Int(int64(-i))}
	}
	mp := runtime.Map(ents)
	mp.Entries() // порядок ключей строится один раз и кэшируется
	cases := []struct {
		fn   string
		args []runtime.Value
	}{
		{"list", []runtime.Value{runtime.Range(1, 1000)}},
		{"Map.keys", []runtime.Value{mp}},
		{"Map.values", []runtime.Value{mp}},
	}
	for _, c := range cases {
		t.Run(c.fn, func(t *testing.T) {
			native := m.Global(c.fn).Func.Native
			got := testing.AllocsPerRun(20, func() {
				if _, err := native(nil, c.args); err != nil {
					t.Fatal(err)
				}
			})
			if got != 1 {
				t.Errorf("%s: allocs = %.0f, want 1", c.fn, got)
			}
		})
	}
}

// TestListOpcodeAllocs — литерал [a, b, c] (опкод LIST) выделяет только
// ячейки списка (T-294). Прирост аллокаций от n дополнительных литералов
// в цикле; постоянная часть прогона вычитается.
func TestListOpcodeAllocs(t *testing.T) {
	const src = `module Main
fn loop(i, acc) -> if i == 0 then acc else loop(i - 1, [i, i, i])

fn main() -> loop(n(), [])
`
	const n = 1000
	small := testing.AllocsPerRun(20, newCallAllocRun(t, src, n))
	large := testing.AllocsPerRun(20, newCallAllocRun(t, src, 2*n))
	if per := (large - small) / n; per > 1.01 {
		t.Errorf("allocs per [i, i, i] = %.3f, want 1", per)
	}
}
