package parser

import (
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/ast"
)

// T-253: `cond` разворачивается во вложенные if (§8.4); последний else —
// raise((:cond_clause, ())).
func TestCondExpr(t *testing.T) {
	src := `module Main
fn f(n) ->
    cond
        n < 0 -> :neg
        n == 0 -> :zero
        true -> :pos
`
	prog, err := ParseProgram(ModeModule, src)
	if err != nil {
		t.Fatal(err)
	}
	want := `(if (< (var n) (lit 0)) (:neg) ` +
		`(else (if (== (var n) (lit 0)) (:zero) ` +
		`(else (if (lit true) (:pos) ` +
		`(else (call (var raise) (call (var ()) (:cond_clause) (lit ()))))))))`
	if got := ast.Pretty(prog); !containsFlat(got, want) {
		t.Fatalf("AST:\n%s\nwant nested if:\n%s", got, want)
	}

	// Ветка-блок и cond в позиции выражения; условие `ok` — не лямбда.
	prog, err = ParseProgram(ModeModule, `module Main
fn g(ok) ->
    x = cond
        ok ->
            y = 1
            y
        true -> 2
    x
`)
	if err != nil {
		t.Fatal(err)
	}
	if got := ast.Pretty(prog); !containsFlat(got, "(if (var ok) (block") {
		t.Fatalf("AST:\n%s", got)
	}

	if _, err := ParseProgram(ModeModule, "module Main\nfn h() ->\n    cond 1 -> 2\n"); err == nil {
		t.Fatal("inline cond: want parse error")
	}
}

// containsFlat ищет want в got, сведя пробельные последовательности к одному.
func containsFlat(got, want string) bool {
	return strings.Contains(strings.Join(strings.Fields(got), " "), want)
}
