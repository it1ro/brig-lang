package examples

import (
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/parser"
	"github.com/it1ro/brig-lang/internal/vm"
)

// moduleVM — фабрика ВМ с загруженными функциями модуля src.
func moduleVM(t *testing.T, src string) func() *vm.VM {
	t.Helper()
	prog, err := parser.ParseProgram(parser.ModeModule, src)
	if err != nil {
		t.Fatal(err)
	}
	img, err := compiler.New().Compile(prog)
	if err != nil {
		t.Fatal(err)
	}
	return func() *vm.VM {
		m := vm.New()
		for name, fn := range img.Functions {
			m.DefineGlobal(name, vm.FuncValue(fn))
		}
		return m
	}
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
	res := Doctests("m.brig", doctestSrc, moduleVM(t, doctestSrc))
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
	res := Doctests("m.brig", src, moduleVM(t, src))
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
	if res := Doctests("m.brig", src, moduleVM(t, src)); len(res) != 0 {
		t.Fatalf("want no doctests, got %v", res)
	}
}
