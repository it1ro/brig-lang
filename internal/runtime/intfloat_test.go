package runtime

import (
	"math"
	"math/big"
	"sort"
	"testing"
)

// I-F8 / T-85: Int×Float сравниваются точно.

func pow2(n uint) Value { return IntBig(new(big.Int).Lsh(big.NewInt(1), n)) }

func TestIntFloatExactAt2Pow53(t *testing.T) {
	f := Float(math.Pow(2, 53))
	i := Int(1<<53 + 1)
	if Equal(i, f) || Equal(f, i) {
		t.Fatal("2^53+1 == 2^53.0 must be false")
	}
	if c, _ := Compare(i, f); c != 1 {
		t.Fatalf("Compare(2^53+1, 2^53.0) = %d, want 1", c)
	}
	if c, _ := Compare(f, i); c != -1 {
		t.Fatalf("Compare(2^53.0, 2^53+1) = %d, want -1", c)
	}
	if !Equal(Int(1<<53), f) {
		t.Fatal("2^53 == 2^53.0 must stay true")
	}
}

func TestIntFloatTransitivity(t *testing.T) {
	a, f, b := Int(1<<53), Float(math.Pow(2, 53)), Int(1<<53+1)
	// a == f, f != b  =>  a != b (раньше f == b тоже была истиной).
	if !Equal(a, f) || Equal(f, b) || Equal(a, b) {
		t.Fatalf("not transitive: a==f %v, f==b %v, a==b %v", Equal(a, f), Equal(f, b), Equal(a, b))
	}
}

func TestIntFloatSmallStaysEqual(t *testing.T) {
	if !Equal(Int(1), Float(1)) || !Equal(Float(1), Int(1)) {
		t.Fatal("1 == 1.0")
	}
	if !Equal(Int(-3), Float(-3)) {
		t.Fatal("-3 == -3.0")
	}
	if Equal(Int(1), Float(1.5)) {
		t.Fatal("1 != 1.5")
	}
	if c, _ := Compare(Int(1), Float(1.5)); c != -1 {
		t.Fatalf("1 < 1.5, got %d", c)
	}
	if c, _ := Compare(Float(-0.5), Int(0)); c != -1 {
		t.Fatalf("-0.5 < 0, got %d", c)
	}
	if !Equal(Int(0), Float(math.Copysign(0, -1))) {
		t.Fatal("0 == -0.0")
	}
}

func TestIntFloatIntBig(t *testing.T) {
	// 2^100 точно представим во float64.
	if !Equal(pow2(100), Float(math.Pow(2, 100))) {
		t.Fatal("2^100 == 2^100.0")
	}
	if Equal(IntBig(new(big.Int).Add(pow2(100).AsBig(), big.NewInt(1))), Float(math.Pow(2, 100))) {
		t.Fatal("2^100+1 != 2^100.0")
	}
	if c, _ := Compare(IntBig(new(big.Int).Add(pow2(100).AsBig(), big.NewInt(1))), Float(math.Pow(2, 100))); c != 1 {
		t.Fatalf("2^100+1 > 2^100.0, got %d", c)
	}
	if c, _ := Compare(pow2(2000), Float(math.MaxFloat64)); c != 1 {
		t.Fatalf("2^2000 > MaxFloat64, got %d", c)
	}
}

func TestIntFloatInf(t *testing.T) {
	inf, ninf := Float(math.Inf(1)), Float(math.Inf(-1))
	for _, i := range []Value{Int(0), Int(math.MaxInt64), Int(math.MinInt64), pow2(5000)} {
		if Equal(i, inf) || Equal(i, ninf) {
			t.Fatalf("%s equals Inf", i.Inspect())
		}
		if c, _ := Compare(i, inf); c != -1 {
			t.Fatalf("%s < +Inf, got %d", i.Inspect(), c)
		}
		if c, _ := Compare(inf, i); c != 1 {
			t.Fatalf("+Inf > %s, got %d", i.Inspect(), c)
		}
		if c, _ := Compare(i, ninf); c != 1 {
			t.Fatalf("%s > -Inf, got %d", i.Inspect(), c)
		}
		if c, _ := Compare(ninf, i); c != -1 {
			t.Fatalf("-Inf < %s, got %d", i.Inspect(), c)
		}
	}
	if !Equal(inf, inf) || Equal(inf, ninf) {
		t.Fatal("Inf == Inf, Inf != -Inf")
	}
}

func TestIntFloatNaN(t *testing.T) {
	nan := Float(math.NaN())
	if Equal(nan, nan) {
		t.Fatal("NaN != NaN")
	}
	for _, i := range []Value{Int(0), Int(1 << 60), pow2(100)} {
		if Equal(i, nan) || Equal(nan, i) {
			t.Fatalf("NaN == %s", i.Inspect())
		}
		// Compare: NaN упорядочен после всех чисел (нужно sort/Set).
		if c, _ := Compare(i, nan); c != -1 {
			t.Fatalf("Compare(%s, NaN) = %d, want -1", i.Inspect(), c)
		}
		if c, _ := Compare(nan, i); c != 1 {
			t.Fatalf("Compare(NaN, %s) = %d, want 1", i.Inspect(), c)
		}
	}
	if c, _ := Compare(nan, nan); c != 0 {
		t.Fatalf("Compare(NaN, NaN) = %d, want 0", c)
	}
	if !IsNaNOperand(nan, Int(1)) || !IsNaNOperand(Int(1), nan) || IsNaNOperand(Int(1), Float(1)) {
		t.Fatal("IsNaNOperand")
	}
}

// sort смешанного списка: порядок непротиворечив и не зависит от
// начальной перестановки.
func TestSortMixedIntFloat(t *testing.T) {
	f53 := Float(math.Pow(2, 53))
	vals := []Value{Int(1<<53 + 1), f53, Int(1 << 53), Float(0.5), Int(-1), Float(math.Inf(1)), Float(math.Inf(-1)), Int(1<<53 - 1)}
	perms := [][]int{{0, 1, 2, 3, 4, 5, 6, 7}, {7, 6, 5, 4, 3, 2, 1, 0}, {3, 0, 6, 1, 7, 2, 5, 4}}
	for _, p := range perms {
		xs := make([]Value, len(vals))
		for k, idx := range p {
			xs[k] = vals[idx]
		}
		sort.SliceStable(xs, func(i, j int) bool {
			c, err := Compare(xs[i], xs[j])
			return err == nil && c < 0
		})
		for k := 1; k < len(xs); k++ {
			if c, _ := Compare(xs[k-1], xs[k]); c > 0 {
				t.Fatalf("perm %v: not sorted at %d: %v", p, k, xs)
			}
		}
		// 2^53+1 строго последний из конечных, +Inf — последний.
		if !Equal(xs[len(xs)-2], Int(1<<53+1)) || xs[len(xs)-1].Float != math.Inf(1) || xs[0].Float != math.Inf(-1) {
			t.Fatalf("perm %v: wrong order: %v", p, xs)
		}
	}
}

func TestIntFloatNestedAndSetCompare(t *testing.T) {
	a := List(Int(1<<53 + 1))
	b := List(Float(math.Pow(2, 53)))
	if Equal(a, b) {
		t.Fatal("[2^53+1] != [2^53.0]")
	}
	if c, _ := Compare(a, b); c != 1 {
		t.Fatalf("Compare lists = %d, want 1", c)
	}
	m1 := Map([]MapEntry{{Key: Int(1<<53 + 1), Val: Int(1)}})
	m2 := Map([]MapEntry{{Key: Float(math.Pow(2, 53)), Val: Int(1)}})
	if Equal(m1, m2) {
		t.Fatal("maps with distinct keys are equal")
	}
}

func TestIntIntCompareExactAbove2Pow53(t *testing.T) {
	if c, _ := Compare(Int(1<<53+1), Int(1<<53)); c != 1 {
		t.Fatalf("2^53+1 > 2^53, got %d", c)
	}
	if c, _ := Compare(pow2(100), IntBig(new(big.Int).Add(pow2(100).AsBig(), big.NewInt(1)))); c != -1 {
		t.Fatalf("2^100 < 2^100+1, got %d", c)
	}
}
