package vm_test

import (
	"sync"
	"testing"
	"time"

	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/parser"
	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/vm"
)

// introspectPrelude — хелперы тестов интроспекции (§12.13).
const introspectPrelude = `module Main
fn idle() ->
    recv
        :stop -> :ok

fn take() ->
    recv
        m -> m
    after 1000 -> :timeout

fn has(xs, p) -> any(xs, (x) -> x == p)

fn info(p) ->
    Some(i) = Actor.info(p)
    i
`

// TestActorList — Actor.list() содержит всех живых, включая вызывающего,
// и не содержит актора, для которого уже поставлен :down (§12.13).
func TestActorList(t *testing.T) {
	runModuleSync(t, introspectPrelude+`
fn main() ->
    me = self()
    assert(has(Actor.list(), me))
    a = spawn(() -> idle())
    b = spawn(() -> idle())
    xs = Actor.list()
    assert(has(xs, a))
    assert(has(xs, b))
    assert(len(xs) == 3)

    ref = watch(a)
    send(a, :stop)
    assert(take() == (:down, ref, :normal))
    assert(not has(Actor.list(), a))
    assert(has(Actor.list(), b))

    rb = watch(b)
    exit(b, :kill)
    assert(take() == (:down, rb, :kill))
    assert(Actor.list() == [me])

    c = spawn(() -> raise(:boom))
    rc = watch(c)
    (:down, r, (:raise, :boom)) = take()
    assert(r == rc)
    assert(Actor.list() == [me])
`)
}

// TestActorInfoObserverFields — поля observer в Actor.info (§12.13): name,
// status, watchers, watching, initial_fn.
func TestActorInfoObserverFields(t *testing.T) {
	runModuleSync(t, introspectPrelude+`
fn waiter(parent) ->
    ref = make_ref()
    send(parent, (:ref, ref))
    await(ref, 5000)

fn main() ->
    me = self()
    mine = info(me)
    assert(mine.name == None)
    assert(mine.status == :running)
    assert(mine.initial_fn == "main")
    assert(mine.watchers == [])
    assert(mine.watching == [])

    (p, _ref) = spawn_watched(idle)
    register(:idle, p)
    register(:second, p)
    i = info(p)
    assert(i.name == Some(:idle))
    assert(i.initial_fn == "idle")
    assert(i.watchers == [me])
    assert(i.watching == [])
    assert(info(me).watching == [p])

    unregister(:idle)
    assert(info(p).name == Some(:second))

    q = spawn(() -> idle())
    assert(info(q).initial_fn == "main$lambda$0$")
    r1 = watch(q)
    r2 = watch(q)
    link(p)
    assert(info(q).watchers == [me])
    assert(info(me).watching == [p, q])
    unwatch(r1)
    assert(info(q).watchers == [me])
    unwatch(r2)
    assert(info(q).watchers == [])
    assert(info(me).watching == [p])

    w = spawn(() -> waiter(me))
    (:ref, _wr) = take()
    take_nothing = recv
        _ -> :unexpected
    after 20 -> :ok
    assert(take_nothing == :ok)
    assert(info(w).status == :waiting)

    assert(info(p).status == :recv)
    assert(info(q).status == :recv)
    send(q, :stop)
    send(p, :stop)
    exit(w, :kill)
    ()
`)
}

// TestSchedulerSnapshotRace — Snapshot() с чужой goroutine, пока цикл
// сессии крутит акторы и бесконечный ввод: таблицу читает только горутина
// планировщика (-race), снимок видит живых с именами и связями.
func TestSchedulerSnapshotRace(t *testing.T) {
	prog, err := parser.ParseProgram(parser.ModeModule, `module Main
fn relay(n) ->
    recv
        k -> relay(n + k)

fn pump(r, k) ->
    send(r, k)
    pump(r, k + 1)

fn spin(n) -> spin(n + 1)

fn start() ->
    r = spawn(() -> relay(0))
    register(:relay, r)
    _ref = watch(r)
    spawn(() -> pump(r, 0))
    r
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
	s := m.Scheduler()
	if _, err := s.Snapshot(); err == nil {
		t.Fatal("Snapshot without session: want error")
	}
	if err := m.StartSession(); err != nil {
		t.Fatal(err)
	}
	defer m.CloseSession()

	relay, err := m.SessionEval(m.Global("start"), nil, nil)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if relay.Kind != runtime.KindPid {
		t.Fatalf("start: %v", relay)
	}

	// Бесконечный ввод: снимок обслуживается между слайсами, не после ввода.
	spinDone := make(chan error, 1)
	go func() {
		_, err := m.SessionEval(m.Global("spin"), []runtime.Value{runtime.Int(0)}, nil)
		spinDone <- err
	}()

	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				snap, err := s.Snapshot()
				if err != nil {
					t.Errorf("Snapshot: %v", err)
					return
				}
				if len(snap) != 3 {
					t.Errorf("Snapshot: %d actors, want 3", len(snap))
					return
				}
			}
		}()
	}
	wg.Wait()

	snap, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	var r *vm.ActorSnapshot
	for i := range snap {
		if snap[i].Pid == relay.Pid {
			r = &snap[i]
		}
	}
	if r == nil {
		t.Fatalf("relay %d not in snapshot %+v", relay.Pid, snap)
	}
	if !r.HasName || !runtime.Equal(r.Name, runtime.Atom("relay")) {
		t.Errorf("relay name = %v (%v), want :relay", r.Name, r.HasName)
	}
	if len(r.Watchers) != 1 || r.InitialFn != "start$lambda$0$" {
		t.Errorf("relay snapshot = %+v", *r)
	}
	if r.Reductions == 0 {
		t.Errorf("relay reductions = 0, want > 0")
	}
	if r.Status != "running" && r.Status != "recv" {
		t.Errorf("relay status = %q", r.Status)
	}

	m.Interrupt()
	select {
	case err := <-spinDone:
		if err != vm.ErrInterrupted {
			t.Errorf("spin: %v, want ErrInterrupted", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("spin input not interrupted")
	}
}
