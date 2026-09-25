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

// S-F2: мультиклозы, guard и параметры-паттерны fn не компилируются
// (T-50, T-51) — Compile обязан вернуть ошибку, а не взять clauses[0].
func TestAuditMultiClauseNotSilentlyDropped(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"t10_multiclause", `module Main
fn fact(0) -> 1
fn fact(n) -> n * fact(n - 1)
fn main() ->
    print(fact(5))
`, "мультиклозные fn"},
		{"u2_guard_fn", `module Main
fn classify(n) when n > 0 -> "positive"
fn classify(0)             -> "zero"
fn classify(n)             -> "negative"
fn main() ->
    print(classify(-5))
`, "мультиклозные fn"},
		{"u1_spec65", `module Main
fn pow(base, exp) ->
    fn go(acc, 0) -> acc
    fn go(acc, n) -> go(acc * base, n - 1)
    go(1, exp)
fn main() ->
    print(pow(2, 10))
`, "мультиклозные fn"},
		{"guard_single_clause", `module Main
fn positive(n) when n > 0 -> "positive"
fn main() ->
    print(positive(-5))
`, "guard"},
		{"local_guard_single_clause", `module Main
fn main() ->
    fn positive(n) when n > 0 -> "positive"
    print(positive(-5))
`, "guard"},
		{"literal_param", `module Main
fn is_zero(0) -> true
fn main() ->
    print(is_zero(5))
`, "параметр-паттерн"},
		{"bool_literal_param", `module Main
fn yes(true) -> 1
fn main() ->
    print(yes(false))
`, "параметр-паттерн"},
		{"wildcard_param", `module Main
fn one(_) -> 1
fn main() ->
    print(one(5))
`, "параметр-паттерн"},
		{"tuple_param", `module Main
fn first((a, b)) -> a
fn main() ->
    print(first((1, 2)))
`, "параметр-паттерн"},
		{"local_literal_param", `module Main
fn main() ->
    fn is_zero(0) -> true
    print(is_zero(5))
`, "параметр-паттерн"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := compileSrc(t, tc.src)
			if err == nil {
				t.Fatalf("Compile: want error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Compile: want error containing %q, got %v", tc.want, err)
			}
		})
	}
}

// S-F3: guard в ветках recv не должен молча игнорироваться компилятором.
// Compile обязан вернуть ошибку, а не матчить ветку по паттерну без
// проверки условия (T-02).
func TestAuditRecvGuardRejected(t *testing.T) {
	src := `module Main
fn main() ->
    recv (:msg) when true -> :ok
`
	err := compileSrc(t, src)
	if err == nil {
		t.Fatalf("Compile: want error containing %q, got nil", "guard")
	}
	if !strings.Contains(err.Error(), "guard") {
		t.Fatalf("Compile: want error containing %q, got %v", "guard", err)
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
	t.Skip("blocked: T-39")
	if err := runModuleErr(t, `module Main
fn outer(base) ->
    fn helper(n) -> base + n
    helper(1)
fn main() -> assert(outer(41) == 42)
`); err != nil {
		t.Errorf("local fn capture: %v", err)
	}
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
	t.Skip("blocked: T-40")
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
func TestAuditTrapInsideNativeCallback(t *testing.T) {
	t.Skip("blocked: T-34")
	if err := runModuleErr(t, `module Main
fn g(x) -> if x == 2 then raise(:bad) else x
fn main() ->
    ys = map(fn (x) -> trap(g(x)), [1, 2])
    assert(ys == [Ok(1), Error(:bad)])
`); err != nil {
		t.Errorf("trap across callSync: %v", err)
	}
}

// §10.3: ensure видит локали тела, объявленные до него (T-37).
func TestAuditEnsureSeesBodyLocals(t *testing.T) {
	t.Skip("blocked: T-37")
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
