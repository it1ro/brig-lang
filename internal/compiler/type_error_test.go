package compiler_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/vm"
)

// A-F4 / T-83 (design decision #42, вариант A): каждый :type_error —
// ловимый raise формы (:type_error, (op, val)), как у decArithErr (§10.4).
// Фатальны для актора только внутренние инварианты VM (internal:).
func TestTypeErrorsAreCatchable(t *testing.T) {
	cases := map[string]string{
		"dec_add": `r = trap(dec"1" + "a")
    assert(r == Error((:type_error, (:add, (dec"1", "a")))))`,
		"add": `r = trap(1 + "a")
    assert(r == Error((:type_error, (:add, (1, "a")))))`,
		"sub": `r = trap(1 - "a")
    assert(r == Error((:type_error, (:sub, (1, "a")))))`,
		"mul": `r = trap("a" * 2)
    assert(r == Error((:type_error, (:mul, ("a", 2)))))`,
		"div": `r = trap(1 / "a")
    assert(r == Error((:type_error, (:div, (1, "a")))))`,
		"intdiv": `r = trap(1 div "a")
    assert(r == Error((:type_error, (:div, (1, "a")))))`,
		"rem": `r = trap(1 rem "a")
    assert(r == Error((:type_error, (:rem, (1, "a")))))`,
		"pow": `r = trap(2 ** "a")
    assert(r == Error((:type_error, (:pow, (2, "a")))))`,
		"neg": `s = "a"
    r = trap(-s)
    assert(r == Error((:type_error, (:neg, "a"))))`,
		"not": `r = trap(not 1)
    assert(r == Error((:type_error, (:not, 1))))`,
		"compare_dec_float": `r = trap(dec"1" < 1.0)
    assert(r == Error((:type_error, (:compare, (dec"1", 1.0)))))`,
		"compare_nested": `r = trap([dec"1"] < [1.0])
    assert(r == Error((:type_error, (:compare, ([dec"1"], [1.0])))))`,
		"compare_fn": `f = fn (x) -> x
    r = trap(f < f)
    ok = match r
        Error((:type_error, (:compare, _))) -> true
        _ -> false
    assert(ok)`,
		"call_int": `r = trap(5(1))
    assert(r == Error((:type_error, (:call, 5))))`,
		"call_str": `g = "s"
    r = trap(g(1))
    assert(r == Error((:type_error, (:call, "s"))))`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if err := runModuleErr(t, "module Main\nfn main() ->\n    "+body+"\n"); err != nil {
				t.Fatalf("want caught :type_error, got %v", err)
			}
		})
	}
}

// Не-функция в хвостовой позиции (TAILCALL): raise всплывает в trap
// вызывающего кадра.
func TestTypeErrorTailCallNonFunction(t *testing.T) {
	runModule(t, `module Main
fn f(g) -> g(1)
fn main() ->
    r = trap(f(7))
    assert(r == Error((:type_error, (:call, 7))))
`)
}

// Непойманный :type_error завершает актор как обычный raise (exit 2 в
// CLI), а не как ошибка другого класса.
func TestTypeErrorUncaughtIsRaise(t *testing.T) {
	err := runModuleErr(t, `module Main
fn main() ->
    1 + "a"
`)
	var rerr *vm.ErrRaise
	if !errors.As(err, &rerr) {
		t.Fatalf("want *vm.ErrRaise, got %T %v", err, err)
	}
	if !strings.Contains(rerr.Val.Inspect(), ":type_error") {
		t.Fatalf("want :type_error payload, got %s", rerr.Val.Inspect())
	}
}

// trap в колбэке прелюдии ловит :type_error (#23 / T-34), и непойманный в
// колбэке :type_error долетает до trap снаружи map.
func TestTypeErrorInsidePreludeCallback(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    cb = fn (x) ->
        r = trap(x + "a")
        r
    ys = map(cb, [1])
    assert(ys == [Error((:type_error, (:add, (1, "a"))))])
    bad = fn (x) -> not x
    zs = trap(map(bad, [1]))
    assert(zs == Error((:type_error, (:not, 1))))
`)
}

// Синхронный вызов (vm.Call → callSync): trap внутри вызываемой функции
// ловит :type_error, непойманный возвращается как *vm.ErrRaise.
func TestTypeErrorCallSync(t *testing.T) {
	img := compileModule(t, `module Main
fn caught(x) -> trap(not x)
fn uncaught(x) -> x < uncaught
fn main() -> 0
`)
	m := vm.New()
	for name, fn := range img.Functions {
		m.DefineGlobal(name, vm.FuncValue(fn))
	}
	got, err := m.Call(m.Global("caught"), []runtime.Value{runtime.Int(1)})
	if err != nil {
		t.Fatalf("caught: %v", err)
	}
	if want := "Error((:type_error, (:not, 1)))"; got.Inspect() != want {
		t.Fatalf("caught: got %s, want %s", got.Inspect(), want)
	}
	_, err = m.Call(m.Global("uncaught"), []runtime.Value{runtime.Int(1)})
	var rerr *vm.ErrRaise
	if !errors.As(err, &rerr) || !strings.Contains(rerr.Val.Inspect(), ":compare") {
		t.Fatalf("uncaught: want (:type_error, (:compare, …)) raise, got %v", err)
	}
}
