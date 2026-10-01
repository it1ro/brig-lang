package runtime

import (
	"math"
	"strconv"
	"strings"
	"testing"
)

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

// T-260 (#412): печатная форма Float всегда содержит '.' или 'e' и
// разбирается обратно в тот же Float.
func TestInspectFloatKeepsKind(t *testing.T) {
	cases := []struct {
		f    float64
		want string
	}{
		{1, "1.0"},
		{2.5, "2.5"},
		{-3, "-3.0"},
		{0, "0.0"},
		{math.Copysign(0, -1), "-0.0"},
		{100000, "100000.0"},
		{1e6, "1.0e6"},
		{1e20, "1.0e20"},
		{1.5e300, "1.5e300"},
		{1e-5, "1.0e-5"},
		{1.234567e-7, "1.234567e-7"},
		{0.1, "0.1"},
		{math.NaN(), "NaN"},
		{math.Inf(1), "+Inf"},
		{math.Inf(-1), "-Inf"},
	}
	for _, tc := range cases {
		got := Float(tc.f).Inspect()
		if got != tc.want {
			t.Errorf("Inspect(%v) = %q, want %q", tc.f, got, tc.want)
		}
		if math.IsNaN(tc.f) || math.IsInf(tc.f, 0) {
			continue
		}
		back, err := strconv.ParseFloat(got, 64)
		if err != nil || back != tc.f || !strings.ContainsAny(got, ".e") {
			t.Errorf("Inspect(%v) = %q does not round-trip as Float (%v, %v)", tc.f, got, back, err)
		}
	}
	if got := List(Float(1), Float(2.5), Float(1e20)).Inspect(); got != "[1.0, 2.5, 1.0e20]" {
		t.Errorf("list Inspect = %q", got)
	}
}
