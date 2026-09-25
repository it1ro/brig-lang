package ast_test

import (
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/ast"
	"github.com/it1ro/brig-lang/internal/parser"
)

// S-F13: Pretty должен печатать fn-декларации.
func TestAuditPrettyPrintsFuncDecl(t *testing.T) {
	prog, err := parser.ParseProgram(parser.ModeModule, "fn main() -> 1\n")
	if err != nil {
		t.Fatal(err)
	}
	if got := ast.Pretty(prog); !strings.Contains(got, "(fn main") {
		t.Errorf("Pretty drops FuncDecl: %q", got)
	}
}

// S-F13: Walk должен заходить в Decl (VisitDecl), а не останавливаться на Expr.
func TestWalkVisitsFuncDecl(t *testing.T) {
	prog, err := parser.ParseProgram(parser.ModeModule, "fn main() -> 1\n")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	err = ast.Walk(&declNameVisitor{names: &names}, prog)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "main" {
		t.Fatalf("Walk skipped FuncDecl, VisitDecl names = %v", names)
	}
}

type declNameVisitor struct {
	names *[]string
}

func (declNameVisitor) VisitNode(ast.Node) error       { return nil }
func (declNameVisitor) VisitExpr(ast.Expr) error       { return nil }
func (declNameVisitor) VisitStmt(ast.Stmt) error       { return nil }
func (declNameVisitor) VisitPattern(ast.Pattern) error { return nil }
func (declNameVisitor) VisitType(ast.Type) error       { return nil }
func (v *declNameVisitor) VisitDecl(d ast.Decl) error {
	fd, ok := d.(ast.FuncDecl)
	if !ok {
		return nil
	}
	*v.names = append(*v.names, fd.FnName())
	return nil
}
