package runtime

import "testing"

// T-212 (#202): print/repr карты сортирует пары по term order (§7.4),
// независимо от порядка вставки и от случайного обхода map при Json.decode.
func TestMapInspectTermOrder(t *testing.T) {
	m := Map([]MapEntry{
		{Key: Str("s"), Val: Str("str")},
		{Key: Atom("a"), Val: Str("atom")},
		{Key: Int(1), Val: Str("n")},
	})
	const want = `%{1 => "n", :a => "atom", "s" => "str"}`
	if got := m.Inspect(); got != want {
		t.Fatalf("Inspect = %s, want %s", got, want)
	}

	inner := Map([]MapEntry{
		{Key: Str("y"), Val: Int(2)},
		{Key: Str("x"), Val: Int(1)},
	})
	outer := Map([]MapEntry{{Key: Str("a"), Val: inner}})
	const wantNested = `%{"a" => %{"x" => 1, "y" => 2}}`
	if got := outer.Inspect(); got != wantNested {
		t.Fatalf("nested Inspect = %s, want %s", got, wantNested)
	}
}

func TestJSONDecodeMapInspectStable(t *testing.T) {
	const raw = `{"y": [true, false], "x": 1}`
	const want = `%{"x" => 1, "y" => [true, false]}`
	for i := 0; i < 30; i++ {
		v, err := JSONDecode(raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := v.Inspect(); got != want {
			t.Fatalf("decode %d: Inspect = %s, want %s", i, got, want)
		}
	}
}
