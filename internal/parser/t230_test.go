package parser

import (
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/ast"
)

// T-230: после DEDENT блочного выражения строка, начинающаяся с `(` или `[`,
// — новый стейтмент, а не вызов/индекс результата блока. Round-trip
// parse → Format → parse сохраняет AST.
func TestParseStmtAfterBlockExpr(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"tuple_after_match", `module Main
fn main() ->
    x = match 1
        a -> a
    (p, q) = (x, 2)
    print(p + q)
`},
		{"list_after_match", `module Main
fn main() ->
    x = match 1
        a -> a
    [p, q] = [x, 2]
    print(p + q)
`},
		{"paren_expr_after_if", `module Main
fn main() ->
    x = if true
        1
    else
        2
    (x)
`},
		{"call_after_lambda_block", `module Main
fn main() ->
    f = fn(y) ->
        y
    (f)(1)
`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prog1, err := ParseProgram(ModeModule, tc.src)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			f1 := ast.Format(prog1)
			prog2, err := ParseProgram(ModeModule, f1)
			if err != nil {
				t.Fatalf("second parse: %v\nformatted:\n%s", err, f1)
			}
			if !ast.Equal(prog1, prog2) || ast.Format(prog2) != f1 {
				t.Fatalf("round-trip mismatch\n--- f1 ---\n%s\n--- f2 ---\n%s", f1, ast.Format(prog2))
			}
		})
	}
}

// Вызов `(…)` на той же строке после однострочного выражения остаётся вызовом.
func TestParseSameLineCallNotSplit(t *testing.T) {
	prog, err := ParseProgram(ModeModule, "module Main\nfn main() ->\n    x = (fn(y) -> y)(1)\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p := ast.Pretty(prog); !strings.Contains(p, "(call (group") {
		t.Fatalf("want call expr, got:\n%s", p)
	}
}
