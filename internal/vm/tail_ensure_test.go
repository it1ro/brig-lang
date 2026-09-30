package vm_test

import (
	"testing"

	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/parser"
	"github.com/it1ro/brig-lang/internal/vm"
)

// TCO сквозь ensure (doc 02 §5.1, T-173).

// runTailEnsure — runModuleSync с нативом frame_depth().
func runTailEnsure(t *testing.T, src string) {
	t.Helper()
	prog, err := parser.ParseProgram(parser.ModeModule, src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	img, err := compiler.New().Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	m := vm.New()
	for name, fn := range img.Functions {
		m.DefineGlobal(name, vm.FuncValue(fn))
	}
	m.DefineGlobal("frame_depth", vm.FrameDepthNative())
	if _, err := m.RunMain(m.Global("main")); err != nil {
		t.Fatalf("run: %v", err)
	}
}

// logPrelude — журнал в Global: log(x) дописывает x, logged() — журнал
// в порядке записи, reset() очищает. unwrap(r, k) снимает Ok-обёртки.
const logPrelude = `module Main
fn log(x) ->
    match Global.get(:log)
        Some(xs) -> Global.put(:log, [x, ..xs])
        None -> Global.put(:log, [x])

fn rev(xs, acc) ->
    match xs
        [] -> acc
        [h, ..t] -> rev(t, [h, ..acc])

fn logged() ->
    match Global.get(:log)
        Some(xs) -> rev(xs, [])
        None -> []

fn reset() -> Global.put(:log, [])

fn count() ->
    match Global.get(:ticks)
        Some(n) -> n
        None -> 0

fn tick() -> Global.put(:ticks, count() + 1)

fn unwrap(r, k) ->
    match r
        Ok(v) -> unwrap(v, k + 1)
        _ -> (r, k)
`

// TestTailCallThroughEnsureDeepRecursion — рекурсия 10⁶ внутри trap с
// ensure: a.frames не растёт, каждый ensure выполнен один раз, результат —
// Ok^(n+1)(v) (инварианты 1–2 §5.1).
func TestTailCallThroughEnsureDeepRecursion(t *testing.T) {
	runTailEnsure(t, logPrelude+`
fn loop(n) ->
    trap
        ensure tick()
        if n == 0 then frame_depth() else loop(n - 1)

fn main() ->
    (d0, k0) = unwrap(loop(0), 0)
    assert(k0 == 1)
    assert(count() == 1)
    (d, k) = unwrap(loop(1000000), 0)
    assert(k == 1000001)
    assert(d == d0)
    assert(count() == 1000002)
`)
}

// TestEnsureOrderWithTailCall — ensure каждого уровня ровно один раз, LIFO
// внутри уровня и от внутреннего уровня к внешнему; захват по значению в
// точке регистрации, даже если хвост в ветке затеняет имя (§5.1).
func TestEnsureOrderWithTailCall(t *testing.T) {
	runTailEnsure(t, logPrelude+`
fn loop(n) ->
    trap
        ensure log((:a, n))
        x = n * 10
        ensure log((:b, x))
        if n == 0 then :done else loop(n - 1)

fn shadow(n) ->
    trap
        f = n
        ensure log((:f, f))
        if n == 0
            :done
        else
            f = n + 100
            shadow(f - 101)

fn main() ->
    assert(loop(3) == Ok(Ok(Ok(Ok(:done)))))
    assert(logged() == [(:b, 0), (:a, 0), (:b, 10), (:a, 1), (:b, 20), (:a, 2), (:b, 30), (:a, 3)])
    reset()
    assert(shadow(2) == Ok(Ok(Ok(:done))))
    assert(logged() == [(:f, 0), (:f, 1), (:f, 2)])
`)
}

// TestTailCallEnsureRaiseMatchesNonTail — raise на уровне k даёт
// Ok^(k)(Error(e)) и тот же порядок ensure, что без TCO (r = trap …; r).
// Источники raise: сам кадр, вызванная им функция, ошибка разбора callee
// и арности (байткод и native) на хвостовом вызове.
func TestTailCallEnsureRaiseMatchesNonTail(t *testing.T) {
	runTailEnsure(t, logPrelude+`
fn boom(n) -> raise((:boom, n))
fn deeper(n) ->
    x = boom(n)
    x
fn two(a, b) -> a

fn at(n, k, how) ->
    if n < k
        (:go, n + 1)
    else
        match how
            :raise -> raise((:here, n))
            :callee -> (:call, boom)
            :deeper -> (:call, deeper)
            :not_fn -> (:call, 42)
            :arity -> (:call, two)
            :native -> (:call, Global.put)

fn tco(n, k, how) ->
    trap
        ensure log(n)
        match at(n, k, how)
            (:go, m) -> tco(m, k, how)
            (:call, f) -> f(n)

fn plain(n, k, how) ->
    r = trap
        ensure log(n)
        match at(n, k, how)
            (:go, m) -> plain(m, k, how)
            (:call, f) -> f(n)
    r

fn check(how) ->
    reset()
    a = tco(0, 3, how)
    la = logged()
    reset()
    b = plain(0, 3, how)
    lb = logged()
    assert(a == b)
    assert(la == lb)
    assert(la == [3, 2, 1, 0])
    a

fn main() ->
    assert(check(:raise) == Ok(Ok(Ok(Error((:here, 3))))))
    assert(check(:callee) == Ok(Ok(Ok(Error((:boom, 3))))))
    assert(check(:deeper) == Ok(Ok(Ok(Error((:boom, 3))))))
    check(:not_fn)
    check(:arity)
    check(:native)
`)
}

// TestTailCallEnsureFailureWins — ошибка ensure на уровне k: побеждает
// последняя ошибка уровня, остальные ensure всех уровней выполняются.
func TestTailCallEnsureFailureWins(t *testing.T) {
	runTailEnsure(t, logPrelude+`
fn e(tag, n) ->
    log((tag, n))
    if n == 1 then raise((tag, n)) else ()

fn tco(n) ->
    trap
        ensure e(:e1, n)
        ensure e(:e2, n)
        if n == 0 then :done else tco(n - 1)

fn plain(n) ->
    r = trap
        ensure e(:e1, n)
        ensure e(:e2, n)
        if n == 0 then :done else plain(n - 1)
    r

fn main() ->
    a = tco(2)
    la = logged()
    reset()
    b = plain(2)
    assert(a == b)
    assert(la == logged())
    assert(a == Ok(Error((:e1, 1))))
    assert(la == [(:e2, 0), (:e1, 0), (:e2, 1), (:e1, 1), (:e2, 2), (:e1, 2)])
`)
}

// TestTailCallEnsureExitAndKill — exit посреди рекурсии выполняет все
// ensure LIFO (ошибка ensure причину не меняет); exit во время drain
// обычного возврата переводит его в режим exit; :kill ensure пропускает
// и прерывает начатый drain (§12.7, §5.1).
func TestTailCallEnsureExitAndKill(t *testing.T) {
	runModuleSync(t, exitPrelude+`
fn ens(parent, n) ->
    send(parent, (:ens, n))
    if n == 2 then raise(:ens_failed) else ()

fn deep(parent, n) ->
    trap
        ensure ens(parent, n)
        if n == 0
            send(parent, :ready)
            idle()
        else
            deep(parent, n - 1)

fn stuck_at(parent, n) ->
    send(parent, (:ens, n))
    if n == 2
        send(parent, :stuck)
        idle()
    else
        ()

fn draining(parent, n) ->
    trap
        ensure stuck_at(parent, n)
        if n == 0 then :done else draining(parent, n - 1)

fn main() ->
    parent = self()

    (p1, r1) = spawn_watched(() -> deep(parent, 4))
    assert(take() == :ready)
    exit(p1, :stop)
    assert(take() == (:down, r1, :stop))
    assert(take() == (:ens, 0))
    assert(take() == (:ens, 1))
    assert(take() == (:ens, 2))
    assert(take() == (:ens, 3))
    assert(take() == (:ens, 4))
    assert(empty() == :empty)

    (p2, r2) = spawn_watched(() -> deep(parent, 4))
    assert(take() == :ready)
    exit(p2, :kill)
    assert(take() == (:down, r2, :kill))
    assert(empty() == :empty)

    (p3, r3) = spawn_watched(() -> draining(parent, 4))
    assert(take() == (:ens, 0))
    assert(take() == (:ens, 1))
    assert(take() == (:ens, 2))
    assert(take() == :stuck)
    exit(p3, :stop)
    assert(take() == (:down, r3, :stop))
    assert(take() == (:ens, 3))
    assert(take() == (:ens, 4))
    assert(empty() == :empty)

    (p4, r4) = spawn_watched(() -> draining(parent, 4))
    assert(take() == (:ens, 0))
    assert(take() == (:ens, 1))
    assert(take() == (:ens, 2))
    assert(take() == :stuck)
    exit(p4, :kill)
    assert(take() == (:down, r4, :kill))
    assert(empty() == :empty)
`)
}
