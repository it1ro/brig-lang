package runtime

import (
	"math/big"
	"testing"
)

// TestPortOpaque — Port (§12.12): печать #<port N>, равенство по identity,
// не сериализуется в JSON (§14.8).
func TestPortOpaque(t *testing.T) {
	h := &PortHandle{ID: 3, Owner: 1}
	p := Value{Kind: KindPort, Port: h}
	if got := p.Inspect(); got != "#<port 3>" {
		t.Fatalf("Inspect = %q", got)
	}
	closed := Value{Kind: KindPort, Port: &PortHandle{ID: 3, Owner: 1, Closed: true}}
	if !Equal(p, closed) || !KeyEqual(p, closed) {
		t.Fatal("ports with one ID must be equal")
	}
	if Equal(p, Value{Kind: KindPort, Port: &PortHandle{ID: 4, Owner: 1}}) {
		t.Fatal("ports with different IDs must differ")
	}
	if Equal(p, Value{Kind: KindRef, Ref: 3}) {
		t.Fatal("port must differ from ref")
	}
	if _, err := JSONEncode(p); err == nil {
		t.Fatal("JSONEncode(port) = nil, want error")
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
