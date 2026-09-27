package ast_test

import (
	"testing"

	"github.com/it1ro/brig-lang/internal/ast"
	"github.com/it1ro/brig-lang/internal/parser"
)

// T-140 (#203): канон пустой полной лямбды — `fn ->` (§6.2), короткая
// лямбда со списком имён печатается как `(a, b) -> expr`.
func TestFormatFnEmptyParensCanon(t *testing.T) {
	cases := []struct{ src, want string }{
		{"f = fn () ->\n    1\n", "f = fn ->\n    1\n"},
		{"f = fn ->\n    1\n", "f = fn ->\n    1\n"},
		{"f = fn (x) ->\n    x\n", "f = fn (x) ->\n    x\n"},
		{"f = (a, b) -> a + b\n", "f = (a, b) -> a + b\n"},
		{"f = (x) -> x\n", "f = x -> x\n"},
	}
	for _, c := range cases {
		a1, err := parser.ParseProgram(parser.ModeRepl, c.src)
		if err != nil {
			t.Fatalf("parse %q: %v", c.src, err)
		}
		f1 := ast.Format(a1)
		if f1 != c.want {
			t.Fatalf("Format(%q) = %q, want %q", c.src, f1, c.want)
		}
		a2, err := parser.ParseProgram(parser.ModeRepl, f1)
		if err != nil {
			t.Fatalf("re-parse %q: %v", f1, err)
		}
		if !ast.Equal(a1, a2) {
			t.Fatalf("round-trip mismatch for %q", c.src)
		}
		if f2 := ast.Format(a2); f2 != f1 {
			t.Fatalf("not idempotent: %q -> %q", f1, f2)
		}
	}
}
