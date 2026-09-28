package ast_test

import (
	"testing"

	"github.com/it1ro/brig-lang/internal/ast"
	"github.com/it1ro/brig-lang/internal/parser"
)

// T-143 (#267): `pub` перед каждым клозом fn сохраняется в AST и в Format.
func TestFormatPubFn(t *testing.T) {
	cases := []struct{ src, want string }{
		{"module M\npub fn f(x) -> x\n", "module M\npub fn f(x) ->\n    x\n"},
		{"module M\npub fn f(0) -> 1\npub fn f(n) -> n\n", "module M\npub fn f(0) ->\n    1\npub fn f(n) ->\n    n\n"},
		{"module M\nfn f(x) -> x\n", "module M\nfn f(x) ->\n    x\n"},
	}
	for _, c := range cases {
		a1, err := parser.ParseProgram(parser.ModeModule, c.src)
		if err != nil {
			t.Fatalf("parse %q: %v", c.src, err)
		}
		f1 := ast.Format(a1)
		if f1 != c.want {
			t.Fatalf("Format(%q) = %q, want %q", c.src, f1, c.want)
		}
		a2, err := parser.ParseProgram(parser.ModeModule, f1)
		if err != nil {
			t.Fatalf("re-parse %q: %v", f1, err)
		}
		if !ast.Equal(a1, a2) {
			t.Fatalf("round-trip mismatch for %q", c.src)
		}
	}

	pub, err := parser.ParseProgram(parser.ModeModule, "module M\npub fn f(x) -> x\n")
	if err != nil {
		t.Fatal(err)
	}
	priv, err := parser.ParseProgram(parser.ModeModule, "module M\nfn f(x) -> x\n")
	if err != nil {
		t.Fatal(err)
	}
	if ast.Equal(pub, priv) {
		t.Fatal("pub fn and fn must differ in ast.Equal")
	}
	if fd := pub.Decls[0].(ast.FuncDecl); !fd.FuncClauses()[0].Pub {
		t.Fatal("pub flag lost")
	}

	// Локальная fn pub не принимает — ошибка парсинга.
	if _, err := parser.ParseProgram(parser.ModeModule, "module M\nfn g() ->\n    pub fn h() -> 1\n    h()\n"); err == nil {
		t.Fatal("pub on local fn must be a parse error")
	}
}
