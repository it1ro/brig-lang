package parser

import (
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/ast"
)

// TestParseInterpolation — якорь T-53 / S-F1: выражение внутри \(...)
// попадает в AST (не остаётся сырым текстом в LiteralExpr).
func TestParseInterpolation(t *testing.T) {
	prog, err := ParseProgram(ModeRepl, "x = \"Привет, \\(name)!\"\n")
	if err != nil {
		t.Fatalf("ParseProgram: %v", err)
	}
	got := ast.Pretty(prog)
	if strings.Contains(got, `\(name)`) {
		t.Fatalf("interpolation still opaque in AST:\n%s", got)
	}
	if !strings.Contains(got, "(var name)") {
		t.Fatalf("want (var name) inside interpolation AST, got:\n%s", got)
	}
	if !strings.Contains(got, "(interp") {
		t.Fatalf("want (interp ...) node, got:\n%s", got)
	}

	// Nested expression.
	prog2, err := ParseProgram(ModeRepl, "x = \"sum: \\(1 + 2)\"\n")
	if err != nil {
		t.Fatalf("ParseProgram nested: %v", err)
	}
	got2 := ast.Pretty(prog2)
	if !strings.Contains(got2, "(+ (lit 1) (lit 2))") && !strings.Contains(got2, "(+ (lit 1)") {
		t.Fatalf("want binary + inside interp, got:\n%s", got2)
	}

	// Plain string stays a literal (not wrapped in Interp).
	prog3, err := ParseProgram(ModeRepl, "x = \"plain\"\n")
	if err != nil {
		t.Fatalf("ParseProgram plain: %v", err)
	}
	got3 := ast.Pretty(prog3)
	if strings.Contains(got3, "(interp") {
		t.Fatalf("plain string must not be Interp, got:\n%s", got3)
	}
	if !strings.Contains(got3, `(lit "plain")`) {
		t.Fatalf("want literal plain string, got:\n%s", got3)
	}
}
