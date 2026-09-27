package vm_test

import (
	"testing"

	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/parser"
	"github.com/it1ro/brig-lang/internal/vm"
)

// TestTimerCostIndependentOfIdleActors — T-102 (#171): срабатывание
// таймера recv … after не обходит заблокированных акторов без таймера.
// 100 000 простаивающих акторов, 20 таймеров main: число осмотренных
// при обслуживании таймеров записей — O(число таймеров), а не O(акторов).
func TestTimerCostIndependentOfIdleActors(t *testing.T) {
	const src = `module Main
fn idle() ->
    recv
        (:stop) -> :ok

fn chain(0, parent) -> send(parent, :done)
fn chain(n, parent) ->
    spawn(() -> chain(n - 1, parent))
    idle()

fn tick(0) -> :ok
fn tick(k) ->
    recv
        (:never) -> :ok
    after 1 -> tick(k - 1)

fn main() ->
    me = self()
    spawn(() -> chain(100000, me))
    recv
        (:done) -> :ok
    tick(20)
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
	// Каждое срабатывание: nextDeadline + wakeExpired, по O(1) записей
	// кучи; запас на повторные проходы при раннем пробуждении sleep.
	if v := m.Scheduler().TimerVisits(); v > 20*10 {
		t.Fatalf("timer bookkeeping visited %d entries for 20 timers with 100000 idle actors", v)
	}
}

// TestTimerRemovedWhenMessageArrivesFirst — T-102 (#171): таймер, снятый
// пришедшим раньше сообщением, уходит из кучи, а не копится до дедлайна.
func TestTimerRemovedWhenMessageArrivesFirst(t *testing.T) {
	const src = `module Main
fn server(0) -> :ok
fn server(k) ->
    recv
        (:ping, from) ->
            send(from, :pong)
            server(k - 1)
    after 60000 -> :timeout

fn ping(pid, 0) -> :done
fn ping(pid, k) ->
    send(pid, (:ping, self()))
    recv
        (:pong) -> ping(pid, k - 1)

fn main() ->
    pid = spawn(() -> server(1000))
    ping(pid, 1000)
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
	if s := got.Inspect(); s != ":done" {
		t.Fatalf("result %s, want :done", s)
	}
	if n := m.Scheduler().ArmedTimers(); n != 0 {
		t.Fatalf("%d timers left armed after all recv took messages", n)
	}
}
