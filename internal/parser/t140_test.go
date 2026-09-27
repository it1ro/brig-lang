package parser

import (
	"testing"

	"github.com/it1ro/brig-lang/internal/ast"
)

// T-140 (#203): формы лямбд §6.2 — `fn ->` без скобок при нуле
// параметров и короткая лямбда со списком имён `(a, b) -> expr`.

// letValue — правая часть первого `x = expr` в repl-программе.
func letValue(t *testing.T, src string) ast.Expr {
	t.Helper()
	prog, err := ParseProgram(ModeRepl, src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	if len(prog.Stmts) == 0 {
		t.Fatalf("parse %q: no statements", src)
	}
	let, ok := prog.Stmts[0].(ast.LetBind)
	if !ok {
		t.Fatalf("parse %q: first stmt is %T, want let", src, prog.Stmts[0])
	}
	return let.Val()
}

// inMain оборачивает строки в тело main модуля.
func inMain(body string) string {
	return "module M\nfn main() ->\n    " + body + "\n    0\n"
}

func TestParseFnArrowNoParams(t *testing.T) {
	cases := []string{
		"f = fn ->\n    y = 1\n    y + 1\n",
		"f = fn -> 42\n",
	}
	for _, src := range cases {
		lf, ok := letValue(t, src).(ast.LambdaFull)
		if !ok {
			t.Fatalf("%q: want LambdaFull, got %T", src, letValue(t, src))
		}
		if n := len(lf.ParamNames()); n != 0 {
			t.Fatalf("%q: want 0 params, got %d", src, n)
		}
	}
	// `fn () ->` остаётся валидным и даёт тот же узел.
	a := letValue(t, "f = fn () ->\n    1\n")
	b := letValue(t, "f = fn ->\n    1\n")
	if !ast.Equal(a, b) {
		t.Fatalf("fn () -> and fn -> differ:\n%s\n%s", a, b)
	}
	// Один параметр без скобок не вводится (§0.2).
	mustFail(t, ModeModule, inMain("f = fn x -> x"), "fn (x)")
	mustFail(t, ModeModule, inMain("f = fn x ->\n        x"), "fn (x)")
}

func TestParseShortLambdaMultiParam(t *testing.T) {
	ls, ok := letValue(t, "f = (a, b) -> a + b\n").(ast.LambdaShort)
	if !ok {
		t.Fatalf("want LambdaShort, got %T", letValue(t, "f = (a, b) -> a + b\n"))
	}
	if got := ls.ParamNames(); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("params = %v, want [a b]", got)
	}
	mustParse(t, ModeRepl, "r = fold(xs, 0, (acc, x) -> acc + x)\n")
	mustParse(t, ModeRepl, "f = (a, b, c) -> a\n")
	mustParse(t, ModeRepl, "f = (x) -> x\n")

	// `(a, b)` без `->` — по-прежнему кортеж.
	if e, ok := letValue(t, "t = (a, b)\n").(ast.LambdaShort); ok {
		t.Fatalf("(a, b) parsed as lambda %s, want tuple", e)
	}
	mustParse(t, ModeRepl, "t = f((a, b))\n")
	// Параметры короткой лямбды — только имена. Негативные случаи — в
	// module-режиме: REPL-режим пока молча отбрасывает хвост после
	// лишнего токена.
	mustFail(t, ModeModule, inMain("f = (a, 1) -> a"), "")
	// Блочного тела у стрелки нет.
	mustFail(t, ModeModule, inMain("f = (a, b) ->\n        a + b"), "")
}

// Таймаут `after` — не короткая лямбда: `(ms) ->` и `ms ->` после
// `after` остаются «выражение, стрелка, тело».
func TestParseRecvAfterNotShortLambda(t *testing.T) {
	for _, head := range []string{"after (ms) -> 1", "after ms -> 1"} {
		src := "module M\nfn wait(ms) ->\n    recv\n        _ -> 0\n    " + head + "\n"
		mustParse(t, ModeModule, src)
	}
}
