package parser

import (
	"testing"

	"github.com/it1ro/brig-lang/internal/ast"
)

// S-F2 / T-50: параметры fn — []ast.Pattern, guard — ast.Expr (не строки).
// На текущем коде Params/Guard — string; тест обязан падать до фикса.
func TestParseFnPatternParams(t *testing.T) {
	src := `module M
fn head(Some(x), ..rest) when x > 0 -> x
fn main() ->
    fn local(Ok(v)) when v -> v
    local(Ok(1))
`
	prog, err := ParseProgram(ModeModule, src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(prog.Decls) < 1 {
		t.Fatalf("want FuncDecl, got %d decls", len(prog.Decls))
	}
	fd, ok := prog.Decls[0].(ast.FuncDecl)
	if !ok {
		t.Fatalf("want FuncDecl, got %T", prog.Decls[0])
	}
	clauses := fd.FuncClauses()
	if len(clauses) != 1 {
		t.Fatalf("want 1 clause, got %d", len(clauses))
	}
	cl := clauses[0]
	if len(cl.Params) != 2 {
		t.Fatalf("want 2 params, got %d", len(cl.Params))
	}

	// T-50 stores params as Pattern and guard as Expr. String storage is the bug.
	assertNotString := func(v any, label string) {
		t.Helper()
		if s, ok := v.(string); ok {
			t.Fatalf("%s still stored as string %q; want AST node (T-50)", label, s)
		}
	}
	assertNotString(cl.Params[0], "Params[0]")
	assertNotString(cl.Params[1], "Params[1]")
	assertNotString(cl.Guard, "Guard")

	if _, ok := any(cl.Params[0]).(ast.Pattern); !ok {
		t.Fatalf("Params[0]: type %T, want ast.Pattern", cl.Params[0])
	}
	if _, ok := any(cl.Params[1]).(ast.Pattern); !ok {
		t.Fatalf("Params[1]: type %T, want ast.Pattern", cl.Params[1])
	}
	if g, ok := any(cl.Guard).(ast.Expr); !ok || g == nil {
		t.Fatalf("Guard: type %T value %v, want non-nil ast.Expr", cl.Guard, cl.Guard)
	}

	main, ok := prog.Decls[1].(ast.FuncDecl)
	if !ok {
		t.Fatalf("want main FuncDecl, got %T", prog.Decls[1])
	}
	body := main.FuncClauses()[0].Body
	if body == nil || len(body.Stmts()) == 0 {
		t.Fatal("main body empty")
	}
	local, ok := body.Stmts()[0].(ast.LocalFnDecl)
	if !ok {
		t.Fatalf("want LocalFnDecl, got %T", body.Stmts()[0])
	}
	lcl := local.Clauses()[0]
	if len(lcl.Params) != 1 {
		t.Fatalf("local: want 1 param, got %d", len(lcl.Params))
	}
	assertNotString(lcl.Params[0], "local Params[0]")
	assertNotString(lcl.Guard, "local Guard")
	if _, ok := any(lcl.Params[0]).(ast.Pattern); !ok {
		t.Fatalf("local Params[0]: type %T, want ast.Pattern", lcl.Params[0])
	}
	if g, ok := any(lcl.Guard).(ast.Expr); !ok || g == nil {
		t.Fatalf("local Guard: type %T value %v, want non-nil ast.Expr", lcl.Guard, lcl.Guard)
	}
}
