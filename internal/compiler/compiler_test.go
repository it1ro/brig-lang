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

func TestAndOr(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    a = true and false
    b = false or true
    c = true and true
    d = false or false
    print(a, b, c, d)
`)
}

func TestLocalFn(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    fn add(x, y) -> x + y
    print(add(1, 2))
`)
}

func TestClosure(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    x = 10
    f = () -> x
    print(f())
`)
}

func TestMutualRecursion(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    fn is_even(n) -> if n == 0 then true else is_odd(n - 1)
    fn is_odd(n) -> if n == 0 then false else is_even(n - 1)
    print(is_even(10))
`)
}

func TestLambdaShort(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    xs = [1, 2, 3]
    ys = map(x -> x * 2, xs)
    print(ys)
`)
}

func TestClosureCapture(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    base = 100
    add_base = x -> base + x
    print(add_base(5))
`)
}

func TestLocalFnRecursion(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    fn fact(n) -> if n <= 1 then 1 else n * fact(n - 1)
    print(fact(5))
`)
}

// ---- trap / ensure (v0.4.7, §10.2/§10.3) ----

func TestTrapInlineOk(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    result = trap(1 + 1)
    print(result)
`)
}

func TestTrapInlineError(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    result = trap(raise(:boom))
    print(result)
`)
}

func TestTrapBlockOk(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    result = trap
        x = 1
        x + 2
    print(result)
`)
}

func TestTrapBlockError(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    result = trap
        x = 1
        raise(:oops)
    print(result)
`)
}

func TestTrapWithEnsure(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    result = trap
        print("body")
        ensure print("cleanup-1")
        ensure print("cleanup-2")
        :ok
    print(result)
`)
}

func TestTrapNested(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    inner = trap(raise(:inner))
    outer = trap(print(inner))
    print(outer)
`)
}

func TestTrapCatchesDivisionByZero(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    result = trap(1 div 0)
    print(result)
`)
}

func TestTrapBlockPropagatesThroughFn(t *testing.T) {
	runModule(t, `module Main
fn boom() -> raise(:deep)
fn main() ->
    result = trap(boom())
    print(result)
`)
}
