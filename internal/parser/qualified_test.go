package parser

import (
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/ast"
)

// T-137, G-15 (§11.1, §A.1 п.13): ModuleName в record_literal,
// record_pattern, constructor_pattern и type_primary; Format сохраняет
// квалификацию (round-trip).
func TestParseQualifiedNames(t *testing.T) {
	src := `module Main
import Accounts.User
type Order { user: Accounts.User.User, shape: Shapes.Shape }
fn f(Shapes.Circle(r)) -> r
fn g(Accounts.User.User{ name: n }) -> n
fn main() ->
    u = Accounts.User{ id: 1, name: "a" }
    v = Accounts.User.User{ ..u, id: 2 }
    match v
        Shapes.Empty -> 0
        Accounts.User{ id: i } -> i
`
	prog1, err := ParseProgram(ModeModule, src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	f1 := ast.Format(prog1)
	for _, want := range []string{
		"Accounts.User.User", "Shapes.Shape", "Shapes.Circle(r)",
		"Accounts.User.User{name: n}", "Accounts.User{ id: 1", "Accounts.User.User{ ..u",
		"Shapes.Empty ->", "Accounts.User{id: i}",
	} {
		if !strings.Contains(f1, want) {
			t.Errorf("Format lost %q:\n%s", want, f1)
		}
	}
	prog2, err := ParseProgram(ModeModule, f1)
	if err != nil {
		t.Fatalf("second parse: %v\nformatted:\n%s", err, f1)
	}
	if f2 := ast.Format(prog2); !ast.Equal(prog1, prog2) || f1 != f2 {
		t.Fatalf("round-trip mismatch\n--- f1 ---\n%s\n--- f2 ---\n%s", f1, f2)
	}
}

// Член модуля без "{" остаётся MemberExpr: `Shapes.Circle(2)` — вызов.
func TestParseQualifiedCallIsNotRecord(t *testing.T) {
	prog, err := ParseProgram(ModeModule, "module Main\nfn main() -> Shapes.Circle(2)\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := ast.Format(prog); !strings.Contains(got, "Shapes.Circle(2)") {
		t.Fatalf("Format:\n%s", got)
	}
}
