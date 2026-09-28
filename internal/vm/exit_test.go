package vm_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/parser"
	"github.com/it1ro/brig-lang/internal/vm"
)

// exitPrelude — хелперы тестов exit (§12.7). take — следующее сообщение
// (:down идёт впереди ящика) или :timeout; idle — актор, ждущий в recv;
// spin — актор, который не блокируется.
const exitPrelude = `module Main
fn take() ->
    recv
        m -> m
    after 1000 -> :timeout

fn empty() ->
    recv
        m -> m
    after 20 -> :empty

fn idle() ->
    recv
        _ -> idle()

fn spin(n) -> spin(n + 1)
`

// TestExitRunsEnsure — ensure жертвы выполняются LIFO, от внутреннего
// кадра к внешнему; незарегистрированный ensure не выполняется; :down
// несёт причину exit (§12.7).
func TestExitRunsEnsure(t *testing.T) {
	runModuleSync(t, exitPrelude+`
fn inner(parent) ->
    r = trap
        ensure send(parent, :inner_1)
        ensure send(parent, :inner_2)
        send(parent, :ready)
        idle()
        ensure send(parent, :not_registered)
        :after
    send(parent, (:inner_continued, r))

fn outer(parent) ->
    r = trap
        ensure send(parent, :outer)
        inner(parent)
    send(parent, (:outer_continued, r))

fn main() ->
    parent = self()
    (pid, ref) = spawn_watched(() -> outer(parent))
    assert(take() == :ready)
    assert(exit(pid, :shutdown) == Ok(()))
    down = take()
    assert(down == (:down, ref, :shutdown))
    assert(take() == :inner_2)
    assert(take() == :inner_1)
    assert(take() == :outer)
    assert(empty() == :empty)
`)
}

// TestExitRunsEnsurePreempted — жертва, которая не блокируется,
// завершается на ближайшей редукции, ensure выполняются.
func TestExitRunsEnsurePreempted(t *testing.T) {
	runModuleSync(t, exitPrelude+`
fn busy(parent) ->
    trap
        ensure send(parent, :cleaned)
        send(parent, :ready)
        spin(0)

fn main() ->
    parent = self()
    (pid, ref) = spawn_watched(() -> busy(parent))
    assert(take() == :ready)
    exit(pid, :stop)
    assert(take() == (:down, ref, :stop))
    assert(take() == :cleaned)
`)
}

// TestExitKillSkipsEnsure — exit(pid, :kill) завершает без ensure;
// :kill поверх начатого unwind прерывает оставшиеся ensure, причина
// остаётся первой (§12.7).
func TestExitKillSkipsEnsure(t *testing.T) {
	runModuleSync(t, exitPrelude+`
fn guarded(parent) ->
    trap
        ensure send(parent, :cleaned)
        send(parent, :ready)
        idle()

fn stuck_cleanup(parent) ->
    trap
        ensure send(parent, :second)
        ensure stuck(parent)
        send(parent, :ready)
        idle()

fn stuck(parent) ->
    send(parent, :stuck)
    idle()

fn main() ->
    parent = self()
    (p1, r1) = spawn_watched(() -> guarded(parent))
    assert(take() == :ready)
    exit(p1, :kill)
    assert(take() == (:down, r1, :kill))
    assert(empty() == :empty)

    (p2, r2) = spawn_watched(() -> stuck_cleanup(parent))
    assert(take() == :ready)
    exit(p2, :shutdown)
    assert(take() == :stuck)
    exit(p2, :other)
    assert(empty() == :empty)
    exit(p2, :kill)
    assert(take() == (:down, r2, :shutdown))
    assert(empty() == :empty)
`)
}

// TestExitNotTrappable — trap жертвы exit не ловит: тело не
// продолжается ни в одном кадре trap; exit(self(), r) — то же синхронно,
// ensure выполняются. Ошибка в ensure причину не меняет.
func TestExitNotTrappable(t *testing.T) {
	runModuleSync(t, exitPrelude+`
fn victim(parent) ->
    r = trap
        send(parent, :ready)
        idle()
    send(parent, (:trapped, r))

fn suicide(parent) ->
    r = trap
        ensure send(parent, :cleaned)
        exit(self(), :bye)
        send(parent, :continued)
    send(parent, (:trapped, r))

fn bad_ensure(parent) ->
    trap
        ensure send(parent, :after_bad)
        ensure raise(:in_ensure)
        exit(self(), :original)

fn main() ->
    parent = self()
    (p1, r1) = spawn_watched(() -> victim(parent))
    assert(take() == :ready)
    exit(p1, :stop)
    assert(take() == (:down, r1, :stop))
    assert(empty() == :empty)

    (_p2, r2) = spawn_watched(() -> suicide(parent))
    assert(take() == (:down, r2, :bye))
    assert(take() == :cleaned)
    assert(empty() == :empty)

    (_p3, r3) = spawn_watched(() -> bad_ensure(parent))
    assert(take() == (:down, r3, :original))
    assert(take() == :after_bad)
    assert(empty() == :empty)
`)
}

// TestExitDownReason — наблюдатели получают причину как есть (любое
// значение); exit мёртвому и несуществующему pid — Ok(()); сигнал не
// проходит через ящик; повторный exit не меняет причину.
func TestExitDownReason(t *testing.T) {
	runModuleSync(t, exitPrelude+`
fn quick() -> :done

fn main() ->
    (p1, r1) = spawn_watched(() -> idle())
    reason = (:custom, [1, 2], "why")
    exit(p1, reason)
    exit(p1, :second)
    assert(take() == (:down, r1, reason))
    assert(exit(p1, :again) == Ok(()))

    (p2, r2) = spawn_watched(quick)
    assert(take() == (:down, r2, :normal))
    assert(exit(p2, :late) == Ok(()))

    (p3, r3) = spawn_watched(() -> idle())
    send(p3, :msg)
    exit(p3, :normal)
    assert(take() == (:down, r3, :normal))

    p4 = spawn(() -> idle())
    w4 = watch(p4)
    assert(exit(p4, :kill) == Ok(()))
    assert(take() == (:down, w4, :kill))
`)
}

// TestExitSelfInMain — exit(self(), r) в main завершает программу
// ошибкой ErrExit с причиной r, после ensure; trap её не ловит.
func TestExitSelfInMain(t *testing.T) {
	prog, err := parser.ParseProgram(parser.ModeModule, `module Main
fn main() ->
    r = trap
        ensure print("cleanup")
        exit(self(), (:stop, 1))
    print(r)
`)
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
	_, err = m.RunMain(m.Global("main"))
	var ee *vm.ErrExit
	if !errors.As(err, &ee) {
		t.Fatalf("err = %v, want *vm.ErrExit", err)
	}
	if got := ee.Reason.Inspect(); got != "(:stop, 1)" {
		t.Fatalf("reason = %s, want (:stop, 1)", got)
	}
	if !strings.HasPrefix(err.Error(), "exit: ") {
		t.Fatalf("message = %q", err.Error())
	}
}

// TestSpawnWatchedAtomic — spawn_watched возвращает (pid, ref); ребёнок,
// упавший на первой редукции, всё равно даёт :down с этим ref (§12.7).
func TestSpawnWatchedAtomic(t *testing.T) {
	runModuleSync(t, exitPrelude+`
fn boom() -> raise(:early)

fn quick() -> :done

fn main() ->
    (p1, r1) = spawn_watched(boom)
    assert(take() == (:down, r1, (:raise, :early)))
    (p2, r2) = spawn_watched(quick)
    assert(take() == (:down, r2, :normal))
    assert(p1 != p2)
    assert(r1 != r2)
    (p3, r3) = spawn_watched(() -> idle())
    unwatch(r3)
    exit(p3, :stop)
    assert(empty() == :empty)
`)
}
