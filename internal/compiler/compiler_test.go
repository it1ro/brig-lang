package compiler_test

import (
	"testing"

	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/parser"
	"github.com/it1ro/brig-lang/internal/vm"
)

// runModule прогоняет модуль через scheduler (RunMain), чтобы recv
// и другие акторные примитивы работали.
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
	if _, err := m.RunMain(m.Global("main")); err != nil {
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

func TestTrapEnsureRaisesInSuccess(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    result = trap
        print("body")
        ensure raise(:cleanup_failed)
        :ok
    print(result)
`)
}

func TestTrapEnsureRaisesInBody(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    result = trap
        raise(:body_failed)
        ensure raise(:cleanup_failed)
    print(result)
`)
}

func TestTrapEnsureLifo(t *testing.T) {
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

func TestTrapEnsureLifoOnError(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    result = trap
        print("body")
        ensure print("cleanup-1")
        ensure print("cleanup-2")
        raise(:boom)
    print(result)
`)
}

func TestTrapNoEnsureUnchanged(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    print(trap(1 + 1))
    print(trap(raise(:x)))
`)
}

func TestTrapInExpression(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    x = 1 + 0
    print(x)
`)
}

// ---- v0.4.8: акторы ----

func TestSpawnAndSend(t *testing.T) {
	runModule(t, `module Main
fn worker_loop() ->
    x = recv
        (:stop) -> :ok
        msg -> msg
    print(x)

fn main() ->
    pid = spawn(worker_loop)
    send(pid, :hello)
    send(pid, :stop)
    recv
        :never -> :never
    after 50 -> :ok
`)
}

func TestSpawnLinked(t *testing.T) {
	runModule(t, `module Main
fn worker() ->
    x = recv
        msg -> msg
    x

fn main() ->
    pid = spawn_linked(worker)
    send(pid, :hi)
    recv
        (:down, _, _) -> :done
        _ -> :other
    after 50 -> :timeout
`)
}

func TestSelfIsPid(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    me = self()
    print(me)
`)
}

func TestWatchAndDown(t *testing.T) {
	runModule(t, `module Main
fn quick() -> :done

fn main() ->
    pid = spawn(quick)
    ref = watch(pid)
    x = recv
        (:down, r, reason) -> (r, reason)
        _ -> :other
    print(x)
`)
}

func TestRecvElse(t *testing.T) {
	runModule(t, `module Main
fn worker() ->
    x = recv
        :a -> :got_a
    else msg
        :unknown
    print(x)

fn main() ->
    pid = spawn(worker)
    send(pid, :b)
    recv
        :never -> :never
    after 50 -> :ok
`)
}

func TestRecvAfter(t *testing.T) {
	runModule(t, `module Main
fn worker() ->
    x = recv
        :a -> :got_a
    after 10 -> :timeout
    print(x)

fn main() ->
    pid = spawn(worker)
    recv
        :never -> :never
    after 100 -> :ok
`)
}

// ---- TCO (v0.4.8) ----

// TestTailRecursion — без TCO этот тест заполнит стек кадров актора
// миллионом записей (~2.5 GB locals) и упадёт по OOM. С TCO — меньше
// секунды.
func TestTailRecursion(t *testing.T) {
	runModule(t, `module Main
fn sum_to(n, acc) ->
    if n == 0 then acc else sum_to(n - 1, acc + n)

fn main() ->
    assert(sum_to(1000000, 0) == 500000500000)
`)
}

// TestNonTailRecursionStillWorks — не-хвостовая рекурсия не должна
// ломаться: n * fact(n-1) требует умножения после возврата.
func TestNonTailRecursionStillWorks(t *testing.T) {
	runModule(t, `module Main
fn fact(n) ->
    if n <= 1 then 1 else n * fact(n - 1)

fn main() ->
    assert(fact(10) == 3628800)
`)
}

// TestTailRecursionThroughRecv — хвостовой вызов внутри ветки recv
// (паттерн CALL ; JMP end ; end: RETURN). Актор обрабатывает много
// сообщений, стек кадров не растёт.
func TestTailRecursionThroughRecv(t *testing.T) {
	runModule(t, `module Main
fn counter_loop(n) ->
    recv
        (:inc) -> counter_loop(n + 1)
        (:get, from) ->
            send(from, n)
            counter_loop(n)
        (:stop) -> :ok

fn main() ->
    pid = spawn(() -> counter_loop(0))
    send(pid, :inc)
    send(pid, :inc)
    send(pid, :inc)
    send(pid, (:get, self()))
    n = recv
        v -> v
    assert(n == 3)
    send(pid, :stop)
`)
}

// ---- v0.4.9: prelude / patterns / ensure --------

func TestPreludeFilter(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    xs = [1, 2, 3, 4, 5]
    ys = filter(x -> x rem 2 == 0, xs)
    print(ys)
`)
}

func TestPreludeFind(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    xs = [1, 2, 3]
    print(find(x -> x == 2, xs))
    print(find(x -> x == 99, xs))
`)
}

func TestPreludeAllAny(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    print(all(x -> x > 0, [1, 2, 3]))
    print(all(x -> x > 0, [1, -2, 3]))
    print(any(x -> x > 2, [1, 2, 3]))
    print(any(x -> x > 9, [1, 2, 3]))
`)
}

func TestPreludeToIntToFloat(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    print(to_int("42"))
    print(to_float("3.14"))
    print(to_int(7))
`)
}

func TestPreludeLog(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    log("hi")
`)
}

func TestSmallIntFastPath(t *testing.T) {
	// Без small-int этот тест медленный; с ним — быстрый.
	runModule(t, `module Main
fn loop(n, acc) ->
    if n == 0 then acc else loop(n - 1, acc + n)
fn main() ->
    print(loop(100000, 0))
`)
}

func TestRecvListPattern(t *testing.T) {
	runModule(t, `module Main
fn worker() ->
    x = recv
        [1, ..rest] -> rest
        _ -> :no_match
    print(x)

fn main() ->
    pid = spawn(worker)
    send(pid, [1, 2, 3])
    recv
        :never -> :never
    after 50 -> :ok
`)
}

func TestRecvMapPattern(t *testing.T) {
	runModule(t, `module Main
fn worker() ->
    x = recv
        %{ "a" => v } -> v
        _ -> :no_match
    print(x)

fn main() ->
    pid = spawn(worker)
    send(pid, %{ "a" => 42 })
    recv
        :never -> :never
    after 50 -> :ok
`)
}

func TestEnsureAllRunOnFailure(t *testing.T) {
	// Полная семантика §10.3: если первый (LIFO) ensure падает,
	// остальные ВСЁ РАВНО выполняются.
	runModule(t, `module Main
fn main() ->
    result = trap
        print("body")
        ensure print("cleanup-1")
        ensure raise(:cleanup_failed)
        :ok
    print(result)
`)
}
