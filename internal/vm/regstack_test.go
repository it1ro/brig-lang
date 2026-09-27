package vm

import (
	"reflect"
	"testing"

	"github.com/it1ro/brig-lang/internal/runtime"
)

// TestRegStack — окна не пересекаются, cap окна равен его размеру,
// освобождённое окно обнулено и отдаётся снова; сегменты переиспользуются
// без аллокаций, дальние после спада глубины отдаются GC (T-103).
func TestRegStack(t *testing.T) {
	var st regStack
	var wins [][]runtime.Value
	for i := 0; i < 200; i++ {
		w := st.alloc(3 + i%5)
		if cap(w) != len(w) {
			t.Fatalf("window %d: len=%d cap=%d", i, len(w), cap(w))
		}
		for j := range w {
			if !reflect.ValueOf(w[j]).IsZero() {
				t.Fatalf("window %d not zeroed at %d: %v", i, j, w[j])
			}
			w[j] = runtime.Int(int64(i))
		}
		wins = append(wins, w)
	}
	for i, w := range wins {
		for j := range w {
			if w[j].SmallInt != int64(i) {
				t.Fatalf("window %d overwritten at %d: %v", i, j, w[j])
			}
		}
	}
	segs := len(st.segs)
	for i := len(wins) - 1; i >= 0; i-- {
		st.free(wins[i])
	}
	if st.depth != 1 || st.used[0] != 0 {
		t.Fatalf("after free: depth=%d used=%v", st.depth, st.used)
	}
	if len(st.segs) != 2 {
		t.Errorf("cached segments = %d (of %d), want 2", len(st.segs), segs)
	}
	for _, seg := range st.segs {
		for j := range seg {
			if !reflect.ValueOf(seg[j]).IsZero() {
				t.Fatalf("freed segment not zeroed at %d: %v", j, seg[j])
			}
		}
	}

	// Вызов на границе сегментов: запасной сегмент переиспользуется.
	base := st.alloc(len(st.segs[0]))
	allocs := testing.AllocsPerRun(100, func() {
		st.free(st.alloc(4))
	})
	if allocs != 0 {
		t.Errorf("alloc/free across segment boundary: %.0f allocs, want 0", allocs)
	}
	st.free(base)
}

// TestRegStackIdleFootprint — TAILCALL из кадра spawn в функцию с большим
// числом регистров подгоняет первый сегмент, а не заводит второй; shrink
// отдаёт запасные сегменты заблокированного актора (Z2, T-103).
func TestRegStackIdleFootprint(t *testing.T) {
	var st regStack
	w := st.alloc(2)
	st.pop(w)
	w = st.alloc(9)
	if len(st.segs) != 1 || len(st.segs[0]) != 9 {
		t.Fatalf("after tail call: segs=%d len(segs[0])=%d, want 1 segment of 9",
			len(st.segs), len(st.segs[0]))
	}
	st.free(st.alloc(4)) // вложенный вызов оставил запасной сегмент
	if len(st.segs) != 2 {
		t.Fatalf("spare segment not cached: segs=%d", len(st.segs))
	}
	st.shrink()
	if len(st.segs) != 1 || st.depth != 1 || st.used[0] != 9 {
		t.Fatalf("after shrink: segs=%d depth=%d used=%v", len(st.segs), st.depth, st.used)
	}
	st.free(w)
}
