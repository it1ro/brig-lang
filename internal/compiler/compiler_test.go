package compiler_test

import (
	"testing"

	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/parser"
	"github.com/it1ro/brig-lang/internal/vm"
)

func runModule(t *testing.T, src string) {
	t.Helper()
	prog, err := parser.ParseProgram(parser.ModeModule, src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	img, err := compiler.New().Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if img.Main == nil {
		t.Fatal("no main")
	}
	m := vm.New()
	for name, fn := range img.Functions {
		m.DefineGlobal(name, vm.FuncValue(fn))
	}
	if _, err := m.Call(m.Global("main"), nil); err != nil {
		t.Fatalf("run: %v", err)
	}
}

func TestHello(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    print("hi")
`)
}

func TestArith(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    x = 2 + 3 * 4
    print(x)
`)
}

func TestRecursion(t *testing.T) {
	runModule(t, `module Main
fn fib(n) ->
    if n < 2 then n else fib(n - 1) + fib(n - 2)
fn main() ->
    print(fib(10))
`)
}
