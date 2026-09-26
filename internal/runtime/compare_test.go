package runtime

import (
	"math/big"
	"testing"
)

func TestCompareTermOrderAcrossKinds(t *testing.T) {
	// по одному представителю каждого вида в порядке §7.4
	ordered := []struct {
		name string
		v    Value
	}{
		{"number", Int(1)},
		{"bool", Bool(true)},
		{"range", Range(1, 2)},
		{"atom", Atom("a")},
		{"bytes", Bytes([]byte("x"))},
		{"str", Str("x")},
		{"unit", Unit},
		{"tuple", Tuple(Int(1))},
		{"vector", Vector(Int(1))},
		{"list", List(Int(1))},
		{"map", Map(nil)},
		{"set", Set()},
		{"nominal record", rec("User", "id", Int(1))},
		{"variant", Variant("None")},
		{"anonymous record", rec("", "a", Int(1))},
		{"pid", Value{Kind: KindPid, Pid: 1}},
		{"ref", Value{Kind: KindRef, Ref: 1}},
	}
	for i := 0; i+1 < len(ordered); i++ {
		a, b := ordered[i], ordered[i+1]
		for _, tc := range []struct {
			x, y Value
			want int
		}{{a.v, b.v, -1}, {b.v, a.v, 1}} {
			got, err := Compare(tc.x, tc.y)
			if err != nil || got != tc.want {
				t.Errorf("Compare(%s, %s) = %d, %v; want %d", a.name, b.name, got, err, tc.want)
			}
		}
	}
}

func TestCompareWithinKind(t *testing.T) {
	m := func(kv ...Value) Value {
		var es []MapEntry
		for i := 0; i < len(kv); i += 2 {
			es = append(es, MapEntry{kv[i], kv[i+1]})
		}
		return Map(es)
	}
	cases := []struct {
		name string
		a, b Value
		want int
	}{
		{"int<float", Int(1), Float(1.5), -1},
		{"decimal=int", Decimal(big.NewRat(2, 1)), Int(2), 0},
		{"bool", Bool(false), Bool(true), -1},
		{"range start", Range(1, 9), Range(2, 3), -1},
		{"range end", Range(1, 2), Range(1, 3), -1},
		{"atom", Atom("a"), Atom("b"), -1},
		{"atom ?", Atom("a"), Atom("a?"), -1},
		{"bytes", Bytes([]byte{1}), Bytes([]byte{2}), -1},
		{"str", Str("a"), Str("b"), -1},
		{"unit<tuple", Unit, Tuple(Int(0)), -1},
		{"tuple len", Tuple(Int(9)), Tuple(Int(1), Int(1)), -1},
		{"tuple elems", Tuple(Int(1), Int(2)), Tuple(Int(1), Int(3)), -1},
		{"vector len", Vector(Int(9)), Vector(Int(1), Int(1)), -1},
		{"list len", List(Int(9)), List(Int(1), Int(1)), -1},
		{"list elems", List(Int(1), Int(2)), List(Int(1), Int(3)), -1},
		{"list mixed kinds", List(Int(1)), List(Str("a")), -1},
		{"map size", m(Int(9), Int(9)), m(Int(1), Int(1), Int(2), Int(2)), -1},
		{"map sorted pairs", m(Int(2), Int(1), Int(1), Int(1)), m(Int(1), Int(1), Int(2), Int(2)), -1},
		{"map equal any order", m(Int(2), Int(2), Int(1), Int(1)), m(Int(1), Int(1), Int(2), Int(2)), 0},
		{"set size", Set(Int(9)), Set(Int(1), Int(2)), -1},
		{"set sorted", Set(Int(3), Int(1)), Set(Int(1), Int(2)), 1},
		{"None<Some", Variant("None"), Variant("Some", Int(0)), -1},
		{"Ok<Error", Variant("Ok", Int(9)), Variant("Error", Int(0)), -1},
		{"Some fields", Variant("Some", Int(1)), Variant("Some", Int(2)), -1},
		{"nominal by type name", rec("A", "z", Int(9)), rec("B", "a", Int(1)), -1},
		{"nominal by field count", rec("U", "a", Int(9)), rec("U", "a", Int(1), "b", Int(1)), -1},
		{"nominal by field name", rec("U", "a", Int(9)), rec("U", "b", Int(1)), -1},
		{"nominal by value", rec("U", "a", Int(1), "b", Int(2)), rec("U", "a", Int(1), "b", Int(3)), -1},
		{"nominal literal order", rec("U", "b", Int(2), "a", Int(1)), rec("U", "a", Int(1), "b", Int(2)), 0},
		{"anon by value", rec("", "a", Int(1)), rec("", "a", Int(2)), -1},
		{"anon by field name", rec("", "a", Int(9)), rec("", "b", Int(1)), -1},
		{"anon literal order", rec("", "b", Int(2), "a", Int(1)), rec("", "a", Int(1), "b", Int(2)), 0},
		{"anon literal order 2", rec("", "b", Int(3), "a", Int(1)), rec("", "a", Int(1), "b", Int(2)), 1},
		{"Set<record", Set(Int(1)), rec("User", "id", Int(1)), -1},
		{"record<None", rec("User", "id", Int(1)), Variant("None"), -1},
		{"Error<anon", Variant("Error", Int(1)), rec("", "a", Int(1)), -1},
		{"pid", Value{Kind: KindPid, Pid: 1}, Value{Kind: KindPid, Pid: 2}, -1},
		{"ref", Value{Kind: KindRef, Ref: 2}, Value{Kind: KindRef, Ref: 1}, 1},
	}
	for _, tc := range cases {
		got, err := Compare(tc.a, tc.b)
		if err != nil || got != tc.want {
			t.Errorf("%s: Compare = %d, %v; want %d", tc.name, got, err, tc.want)
		}
	}
}

func TestCompareErrors(t *testing.T) {
	dec := Decimal(big.NewRat(1, 2))
	fn := Value{Kind: KindFunction}
	for name, pair := range map[string][2]Value{
		"decimal/float":      {dec, Float(0.5)},
		"float/decimal":      {Float(0.5), dec},
		"function/int":       {fn, Int(1)},
		"int/function":       {Int(1), fn},
		"function/fn":        {fn, fn},
		"list/decimal-float": {List(dec), List(Float(1))},
	} {
		if _, err := Compare(pair[0], pair[1]); err == nil {
			t.Errorf("%s: want :type_error", name)
		}
	}
}
