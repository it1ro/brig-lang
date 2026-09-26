package parser

import (
	"testing"

	"github.com/it1ro/brig-lang/internal/ast"
)

// S-F2 / T-50: параметры fn — []ast.Pattern, guard — ast.Expr (не строки).
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

	if _, ok := cl.Params[0].(ast.PatternCtor); !ok {
		t.Fatalf("Params[0]: type %T, want PatternCtor (Some(x))", cl.Params[0])
	}
	sp, ok := cl.Params[1].(ast.SpreadPattern)
	if !ok {
		t.Fatalf("Params[1]: type %T, want SpreadPattern (..rest)", cl.Params[1])
	}
	if sp.SpreadName() != "rest" {
		t.Fatalf("SpreadName = %q, want rest", sp.SpreadName())
	}
	if cl.Guard == nil {
		t.Fatal("Guard: nil, want non-nil Expr")
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
	if _, ok := lcl.Params[0].(ast.PatternCtor); !ok {
		t.Fatalf("local Params[0]: type %T, want PatternCtor", lcl.Params[0])
	}
	if lcl.Guard == nil {
		t.Fatal("local Guard: nil, want Expr")
	}
}
