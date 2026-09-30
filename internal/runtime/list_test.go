package runtime

import "testing"

// T-271 (DD #397): List — cons-ячейки с длиной, хвост разделяется.
func TestListCons(t *testing.T) {
	xs := List(Int(2), Int(3))
	ys := ListPrepend([]Value{Int(0), Int(1)}, xs)
	if ys.Inspect() != "[0, 1, 2, 3]" || ys.Len() != 4 {
		t.Fatalf("ListPrepend = %s (len %d)", ys.Inspect(), ys.Len())
	}
	if xs.Inspect() != "[2, 3]" {
		t.Fatalf("tail changed: %s", xs.Inspect())
	}
	if ys.Drop(2).list != xs.list {
		t.Fatal("Drop(2) does not share the tail")
	}
	h, tl, ok := ys.Uncons()
	if !ok || !Equal(h, Int(0)) || tl.Inspect() != "[1, 2, 3]" || tl.Len() != 3 {
		t.Fatalf("Uncons = %s, %s, %v", h.Inspect(), tl.Inspect(), ok)
	}
	if _, _, ok := List().Uncons(); ok {
		t.Fatal("Uncons([]) ok")
	}
	if _, _, ok := Vector(Int(1)).Uncons(); ok {
		t.Fatal("Uncons(Vector) ok")
	}
	if !Equal(ys.At(3), Int(3)) || len(ys.Elems()) != 4 {
		t.Fatalf("At/Elems: %s", ys.At(3).Inspect())
	}
	if !Equal(ListPrepend(nil, xs), xs) || List().Len() != 0 || List().Elems() != nil {
		t.Fatal("empty prepend / empty list")
	}
}

func TestListEqualCompare(t *testing.T) {
	a := ListPrepend([]Value{Int(1)}, List(Int(2)))
	b := List(Int(1), Int(2))
	if !Equal(a, b) || !MatchEqual(a, b) {
		t.Fatal("equal lists differ")
	}
	if Equal(a, List(Int(1))) || MatchEqual(List(Int(1)), List(Float(1))) {
		t.Fatal("unequal lists match")
	}
	for _, c := range []struct {
		x, y Value
		want int
	}{
		{List(Int(1)), List(Int(1), Int(0)), -1}, // сначала длина
		{List(Int(2), Int(0)), List(Int(1), Int(9)), 1},
		{a, b, 0},
	} {
		got, err := Compare(c.x, c.y)
		if err != nil || got != c.want {
			t.Errorf("Compare(%s, %s) = %d, %v; want %d", c.x.Inspect(), c.y.Inspect(), got, err, c.want)
		}
	}
}

func TestListBuilderAndItems(t *testing.T) {
	var zero ListBuilder
	if got := zero.List(); got.Kind != KindList || got.Len() != 0 {
		t.Fatalf("empty builder = %s", got.Inspect())
	}
	b := NewListBuilder(10)
	for i := range 3 {
		b.Add(Int(int64(i)))
	}
	xs := b.List()
	if xs.Inspect() != "[0, 1, 2]" || xs.Len() != 3 || xs.Drop(1).Len() != 2 {
		t.Fatalf("builder = %s (len %d)", xs.Inspect(), xs.Len())
	}
	var got []Value
	for e := range xs.Items() {
		got = append(got, e)
		if len(got) == 2 {
			break
		}
	}
	if len(got) != 2 || !Equal(got[1], Int(1)) {
		t.Fatalf("Items early stop = %v", got)
	}
	for range Vector(Int(1)).Items() {
		t.Fatal("Items over Vector yields")
	}
}

func TestListCursor(t *testing.T) {
	it := List(Int(1), Int(2)).Cursor()
	if it.Started() {
		t.Fatal("started before Next")
	}
	var got []Value
	for it.Next() {
		if !it.Started() {
			t.Fatal("not started after Next")
		}
		got = append(got, it.Value())
	}
	if len(got) != 2 || !Equal(got[0], Int(1)) || !Equal(got[1], Int(2)) {
		t.Fatalf("cursor = %v", got)
	}
	empty := Vector(Int(1)).Cursor()
	if empty.Next() {
		t.Fatal("cursor over Vector yields")
	}
}
