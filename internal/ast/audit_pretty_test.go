package ast_test

import (
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/ast"
	"github.com/it1ro/brig-lang/internal/parser"
)

// S-F13: Pretty должен печатать fn-декларации (golden *.ast сейчас «(program )»).
func TestAuditPrettyPrintsFuncDecl(t *testing.T) {
	t.Skip("blocked: T-11")
	prog, err := parser.ParseProgram(parser.ModeModule, "fn main() -> 1\n")
	if err != nil {
		t.Fatal(err)
	}
	if got := ast.Pretty(prog); !strings.Contains(got, "(fn main") {
		t.Errorf("Pretty drops FuncDecl: %q", got)
	}
}
