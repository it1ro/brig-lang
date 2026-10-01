package vm_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/parser"
	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/vm"
)

// TestTelemetryAttachEmit — attach по префиксу, emit по совпадающему пути,
// detach снимает; обработчики — в порядке attach; доставка — в излучающем
// акторе (§12.14).
func TestTelemetryAttachEmit(t *testing.T) {
	runModuleSync(t, `module Main
fn emitter() ->
    Telemetry.emit([:app, :from_child], {}, {})
    :done

fn main() ->
    me = self()
    Ok(()) = Telemetry.attach(:sub, [:app], (e, m, meta) -> send(me, (:hit, e)))
    Ok(()) = Telemetry.attach(:wide, [], (e, m, meta) -> send(me, (:wide, e)))
    assert(Telemetry.attach(:sub, [:other], fn (e, m, meta) -> :no) == Error(:already_exists))
    assert(Telemetry.emit([:app, :request], {ms: 1}, {route: "/u"}) == ())
    assert(Telemetry.emit([:no, :match], {ms: 2}, {route: "/x"}) == ())
    Telemetry.detach(:sub)
    assert(Telemetry.detach(:sub) == ())
    assert(Telemetry.emit([:app, :again], {}, {}) == ())
    a = recv
        m -> m
    assert(a == (:hit, [:app, :request]))
    b = recv
        m -> m
    assert(b == (:wide, [:app, :request]))
    c = recv
        m -> m
    assert(c == (:wide, [:no, :match]))
    d = recv
        m -> m
    assert(d == (:wide, [:app, :again]))
    Telemetry.detach(:wide)
    child = spawn(emitter)
    Ok(()) = Telemetry.attach(:sub, [:app], (e, m, meta) -> send(me, (:hit, self())))
    e = recv
        m -> m
    assert(e == (:hit, child))
    h2 = fn (e, m) -> :no
    h3 = fn (e, m, meta) -> :yes
    assert(trap(Telemetry.attach(:bad, :no, h3)) == Error((:type_error, ((:telemetry, :attach), :no))))
    assert(trap(Telemetry.attach(:bad, [:app, 1], h3)) == Error((:type_error, ((:telemetry, :attach), [:app, 1]))))
    assert(trap(Telemetry.attach(:bad, [:app], h2)) == Error((:type_error, ((:telemetry, :attach), h2))))
    assert(trap(Telemetry.emit(:no, {}, {})) == Error((:type_error, ((:telemetry, :emit), :no))))
    assert(trap(Telemetry.emit([], {}, {})) == Error((:type_error, ((:telemetry, :emit), []))))
    assert(trap(Telemetry.emit([:a, 1], {}, {})) == Error((:type_error, ((:telemetry, :emit), [:a, 1]))))
    assert(trap(Telemetry.emit([:a], :no, {})) == Error((:type_error, ((:telemetry, :emit), :no))))
    assert(trap(Telemetry.emit([:a], {}, :no)) == Error((:type_error, ((:telemetry, :emit), :no))))
    :ok
`)
}

// TestTelemetryHandlerFailureDetaches — непойманный raise обработчика
// отцепляет его, излучает [:telemetry, :handler, :failed] с {id, event,
// reason}, доставка продолжается со следующего, излучающий жив (§12.14);
// событие смерти доставляет служебный актор — его raise не роняет и его.
func TestTelemetryHandlerFailureDetaches(t *testing.T) {
	runModuleSync(t, `module Main
fn victim() ->
    raise((:crash, 1))

fn main() ->
    me = self()
    Ok(()) = Telemetry.attach(:bad, [:vm, :actor, :down], fn (e, m, meta) -> raise((:huh, e)))
    Ok(()) = Telemetry.attach(:telev, [:telemetry], (e, m, meta) -> send(me, (:telev, e, meta.id, meta.event, meta.reason)))
    Ok(()) = Telemetry.attach(:good, [:vm, :actor], (e, m, meta) -> send(me, (:good, e, m, meta)))
    v1 = spawn(victim)
    (:good, ea, ma, ta) = recv
        m -> m
    assert(ea == [:vm, :actor, :crash])
    assert(ta.pid == v1)
    b = recv
        m -> m
    assert(b == (:telev, [:telemetry, :handler, :failed], :bad, [:vm, :actor, :down], (:huh, [:vm, :actor, :down])))
    (:good, ec, mc, tc) = recv
        m -> m
    assert(ec == [:vm, :actor, :down])
    assert(tc.pid == v1)
    assert(tc.reason == (:raise, (:crash, 1)))
    v2 = spawn(victim)
    (:good, ed, md, td) = recv
        m -> m
    assert(ed == [:vm, :actor, :crash])
    assert(td.pid == v2)
    (:good, ee, me2, te) = recv
        m -> m
    assert(ee == [:vm, :actor, :down])
    assert(te.pid == v2)
    Ok(()) = Telemetry.attach(:boom, [:user], fn (e, m, meta) -> raise((:nope, e)))
    Ok(()) = Telemetry.attach(:keep, [:user], (e, m, meta) -> send(me, :kept))
    assert(Telemetry.emit([:user, :e], {}, {}) == ())
    u1 = recv
        m -> m
    assert(u1 == (:telev, [:telemetry, :handler, :failed], :boom, [:user, :e], (:nope, [:user, :e])))
    u2 = recv
        m -> m
    assert(u2 == :kept)
    assert(Telemetry.emit([:user, :e2], {}, {}) == ())
    u3 = recv
        m -> m
    assert(u3 == :kept)
    :ok
`)
}

// TestTelemetryVMEvents — [:vm, :spawn], [:vm, :actor, :crash] (с trace),
// [:vm, :actor, :down], [:vm, :mailbox, :hwm] от send и от потерявшего
// сообщение Timer.send_after — с полями §12.14 (T-220).
func TestTelemetryVMEvents(t *testing.T) {
	runModuleSync(t, `module Main
fn id(x) ->
    x

fn victim() ->
    _ = id(1)
    raise((:crash, 1))

fn filler() ->
    await(make_ref(), 60000)

fn fill(pid, 0) -> ()
fn fill(pid, n) ->
    _ = send(pid, :m)
    fill(pid, n - 1)

fn main() ->
    me = self()
    Ok(()) = Telemetry.attach(:vm, [:vm], (e, m, meta) -> send(me, (:evt, e, m, meta)))
    v = spawn(victim)
    (:evt, ea, ma, ta) = recv
        m -> m
    assert(ea == [:vm, :spawn])
    assert(ma.monotonic_ms >= 0)
    assert(ta.pid == v)
    assert(ta.parent == me)
    assert(ta.initial_fn == "victim")
    (:evt, eb, mb, tb) = recv
        m -> m
    assert(eb == [:vm, :actor, :crash])
    assert(mb.monotonic_ms >= 0)
    assert(mb.reductions >= 1)
    assert(mb.alloc_bytes >= 0)
    assert(tb.pid == v)
    assert(tb.name == None)
    assert(tb.reason == (:raise, (:crash, 1)))
    assert(len(tb.trace) >= 1)
    assert(tb.trace[0].fn == "victim")
    assert(tb.trace[0].line >= 1)
    (:evt, ec, mc, tc) = recv
        m -> m
    assert(ec == [:vm, :actor, :down])
    assert(mc.monotonic_ms >= 0)
    assert(mc.reductions >= 1)
    assert(mc.lifetime_ms >= 0)
    assert(tc.pid == v)
    assert(tc.name == None)
    assert(tc.reason == (:raise, (:crash, 1)))
    f = spawn(filler, { mailbox_hwm: 2 })
    (:evt, ed0, md0, td0) = recv
        m -> m
    assert(ed0 == [:vm, :spawn])
    assert(td0.pid == f)
    assert(td0.initial_fn == "filler")
    fill(f, 3)
    (:evt, ed, md, td) = recv
        m -> m
    assert(ed == [:vm, :mailbox, :hwm])
    assert(md.monotonic_ms >= 0)
    assert(md.mailbox == 2)
    assert(md.hwm == 2)
    assert(td.pid == f)
    assert(td.from == Some(me))
    _ = Timer.send_after(1, f, :late)
    (:evt, ee, me2, te) = recv
        m -> m
    assert(ee == [:vm, :mailbox, :hwm])
    assert(me2.hwm == 2)
    assert(te.pid == f)
    assert(te.from == None)
    assert(len(Actor.list()) == 3)
    :ok
`)
}

// teleRunners — программы цены без подписчиков (§12.14): обе зовут
// трёхаргументный натив в цикле; baseline — nop, emit — Telemetry.emit,
// spawn — цикл spawn без подписок. ev/m0/meta0 — нативы с готовыми
// значениями: программа ничего не строит.
var teleRunners = map[string]string{
	"baseline": `module Main
fn loop(n) ->
    if n == 0 then :ok else step(n)

fn step(n) ->
    nop(ev(), m0(), meta0())
    loop(n - 1)

fn main() ->
    loop(NLOOP)
`,
	"emit": `module Main
fn loop(n) ->
    if n == 0 then :ok else step(n)

fn step(n) ->
    Telemetry.emit(ev(), m0(), meta0())
    loop(n - 1)

fn main() ->
    loop(NLOOP)
`,
	"spawn": `module Main
fn dummy() ->
    recv
        m -> m

fn loop(n) ->
    if n == 0 then :ok else step(n)

fn step(n) ->
    _ = spawn(dummy)
    loop(n - 1)

fn main() ->
    loop(NLOOP)
`,
}

// newTeleRun компилирует программу name и возвращает прогон main на новой
// VM; ev/m0/meta0/nop — нативы с готовыми значениями.
func newTeleRun(tb testing.TB, name string, n int) func() {
	tb.Helper()
	src := strings.ReplaceAll(teleRunners[name], "NLOOP", strconv.Itoa(n))
	prog, err := parser.ParseProgram(parser.ModeModule, src)
	if err != nil {
		tb.Fatalf("parse: %v", err)
	}
	img, err := compiler.New().Compile(prog)
	if err != nil {
		tb.Fatalf("compile: %v", err)
	}
	if img.Main == nil {
		tb.Fatal("no main")
	}
	mk := func(name string, v runtime.Value) runtime.Value {
		return runtime.Func(&runtime.FuncValue{Name: name, Arity: 0, IsNative: true,
			Native: func(runtime.Caller, []runtime.Value) (runtime.Value, error) { return v, nil }})
	}
	ev := mk("ev", runtime.List(runtime.Atom("app")))
	m0 := mk("m0", runtime.Record("", nil))
	meta0 := mk("meta0", runtime.Record("", nil))
	nop := runtime.Func(&runtime.FuncValue{Name: "nop", Arity: 3, IsNative: true,
		Native: func(runtime.Caller, []runtime.Value) (runtime.Value, error) { return runtime.Unit, nil }})
	return func() {
		m := vm.New()
		for name, fn := range img.Functions {
			m.DefineGlobal(name, vm.FuncValue(fn))
		}
		m.DefineGlobal("ev", ev)
		m.DefineGlobal("m0", m0)
		m.DefineGlobal("meta0", meta0)
		m.DefineGlobal("nop", nop)
		if _, err := m.RunMain(m.Global("main")); err != nil {
			tb.Fatalf("run: %v", err)
		}
	}
}

// TestTelemetryNoSubscribersCost — emit без подписок не дороже обычного
// трёхаргументного натива: прирост аллокаций на emit ≈ 0 (§12.14).
func TestTelemetryNoSubscribersCost(t *testing.T) {
	const n = 1000
	base := testing.AllocsPerRun(20, newTeleRun(t, "baseline", n))
	emit := testing.AllocsPerRun(20, newTeleRun(t, "emit", n))
	if per := (emit - base) / n; per > 0.01 {
		t.Errorf("allocs per emit without subscribers = %.3f (baseline %.0f, emit %.0f), want 0",
			per, base, emit)
	}
}

// BenchmarkTelemetryNoSubscribers — профиль emit и spawn без подписок
// (§12.14; benchstat против main — в body PR).
func BenchmarkTelemetryNoSubscribers(b *testing.B) {
	for _, name := range []string{"baseline", "emit", "spawn"} {
		b.Run(name, func(b *testing.B) {
			run := newTeleRun(b, name, 1000)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				run()
			}
		})
	}
}
