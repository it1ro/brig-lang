package compiler_test

import (
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/parser"
	"github.com/it1ro/brig-lang/internal/vm"
)

// ---- D-1: variadic rest is List (§6.3) ----

func TestVariadicRestIsList(t *testing.T) {
	runModule(t, `module Main
fn collect(x, ..rest) -> rest
fn main() ->
    r = collect(1, 2, 3, 4)
    assert(r == [2, 3, 4])
`)
}

func TestVariadicOnlyRest(t *testing.T) {
	runModule(t, `module Main
fn collect(..args) -> args
fn main() ->
    assert(collect(1, 2, 3) == [1, 2, 3])
    assert(collect() == [])
`)
}

func TestVariadicSum(t *testing.T) {
	t.Skip("K-8: only first fn clause compiles; multi-clause variadic outside scope")
	runModule(t, `module Main
fn sum() -> 0
fn sum(x, ..rest) -> x + sum(..rest)
fn main() ->
    assert(sum(1, 2, 3, 4) == 10)
    assert(sum() == 0)
`)
}

// ---- D-2: native arity error is fatal, not Go-panic ----

func TestNativeArityErrorTooFew(t *testing.T) {
	err := runModuleErr(t, `module Main
fn main() ->
    to_int()
`)
	if err == nil {
		t.Fatal("want function_clause error, got nil")
	}
	if !strings.Contains(err.Error(), "function_clause") {
		t.Fatalf("want function_clause, got: %v", err)
	}
}

func TestNativeArityErrorTooMany(t *testing.T) {
	err := runModuleErr(t, `module Main
fn main() ->
    to_int(1, 2)
`)
	if err == nil {
		t.Fatal("want function_clause error, got nil")
	}
	if !strings.Contains(err.Error(), "function_clause") {
		t.Fatalf("want function_clause, got: %v", err)
	}
}

// ---- D-3: recv else msg alias (§12.4) ----

func TestRecvElseAlias(t *testing.T) {
	runModule(t, `module Main
fn worker(from) ->
    x = recv
        :a -> :got_a
    else msg
        to_str(msg)
    send(from, x)

fn main() ->
    me = self()
    pid = spawn(() -> worker(me))
    send(pid, :b)
    reply = recv
        r -> r
    assert(reply == ":b")
`)
}

// ---- D-4: nested upvalue (§7) ----

func TestNestedUpvalue(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    x = 42
    f = fn () ->
        g = () -> x
        g()
    assert(f() == 42)
`)
}

func TestNestedUpvalueDeep(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    x = 7
    f = fn () ->
        g = fn () ->
            h = () -> x
            h()
        g()
    assert(f() == 7)
`)
}

// ---- D-5: trap ensure discriminates :no_error by Bool flag (§10.3) ----

func TestTrapEnsureNoErrorAtom(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    result = trap
        ensure :cleanup
        raise(:no_error)
    assert(result == Error(:no_error))
`)
}

func TestTrapEnsureSuccessStillOk(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    result = trap
        ensure :cleanup
        1 + 1
    assert(result == Ok(2))
`)
}

// ---- TCO: explicit TAILCALL emitted by compiler ----

func TestTailCallEmitted(t *testing.T) {
	img := compileModule(t, `module Main
fn sum_to(n, acc) ->
    if n == 0 then acc else sum_to(n - 1, acc + n)
fn main() ->
    sum_to(10, 0)
`)
	dis := img.Functions["sum_to"].Disassemble()
	if !strings.Contains(dis, "TAILCALL") {
		t.Fatalf("sum_to lacks TAILCALL:\n%s", dis)
	}
	if strings.Contains(dis, " CALL ") {
		t.Fatalf("sum_to has non-tail CALL:\n%s", dis)
	}
}

func TestTailCallArgOrder(t *testing.T) {
	runModule(t, `module Main
fn go(n, acc) ->
    if n == 0 then acc else go(n - 1, acc + n)
fn main() ->
    assert(go(5, 0) == 15)
    assert(go(100000, 0) == 5000050000)
`)
}

func TestTailCallGCD(t *testing.T) {
	runModule(t, `module Main
fn gcd(a, b) ->
    if b == 0 then a else gcd(b, a rem b)
fn main() ->
    assert(gcd(48, 18) == 6)
    assert(gcd(270, 192) == 6)
`)
}

// ---- K-5: native не удерживает слайс аргументов ----

func TestListLiteralViaNative(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    xs = [1, 2, 3]
    ys = map(x -> x * 2, xs)
    assert(xs == [1, 2, 3])
    assert(ys == [2, 4, 6])
`)
}

func TestListLiteralViaNativeFilter(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    xs = [1, 2, 3, 4, 5]
    ys = filter(x -> x rem 2 == 0, xs)
    assert(xs == [1, 2, 3, 4, 5])
    assert(ys == [2, 4])
`)
}

// ---- helpers ----

func runModuleErr(t *testing.T, src string) error {
	t.Helper()
	prog, err := parser.ParseProgram(parser.ModeModule, src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	img, err := compiler.New().Compile(prog)
	if err != nil {
		return err
	}
	if img.Main == nil {
		t.Fatal("no main")
	}
	m := vm.New()
	for name, fn := range img.Functions {
		m.DefineGlobal(name, vm.FuncValue(fn))
	}
	_, rerr := m.RunMain(m.Global("main"))
	return rerr
}

func compileModule(t *testing.T, src string) *compiler.ProgramImage {
	t.Helper()
	prog, err := parser.ParseProgram(parser.ModeModule, src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	img, err := compiler.New().Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return img
}
