package runtime

import (
	"math/big"
	"testing"
)

func TestSerializeRejectsFunction(t *testing.T) {
	fn := Func(&FuncValue{Name: "test", Arity: 1, IsNative: true})
	if err := Serialize(fn); err == nil {
		t.Fatal("Serialize(function) = nil, want error")
	}
}

func TestSerializeRejectsClosure(t *testing.T) {
	cl := MakeClosure("lambda$", 0, nil, nil)
	if err := Serialize(cl); err == nil {
		t.Fatal("Serialize(closure) = nil, want error")
	}
}

func TestSerializeRejectsPidRef(t *testing.T) {
	pid := Value{Kind: KindPid, Pid: 1}
	ref := Value{Kind: KindRef, Ref: 1}
	if err := Serialize(pid); err == nil {
		t.Fatal("Serialize(pid) = nil, want error")
	}
	if err := Serialize(ref); err == nil {
		t.Fatal("Serialize(ref) = nil, want error")
	}
}

func TestSerializeAcceptsPrimitives(t *testing.T) {
	cases := []Value{Unit, Bool(true), Int(42), Float(3.14), Str("hi"), Atom("ok")}
	for _, v := range cases {
		if err := Serialize(v); err != nil {
			t.Errorf("Serialize(%s) = %v, want nil", v.Inspect(), err)
		}
	}
}

func TestSerializeRejectsNestedFunction(t *testing.T) {
	fn := Func(&FuncValue{Name: "nested", Arity: 0})
	list := List(Int(1), fn, Int(3))
	if err := Serialize(list); err == nil {
		t.Fatal("Serialize([1, fn, 3]) = nil, want error")
	}
}

func TestMFAIsSerializable(t *testing.T) {
	mfa := MFA("Math", "fib", []Value{Int(10)})
	if err := Serialize(mfa); err != nil {
		t.Fatalf("Serialize(MFA) = %v, want nil", err)
	}
}

func TestSerializeAcceptsDecimal(t *testing.T) {
	r, err := ParseDecimal("1.50")
	if err != nil {
		t.Fatal(err)
	}
	if err := Serialize(Decimal(r)); err != nil {
		t.Fatalf("Serialize(decimal) = %v, want nil", err)
	}
}

func TestDecimalTrailingZerosEqual(t *testing.T) {
	a, _ := ParseDecimal("1.50")
	b, _ := ParseDecimal("1.5")
	if !Equal(Decimal(a), Decimal(b)) {
		t.Fatal("1.50 != 1.5, want equal")
	}
	if FormatDecimal(a) != "1.5" {
		t.Fatalf("FormatDecimal(1.50) = %q, want \"1.5\"", FormatDecimal(a))
	}
}

func TestDecimalNonTerminating(t *testing.T) {
	a, _ := ParseDecimal("1")
	b, _ := ParseDecimal("3")
	q := new(big.Rat).Quo(a, b)
	if got := FormatDecimal(q); got != "1/3" {
		t.Fatalf("FormatDecimal(1/3) = %q, want \"1/3\"", got)
	}
}
