package ast_test

import (
	"testing"

	"github.com/it1ro/brig-lang/internal/ast"
	"github.com/it1ro/brig-lang/internal/parser"
)

type varCounter struct{ seen map[string]int }

func (c *varCounter) VisitNode(ast.Node) error       { return nil }
func (c *varCounter) VisitStmt(ast.Stmt) error       { return nil }
func (c *varCounter) VisitPattern(ast.Pattern) error { return nil }
func (c *varCounter) VisitType(ast.Type) error       { return nil }
func (c *varCounter) VisitDecl(ast.Decl) error       { return nil }
func (c *varCounter) VisitExpr(e ast.Expr) error {
	if v, ok := e.(ast.VariableExpr); ok {
		c.seen[v.Name()]++
	}
	return nil
}

// T-285 (#425): Walk спускается в инструкции BlockStmt ровно один раз.
func TestWalkEntersBlockBody(t *testing.T) {
	prog, err := parser.ParseProgram(parser.ModeScript, "fn f() ->\n    g()\n")
	if err != nil {
		t.Fatal(err)
	}
	c := &varCounter{seen: map[string]int{}}
	if err := ast.Walk(c, prog); err != nil {
		t.Fatal(err)
	}
	if c.seen["g"] != 1 {
		t.Fatalf("g visited %d times, want 1", c.seen["g"])
	}
}
