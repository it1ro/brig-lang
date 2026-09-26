package runtime

import (
	"math/big"
	"testing"
)

func TestMatchEqualIsExactByKind(t *testing.T) {
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
		{"int-int", one, Int(1), true},
		{"int-float", one, oneF, false},
		{"float-int", oneF, one, false},
		{"int-dec", one, oneD, false},
		{"dec-int", oneD, one, false},
		{"float-float", oneF, Float(1), true},
		{"dec-dec", oneD, dec("1.0"), true},
		{"tuple-mixed", Tuple(one, one), Tuple(one, oneF), false},
		{"tuple-same", Tuple(one, oneF), Tuple(one, oneF), true},
		{"list-mixed", List(one), List(oneF), false},
		{"str", Str("a"), Str("a"), true},
	}
	for _, c := range cases {
		if got := MatchEqual(c.a, c.b); got != c.want {
			t.Errorf("%s: MatchEqual = %v, want %v", c.name, got, c.want)
		}
	}
}
