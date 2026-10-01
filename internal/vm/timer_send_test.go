package vm_test

import (
	"testing"
	"time"

	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/parser"
	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/vm"
)

func runTimerSrc(t *testing.T, src string) runtime.Value {
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
	got, err := m.RunMain(m.Global("main"))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return got
}

// TestTimerSendAfterOrder — T-166: два send_after с одним сроком
// доставляются в порядке взвода; send_after, взведённый раньше
// recv … after с тем же сроком, кладёт сообщение в ящик, и recv берёт
// его, а не ветку after (одна очередь, G4).
func TestTimerSendAfterOrder(t *testing.T) {
	const src = `module Main
fn pair() ->
    recv
        a ->
            recv
                b -> (a, b)

fn mixed() ->
    me = self()
    _ = Timer.send_after(20, me, :timer)
    recv
        :timer -> :timer
    after 20 -> :after

fn main() ->
    me = self()
    _ = Timer.send_after(0, me, :a)
    _ = Timer.send_after(0, me, :b)
    (pair(), mixed())
`
	got := runTimerSrc(t, src)
	if s := got.Inspect(); s != "((:a, :b), :timer)" {
		t.Fatalf("order %s, want ((:a, :b), :timer)", s)
	}
}

// TestTimerCancel — T-166: cancel до срабатывания → true, повторно,
// после срабатывания и на чужой ref → false.
func TestTimerCancel(t *testing.T) {
	const src = `module Main
fn before() ->
    me = self()
    ref = Timer.send_after(500, me, :late)
    c1 = Timer.cancel(ref)
    c2 = Timer.cancel(ref)
    c3 = Timer.cancel(make_ref())
    recv
        :late -> (c1, c2, c3, :fired)
    after 30 -> (c1, c2, c3, :cancelled)

fn after_fire() ->
    me = self()
    ref = Timer.send_after(0, me, :tick)
    recv
        :tick -> Timer.cancel(ref)
    after 200 -> :timeout

fn main() -> (before(), after_fire())
`
	got := runTimerSrc(t, src)
	if s := got.Inspect(); s != "((true, false, false, :cancelled), false)" {
		t.Fatalf("cancel %s, want ((true, false, false, :cancelled), false)", s)
	}
}

// TestTimeMonotonic — T-166: monotonic_ms не убывает; now — мс Unix UTC.
func TestTimeMonotonic(t *testing.T) {
	const src = `module Main
fn main() ->
    a = Time.monotonic_ms()
    b = Time.monotonic_ms()
    (b >= a, Time.now())
`
	before := time.Now().UnixMilli()
	got := runTimerSrc(t, src)
	after := time.Now().UnixMilli()
	if got.Kind != runtime.KindTuple || len(got.Tuple) != 2 {
		t.Fatalf("result %s, want (bool, int)", got.Inspect())
	}
	if got.Tuple[0].Kind != runtime.KindBool || !got.Tuple[0].Bool {
		t.Fatalf("monotonic decreased: %s", got.Inspect())
	}
	now := got.Tuple[1]
	if now.Kind != runtime.KindInt || !now.IsSmall {
		t.Fatalf("now %s, want small int", now.Inspect())
	}
	if now.SmallInt < before-1000 || now.SmallInt > after+1000 {
		t.Fatalf("now %d, want within [%d, %d]", now.SmallInt, before-1000, after+1000)
	}
}

// TestTimerSendAfterCost — T-166: 100k взведённых таймеров не делают
// срабатывание одного обходом кучи (тот же критерий, что T-102).
func TestTimerSendAfterCost(t *testing.T) {
	const src = `module Main
fn arm(me, 0) -> ()
fn arm(me, n) ->
    _ = Timer.send_after(60000, me, :late)
    arm(me, n - 1)

fn main() ->
    me = self()
    arm(me, 100000)
    _ = Timer.send_after(1, me, :tick)
    recv
        :tick -> :ok
    after 5000 -> :slow
`
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
	got, err := m.RunMain(m.Global("main"))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if s := got.Inspect(); s != ":ok" {
		t.Fatalf("result %s, want :ok", s)
	}
	if v := m.Scheduler().TimerVisits(); v > 50 {
		t.Fatalf("timer bookkeeping visited %d entries for one firing among 100000 armed timers", v)
	}
}

// TestTimerSurvivesActor — T-166: таймер принадлежит VM и срабатывает
// после завершения актора, который его взвёл.
func TestTimerSurvivesActor(t *testing.T) {
	const src = `module Main
fn boom(parent) ->
    _ = Timer.send_after(15, parent, :ping)

fn main() ->
    me = self()
    spawn(() -> boom(me))
    recv
        :ping -> :ok
    after 200 -> :lost
`
	got := runTimerSrc(t, src)
	if s := got.Inspect(); s != ":ok" {
		t.Fatalf("result %s, want :ok", s)
	}
}

// TestTimerSendDropsWhenMailboxFull — T-166: полный ящик — сообщение
// таймера молча теряется, cancel после срабатывания → false.
func TestTimerSendDropsWhenMailboxFull(t *testing.T) {
	const src = `module Main
fn holder(parent) ->
    send(parent, self())
    await(make_ref(), 60000)

fn fill(pid, 0) -> ()
fn fill(pid, n) ->
    send(pid, :pad)
    fill(pid, n - 1)

fn main() ->
    me = self()
    spawn(() -> holder(me), { mailbox_hwm: 64 })
    pid = recv
        p -> p
    fill(pid, 64)
    ref = Timer.send_after(0, pid, :extra)
    recv
        :never -> :never
    after 20 -> (mailbox_size(pid), Timer.cancel(ref))
`
	got := runTimerSrc(t, src)
	if s := got.Inspect(); s != "(64, false)" {
		t.Fatalf("drop %s, want (64, false)", s)
	}
}

// TestTimerSendTypeErrors — T-166: плохие аргументы ловятся trap;
// ref из send_after без слота ответа, await на нём — :type_error.
func TestTimerSendTypeErrors(t *testing.T) {
	runModuleSync(t, `module Main
fn main() ->
    r = trap
        Timer.send_after(-1, self(), :x)
    assert(r == Error((:type_error, ((:timer, :send_after), -1))))
    r = trap
        Timer.send_after(:ms, self(), :x)
    assert(r == Error((:type_error, ((:timer, :send_after), :ms))))
    r = trap
        Timer.send_after(1, :pid, :x)
    assert(r == Error((:type_error, ((:timer, :send_after), :pid))))
    r = trap
        Timer.cancel(1)
    assert(r == Error((:type_error, ((:timer, :cancel), 1))))
    ref = Timer.send_after(5000, self(), :x)
    assert(Timer.cancel(ref) == true)
    r = trap
        await(ref, 10)
    assert(r == Error((:type_error, (:await, ref))))
    :ok
`)
}
