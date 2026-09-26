package compiler_test

import (
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/parser"
)

func compileSrc(t *testing.T, src string) error {
	t.Helper()
	prog, err := parser.ParseProgram(parser.ModeModule, src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, err = compiler.New().Compile(prog)
	return err
}

// S-F2 / T-51: мультиклозы, guard и параметры-паттерны дают результат
// спеки, а не молча clauses[0].
func TestAuditMultiClauseNotSilentlyDropped(t *testing.T) {
	runModule(t, `module Main
fn fact(0) -> 1
fn fact(n) -> n * fact(n - 1)
fn main() ->
    assert(fact(5) == 120)
`)
	runModule(t, `module Main
fn classify(n) when n > 0 -> "positive"
fn classify(0)             -> "zero"
fn classify(n)             -> "negative"
fn main() ->
    assert(classify(-5) == "negative")
    assert(classify(0) == "zero")
    assert(classify(3) == "positive")
`)
	runModule(t, `module Main
fn pow(base, exp) ->
    fn go(acc, 0) -> acc
    fn go(acc, n) -> go(acc * base, n - 1)
    go(1, exp)
fn main() ->
    assert(pow(2, 10) == 1024)
`)
	runModule(t, `module Main
fn yes(true) -> 1
fn yes(false) -> 0
fn one(_) -> 1
fn first((a, b)) -> a
fn main() ->
    fn is_zero(0) -> true
    fn is_zero(_) -> false
    assert(yes(true) == 1)
    assert(yes(false) == 0)
    assert(one(5) == 1)
    assert(first((1, 2)) == 1)
    assert(is_zero(0) == true)
    assert(is_zero(5) == false)
`)
}

// S-F2 / T-51: ни один клоз не подошёл — (:function_clause, args), §6.1.
func TestFunctionClauseRaise(t *testing.T) {
	runModule(t, `module Main
fn fact(0) -> 1
fn fact(n) when n > 0 -> n * fact(n - 1)
fn is_zero(0) -> true
fn both(0, 0) -> :zero
fn main() ->
    fn positive(n) when n > 0 -> "positive"
    missed = trap(fact(-1))
    assert(missed == Error((:function_clause, [-1])))
    notZero = trap(is_zero(5))
    assert(notZero == Error((:function_clause, [5])))
    neg = trap(positive(-5))
    assert(neg == Error((:function_clause, [-5])))
    pair = trap(both(0, 7))
    assert(pair == Error((:function_clause, [0, 7])))
`)
}

// T-51: guard видит имена из паттернов параметров; variadic-мультиклоз.
func TestMultiClausePatternGuardAndVariadic(t *testing.T) {
	runModule(t, `module Main
fn order((a, b)) when a > b -> :desc
fn order((a, b)) -> :asc
fn count(0, ..rest) -> 0
fn count(n, ..rest) -> n + len(rest)
fn main() ->
    assert(order((2, 1)) == :desc)
    assert(order((1, 2)) == :asc)
    assert(count(0, 1, 2) == 0)
    assert(count(5, 1, 2) == 7)
`)
}

// T-51: клозы разной арности — ошибка компиляции без двойного префикса.
func TestMultiClauseArityMismatch(t *testing.T) {
	err := compileSrc(t, `module Main
fn f(0) -> 1
fn f(a, b) -> 2
fn main() ->
    print(f(0))
`)
	if err == nil {
		t.Fatal("Compile: want arity mismatch error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "арност") {
		t.Fatalf("Compile: want error about арность, got %q", msg)
	}
	if strings.Count(msg, "fn f:") != 1 {
		t.Fatalf("Compile: want single `fn f:` prefix, got %q", msg)
	}
}

// T-51: захваты локальных fn, включая взаимную рекурсию в обоих порядках
// объявления и транзитивный захват через другую локальную fn.
func TestLocalFnRecursiveCapture(t *testing.T) {
	runModule(t, `module Main
fn parity_a(k, n) ->
    fn ev(0) -> true
    fn ev(m) -> od(m - 1)
    fn od(0) -> false
    fn od(m) -> k and ev(m - 1)
    ev(n)
fn parity_b(k, n) ->
    fn od(0) -> false
    fn od(m) -> k and ev(m - 1)
    fn ev(0) -> true
    fn ev(m) -> od(m - 1)
    ev(n)
fn outer(base) ->
    fn add(n) -> base + n
    fn twice(n) -> add(add(n))
    twice(1)
fn main() ->
    assert(parity_a(true, 4) == true)
    assert(parity_a(true, 3) == false)
    assert(parity_b(true, 4) == true)
    assert(parity_b(true, 3) == false)
    assert(outer(10) == 21)
`)
}

// T-51: захват ищется по разрешённому имени, а не по строке: параметр
// лямбды `go` затеняет локальную fn `go` с захватом.
func TestLocalFnCaptureShadowedByParam(t *testing.T) {
	runModule(t, `module Main
fn f(k) ->
    fn go(0) -> k
    fn go(n) -> go(n - 1)
    h = fn (go) -> go(1)
    h(x -> x * 10) + go(3)
fn main() ->
    assert(f(7) == 17)
`)
}

// A-F6 (T-56): локаль промежуточной лямбды затеняет локальную fn
// внешней fn и для вложенной лямбды — как значение и как callee.
func TestLocalFnShadowedAtIntermediateLevel(t *testing.T) {
	runModule(t, `module Main
fn f() ->
    fn g() -> 1
    h = fn () ->
        g = 5
        k = fn () -> g
        k()
    h()
fn call() ->
    fn g(x) -> x
    h = fn () ->
        g = x -> x * 10
        k = fn () -> g(2)
        k()
    h()
fn main() ->
    assert(f() == 5)
    assert(call() == 20)
`)
}

// T-49: параметр промежуточной лямбды затеняет локальную fn внешней fn
// для вложенной лямбды — ближайшее связывание побеждает на каждом
// уровне (doc 02 §7). Вариант с захватом: к затенённому имени не
// дописываются захваты лифтнутой fn (T-51).
func TestLocalFnShadowedByEnclosingLambdaParam(t *testing.T) {
	runModule(t, `module Main
fn f() ->
    fn go(n) -> n
    g = fn (go) -> (x -> go(x))
    h = g(y -> y * 10)
    h(2)
fn captured(k) ->
    fn go(n) -> n + k
    g = fn (go) -> (x -> go(x))
    h = g(y -> y * 10)
    h(2) + go(1)
fn main() ->
    assert(f() == 20)
    assert(captured(100) == 121)
`)
}

// S-F3 (T-52): ложный guard ветки recv переводит к следующей ветке
// (§12.4). Guard видит связывания паттерна и внешние локали; ветка с
// guard в хвостовой позиции остаётся TAILCALL.
func TestRecvGuardSelectsBranch(t *testing.T) {
	runModule(t, `module Main
fn classify(x) ->
    send(self(), x)
    recv
        n when n > 10 -> :big
        n -> :small
fn worker_loop(pending, acc) ->
    recv
        (:reply, id, v) when id == pending ->
            worker_loop(pending, acc + v)
        (:reply, _, _) ->
            worker_loop(pending, acc + 1000)
        (:stop) -> acc
fn to_else(x) ->
    send(self(), x)
    recv
        n when n == 1 -> :one
    else msg
        (:else, msg)
fn one_line(x) ->
    send(self(), x)
    recv n when n == 1 -> :one
fn main() ->
    assert(classify(5) == :small)
    assert(classify(50) == :big)
    send(self(), (:reply, 7, 1))
    send(self(), (:reply, 8, 1))
    send(self(), (:reply, 7, 2))
    send(self(), (:stop))
    assert(worker_loop(7, 0) == 1003)
    assert(to_else(1) == :one)
    assert(to_else(2) == (:else, 2))
    assert(one_line(1) == :one)
`)
}

// T-52: guard, ложный во всех ветках, без else — (:recv_clause, msg).
func TestRecvGuardNoBranchRaises(t *testing.T) {
	err := runModuleErr(t, `module Main
fn main() ->
    send(self(), 5)
    recv
        n when n > 10 -> :big
`)
	if err == nil || !strings.Contains(err.Error(), "recv_clause") {
		t.Fatalf("want :recv_clause raise, got %v", err)
	}
}

func TestAuditSimpleParamsStillCompile(t *testing.T) {
	src := `module Main
fn head(a, ..rest) -> a
fn ok?(ready?) -> ready?
fn main() ->
    fn add(x, y) -> x + y
    print(add(head(1, 2, 3), 2), ok?(true))
`
	if err := compileSrc(t, src); err != nil {
		t.Fatalf("Compile: %v", err)
	}
}

// R0 / I-F2: ни одна форма trap (inline / block / ensure) не должна давать
// TAILCALL, даже если trap стоит в хвостовой позиции функции.
func TestAuditNoTailCallInsideTrap(t *testing.T) {
	img := compileModule(t, `module Main
fn g() -> 1
fn f_inline() -> trap(g())
fn f_block() ->
    trap
        g()
fn f_ensure() ->
    trap
        ensure g()
        g()
fn main() -> f_inline()
`)
	for _, name := range []string{"f_inline", "f_block", "f_ensure"} {
		if dis := img.Functions[name].Disassemble(); strings.Contains(dis, "TAILCALL") {
			t.Errorf("%s: TAILCALL inside trap region:\n%s", name, dis)
		}
	}
}

// §4 табл. п.6 (doc 02): правый операнд and/or — хвостовая позиция (T-82).
func TestAuditAndOrRightOperandIsTail(t *testing.T) {
	t.Skip("blocked: T-82")
	img := compileModule(t, `module Main
fn loop(n) -> n == 0 or loop(n - 1)
fn main() -> loop(3)
`)
	if dis := img.Functions["loop"].Disassemble(); !strings.Contains(dis, "TAILCALL") {
		t.Errorf("right operand of `or` is not TAILCALL:\n%s", dis)
	}
}

// §6.5: канонический пример локальной fn с захватом `base` (T-39).
func TestAuditLocalFnCapturesEnclosingParam(t *testing.T) {
	if err := runModuleErr(t, `module Main
fn outer(base) ->
    fn helper(n) -> base + n
    helper(1)
fn main() -> assert(outer(41) == 42)
`); err != nil {
		t.Errorf("local fn capture: %v", err)
	}
}

// I-F7: локальная fn с захватом как значение — замыкание над захватами
// (T-39): присваивание, передача в функцию высшего порядка, spawn,
// рекурсивная fn как значение, значение из вложенной лямбды.
func TestLocalFnCaptureAsValue(t *testing.T) {
	runModule(t, `module Main
fn value(base) ->
    fn helper(n) -> base + n
    h = helper
    h(1)
fn higher(k, xs) ->
    fn scale(x) -> x * k
    map(scale, xs)
fn rec_value(base) ->
    fn go(0) -> base
    fn go(n) -> go(n - 1)
    g = go
    g(3)
fn from_lambda(base) ->
    fn helper(n) -> base + n
    mk = fn () -> helper
    f = mk()
    f(2)
fn run(parent, base) ->
    fn helper() -> send(parent, base + 1)
    spawn(helper)
fn main() ->
    assert(value(41) == 42)
    assert(higher(3, [1, 2]) == [3, 6])
    assert(rec_value(7) == 7)
    assert(from_lambda(40) == 42)
    run(self(), 41)
    recv
        n -> assert(n == 42)
`)
}

// I-F7: захват передаётся по связыванию, видимому в точке объявления
// локальной fn, а не по имени в точке вызова (T-39).
func TestLocalFnCaptureResolvedByBinding(t *testing.T) {
	runModule(t, `module Main
fn lambda_param(base) ->
    fn helper(n) -> base + n
    f = fn (base) -> helper(base)
    f(1)
fn sibling_param(x) ->
    fn g(n) -> x + n
    fn f(x) -> g(x)
    f(1)
fn nested(x) ->
    fn g(n) -> x + n
    fn f(x) ->
        fn h(n) -> x * g(n)
        h(2)
    f(3)
fn rec_shadow(base) ->
    fn go(0) -> base
    fn go(n) ->
        base = 100
        go(n - 1)
    go(2)
fn main() ->
    assert(lambda_param(41) == 42)
    assert(sibling_param(10) == 11)
    assert(nested(10) == 36)
    assert(rec_shadow(1) == 1)
`)
}

// §6.5: пример из спеки — мультиклозная локальная fn с захватом `base`.
func TestLocalFnSpec65Pow(t *testing.T) {
	runModule(t, `module Main
fn pow(base, exp) ->
    fn go(acc, 0) -> acc
    fn go(acc, n) -> go(acc * base, n - 1)
    go(1, exp)
fn main() ->
    assert(pow(2, 10) == 1024)
`)
}

// §3.1: Int — произвольной точности; литерал за пределами int64 — Int (T-22).
func TestAuditBigIntLiteral(t *testing.T) {
	if err := runModuleErr(t, `module Main
fn main() -> assert(99999999999999999999 - 99999999999999999998 == 1)
`); err != nil {
		t.Errorf("big int literal: %v", err)
	}
}

// brig.ebnf int_lit ::= dec_digits: `010` — десятичное 10, не восьмеричное (T-22).
func TestAuditLeadingZeroIsDecimal(t *testing.T) {
	if err := runModuleErr(t, `module Main
fn main() -> assert(010 == 10)
`); err != nil {
		t.Errorf("leading-zero literal: %v", err)
	}
}

// S-F7 / T-22: base по префиксу; иначе 10; произвольная точность Int.
func TestIntLiteralBases(t *testing.T) {
	if err := runModuleErr(t, `module Main
fn main() ->
    assert(010 == 10)
    assert(08 == 8)
    assert(0x10 == 16)
    assert(0b1010 == 10)
    assert(0o10 == 8)
    assert(1_000 == 1000)
    assert(to_str(99999999999999999999) == "99999999999999999999")
    assert(-9223372036854775808 + 9223372036854775807 == -1)
    assert(to_str(-9223372036854775808) == "-9223372036854775808")
`); err != nil {
		t.Errorf("int literal bases: %v", err)
	}
}

// §12 / doc 02 §6: наблюдатель получает значение raise в причине :down (T-40).
func TestAuditDownReasonCarriesRaiseValue(t *testing.T) {
	if err := runModuleErr(t, `module Main
fn boom() -> raise(:boom)
fn main() ->
    p = spawn(boom)
    r = watch(p)
    reason = recv
        (:down, _, why) -> why
    assert(reason == (:raise, :boom))
`); err != nil {
		t.Errorf("down reason: %v", err)
	}
}

// trap внутри колбэка прелюдии должен ловить raise из вложенного кадра (T-34).
// Полная лямбда с trap как let RHS (§10.2); inline short-lambda с trap
// отвергается sema, а блочная лямбда в аргументе map не парсится.
func TestAuditTrapInsideNativeCallback(t *testing.T) {
	if err := runModuleErr(t, `module Main
fn g(x) -> if x == 2 then raise(:bad) else x
fn main() ->
    cb = fn (x) ->
        r = trap(g(x))
        r
    ys = map(cb, [1, 2])
    assert(ys == [Ok(1), Error(:bad)])
`); err != nil {
		t.Errorf("trap across callSync: %v", err)
	}
}

// §10.3: ensure видит локали тела, объявленные до него (T-37).
func TestAuditEnsureSeesBodyLocals(t *testing.T) {
	if err := runModuleErr(t, `module Main
fn main() ->
    r = trap
        f1 = 10
        ensure f1
        f1 + 1
    assert(r == Ok(11))
`); err != nil {
		t.Errorf("ensure scope: %v", err)
	}
}

// T-44: полная лямбда `fn (…) ->` с параметром-паттерном или `..name`
// обязана дать ошибку компиляции, а не связать паттерн как имя.
func TestAuditLambdaPatternParamsFailFast(t *testing.T) {
	err := compileSrc(t, `module Main
fn main() ->
    f = fn (0) -> "zero"
    print(f(5))
    g = fn (a, ..rest) -> rest
    print(g(1, 2, 3))
`)
	if err == nil {
		t.Fatal("Compile: want error for pattern param, got nil")
	}
	if !strings.Contains(err.Error(), "срез:") {
		t.Fatalf("want срез: error, got %v", err)
	}

	err = compileSrc(t, `module Main
fn main() ->
    g = fn (a, ..rest) -> rest
    print(g(1, 2, 3))
`)
	if err == nil {
		t.Fatal("Compile: want error for variadic lambda param, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "срез:") || !strings.Contains(msg, "..rest") {
		t.Fatalf("want variadic срез error mentioning ..rest, got %v", err)
	}
}

// S-F1 / T-54: интерполяция компилируется в concat через to_str.
func TestInterpolationConcat(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    x = 5
    assert("x = \(x)" == "x = 5")
    assert("sum: \(1 + 2)" == "sum: 3")
    name = "Ada"
    assert("Hi, \(name)!" == "Hi, Ada!")
    assert("a \(10) b \(true) c" == "a 10 b true c")
    assert("\\(not interp)" == "\\(not interp)")
`)
}

// §4.8: pattern matching чисел — точный (1.0 не матчит паттерн 1) (T-84).
func TestAuditLiteralPatternIsExact(t *testing.T) {
	t.Skip("blocked: T-84")
	if err := runModuleErr(t, `module Main
fn main() ->
    send(self(), 1.0)
    r = recv
        1 -> :int
        _ -> :other
    assert(r == :other)
`); err != nil {
		t.Errorf("exact literal pattern: %v", err)
	}
}
