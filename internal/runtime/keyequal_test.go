package runtime

import (
	"math/big"
	"testing"
)

// T-86 (#111): KeyEqual — Equal, но Decimal равен только Decimal.
func TestKeyEqualDecimalStrictByKind(t *testing.T) {
	dec := func(s string) Value {
		v, ok := new(big.Rat).SetString(s)
		if !ok {
			t.Fatalf("bad decimal %q", s)
		}
		return Decimal(v)
	}
	one, oneF, oneD := Int(1), Float(1), dec("1")
	cases := []struct {
		name string
		a, b Value
		want bool
	}{
		{"dec-dec", oneD, dec("1.0"), true},
		{"dec-int", oneD, one, false},
		{"int-dec", one, oneD, false},
		{"dec-float", oneD, oneF, false},
		{"float-dec", oneF, oneD, false},
		{"int-float-exact", one, oneF, true},
		{"nested-tuple", Tuple(oneD, one), Tuple(one, one), false},
		{"nested-list", List(oneD), List(oneD), true},
		{"nested-list-int", List(oneD), List(one), false},
	}
	for _, c := range cases {
		if got := KeyEqual(c.a, c.b); got != c.want {
			t.Errorf("%s: KeyEqual = %v, want %v", c.name, got, c.want)
		}
	}
	// Equal не изменился: Decimal×Int по значению (§4.8).
	if !Equal(oneD, one) || Equal(oneD, oneF) {
		t.Error("Equal must keep Decimal×Int by value and Decimal×Float false")
	}
}
