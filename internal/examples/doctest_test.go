package examples

import (
	"os"
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/parser"
	"github.com/it1ro/brig-lang/internal/repl"
	"github.com/it1ro/brig-lang/internal/vm"
)

// moduleEnv — фабрика ВМ и программа доктестов модуля src (как у
// brig test: образ CompileProgram и декларации для ввода и ответа).
func moduleEnv(t *testing.T, src string) (func() *vm.VM, *repl.Entry) {
	t.Helper()
	prog, err := parser.ParseProgram(parser.ModeModule, src)
	if err != nil {
		t.Fatal(err)
	}
	mods := []compiler.Module{{Name: prog.Module, Path: "m.brig", Prog: prog}}
	img, err := compiler.New().CompileProgram(mods)
	if err != nil {
		t.Fatal(err)
	}
	return vm.New, &repl.Entry{Mods: mods, Image: img}
}

// doctests прогоняет доктесты модуля src.
func doctests(t *testing.T, src string) []Result {
	t.Helper()
	newVM, entry := moduleEnv(t, src)
	return Doctests("m.brig", src, newVM, entry)
}

const doctestSrc = `## Модуль-пример.
module Main

## Сумма двух чисел.
##
## ` + "```brig repl" + `
## > add(1, 2)
## 3
## > add(2, 2) == 4
## true
## ` + "```" + `
fn add(a, b) -> a + b

## Не ноль.
##
##     ` + "```brig repl" + `
##     > nonzero(0)
##     raise (:zero, 0)
##     ` + "```" + `
fn nonzero(0) -> raise((:zero, 0))
fn nonzero(n) -> n

# Обычный комментарий — не документация:
# ` + "```brig repl" + `
# > add(1, 1)
# 5
# ` + "```" + `
`

func TestDoctestPasses(t *testing.T) {
	res := doctests(t, doctestSrc)
	if len(res) != 2 {
		t.Fatalf("want 2 doctests, got %d: %v", len(res), res)
	}
	for _, r := range res {
		if !r.OK {
			t.Errorf("%v", r)
		}
	}
	if res[0].Line != 6 || res[1].Line != 16 {
		t.Errorf("fence lines: %d, %d; want 6, 16", res[0].Line, res[1].Line)
	}
}

func TestDoctestMismatch(t *testing.T) {
	src := strings.Join([]string{
		"## ```brig repl",
		"## > add(1, 2)",
		"## 1 + 2",
		"## > add(1, 1)",
		"## 3",
		"## ```",
		"fn add(a, b) -> a + b",
		"",
		"## ```brig repl",
		"## > add(:a, 1)",
		"## 2",
		"## ```",
		"",
		"## ```brig repl",
		"## > missing(1)",
		"## ```",
	}, "\n")
	res := doctests(t, src)
	if len(res) != 3 {
		t.Fatalf("want 3 doctests, got %d: %v", len(res), res)
	}
	want := []struct {
		line int
		msg  string
	}{
		{5, "want 3, got 2"},
		{11, "want 2, got raise"},
		{15, "missing"},
	}
	for i, w := range want {
		r := res[i]
		if r.OK || r.Line != w.line || !strings.Contains(r.ErrMsg, w.msg) {
			t.Errorf("doctest %d: %v; want FAIL at line %d with %q", i, r, w.line, w.msg)
		}
	}
}

func TestDoctestBlocksOnlyRepl(t *testing.T) {
	src := "## ```brig\n## raise(:not_run)\n## ```\n##\n## ```brig norun\n## > raise(:not_run)\n## 1\n## ```\nfn f() -> 1\n"
	if res := doctests(t, src); len(res) != 0 {
		t.Fatalf("want no doctests, got %v", res)
	}
}

// Ответ доктеста видит декларации модуля файла (T-245, G-19): запись и
// варианты модуля в ответе, как в файле; привязки ввода ответу не видны.
func TestDoctestUserTypeAnswer(t *testing.T) {
	src, err := os.ReadFile("testdata/semver.brig")
	if err != nil {
		t.Fatal(err)
	}
	res := doctests(t, string(src))
	if len(res) != 5 {
		t.Fatalf("semver.brig: want 5 doctests, got %d: %v", len(res), res)
	}
	for _, r := range res {
		if !r.OK {
			t.Errorf("%v", r)
		}
	}

	inline := strings.Join([]string{
		"module Shapes",
		"type Pt { x: Int, y: Int }",
		"type Shape { Dot(Pt), Empty }",
		"## ```brig repl",
		"## > p = Pt{ x: 1, y: 2 }",
		"## > Dot(p)",
		"## Dot(Pt{ x: 1, y: 2 })",
		"## > origin()",
		"## Pt{ x: 0, y: 0 }",
		"## > Empty",
		"## Empty",
		"## > origin()",
		"## p",
		"## ```",
		"fn origin() -> Pt{ x: 0, y: 0 }",
	}, "\n")
	res = doctests(t, inline)
	if len(res) != 1 || res[0].OK || res[0].Line != 13 {
		t.Fatalf("want FAIL at line 13 (answer must not see input binding p), got %v", res)
	}
	passing := strings.Replace(inline, "## > origin()\n## p\n", "", 1)
	for _, r := range doctests(t, passing) {
		if !r.OK {
			t.Errorf("%v", r)
		}
	}
}

// Функция модуля с именем хелпера Repl затеняет хелпер во вводе
// доктеста (T-245, G-19, §11.4 «Затенение»); хелпер — как Repl.v.
func TestDoctestModuleFnShadowsHelper(t *testing.T) {
	src := strings.Join([]string{
		"module Short",
		"## ```brig repl",
		"## > v(1)",
		"## 101",
		"## > Repl.v(1)",
		"## 101",
		"## > time(2)",
		"## (:time, 2)",
		"## > h(3)",
		"## 3",
		"## ```",
		"pub fn v(x) -> x + 100",
		"pub fn time(x) -> (:time, x)",
		"fn h(x) -> x",
	}, "\n")
	res := doctests(t, src)
	if len(res) != 1 {
		t.Fatalf("want 1 doctest, got %v", res)
	}
	if !res[0].OK {
		t.Errorf("%v", res[0])
	}
}
