package repl_test

import (
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/it1ro/brig-lang/internal/repl"
	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/vm"
)

// sessionHarness — REPL-сессия со счётчиком редукций фонового планировщика.
type sessionHarness struct {
	m *vm.VM
	s *repl.Session
	n *atomic.Uint64
}

func newSession(t *testing.T) *sessionHarness {
	t.Helper()
	m := vm.New()
	n := new(atomic.Uint64)
	m.CountSessionReductions(n)
	s := repl.New(m, io.Discard)
	t.Cleanup(s.Close)
	return &sessionHarness{m: m, s: s, n: n}
}

func (h *sessionHarness) eval(t *testing.T, src string) runtime.Value {
	t.Helper()
	res, err := h.s.Eval(src)
	if err != nil {
		t.Fatalf("Eval %q: %v", src, err)
	}
	if len(res) == 0 {
		t.Fatalf("Eval %q: no result", src)
	}
	return res[len(res)-1].Value
}

// waitRunning ждёт, пока текущий ввод исполнит хотя бы одну редукцию
// сверх before: значит, работа уже стоит на акторе сессии.
func (h *sessionHarness) waitRunning(t *testing.T, before uint64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for h.n.Load() == before {
		if time.Now().After(deadline) {
			t.Fatal("input did not start")
		}
		time.Sleep(time.Millisecond)
	}
}

// TestSessionStableSelf — T-205: self() в двух вводах подряд — один pid.
func TestSessionStableSelf(t *testing.T) {
	h := newSession(t)
	a := h.eval(t, "self()\n")
	b := h.eval(t, "self()\n")
	if a.Inspect() != b.Inspect() {
		t.Fatalf("self() changed: %s then %s", a.Inspect(), b.Inspect())
	}
	both := h.eval(t, "[self(), self()]\n")
	want := "[" + a.Inspect() + ", " + a.Inspect() + "]"
	if both.Inspect() != want {
		t.Fatalf("[self(), self()] = %s, want %s", both.Inspect(), want)
	}
}

// TestSessionActorsRunBetweenInputs — T-205: актор, заспавненный в одном
// вводе, шлёт в ящик сессии, пока ввода нет; следующий recv это видит.
func TestSessionActorsRunBetweenInputs(t *testing.T) {
	h := newSession(t)
	h.eval(t, `me = self()
fn tick() ->
    send(me, :tick)
    recv
        _ -> ()
    after 20 -> ()
    tick()
spawn(tick)
`)
	time.Sleep(200 * time.Millisecond)
	got := h.eval(t, `recv
    :tick -> :ok
after 500 -> :timeout
`)
	if got.Inspect() != ":ok" {
		t.Fatalf("recv between inputs: got %s, want :ok", got.Inspect())
	}
}

// TestSessionRecvFromSpawned — T-205: recv с таймаутом ждёт фонового
// актора и не падает deadlock; recv без after и без отправителя снимается
// прерыванием.
func TestSessionRecvFromSpawned(t *testing.T) {
	h := newSession(t)
	pid := h.eval(t, "self()\n")
	got := h.eval(t, `parent = self()
fn pong() ->
    recv
        _ -> ()
    after 30 -> ()
    send(parent, :pong)
spawn(pong)
recv
    :pong -> :ok
after 500 -> :timeout
`)
	if got.Inspect() != ":ok" {
		t.Fatalf("recv from spawned: got %s, want :ok", got.Inspect())
	}

	h.eval(t, "fn wait() ->\n    recv\n        msg -> msg\n")
	before := h.n.Load()
	errCh := make(chan error, 1)
	go func() {
		// Вызов сжигает редукцию до RECVTAKE: по ней видно, что ввод уже на акторе.
		_, err := h.s.Eval("wait()\n")
		errCh <- err
	}()
	h.waitRunning(t, before)
	h.s.Interrupt()
	select {
	case err := <-errCh:
		if !errors.Is(err, vm.ErrInterrupted) {
			t.Fatalf("recv without sender: %v, want interrupted", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("recv without sender was not interrupted")
	}
	if again := h.eval(t, "self()\n"); again.Inspect() != pid.Inspect() {
		t.Fatalf("self() after interrupted recv: %s, want %s", again.Inspect(), pid.Inspect())
	}
}

// TestSessionInterruptInfiniteLoop — T-205: бесконечная хвостовая рекурсия
// снимается не позже чем через один редукционный слайс. trap прерывание
// не ловит. Привязки и pid сессии остаются, фоновый актор тоже.
func TestSessionInterruptInfiniteLoop(t *testing.T) {
	h := newSession(t)
	pid := h.eval(t, "self()\n")
	h.eval(t, "x = 1\n")
	h.eval(t, `me = self()
fn beat() ->
    send(me, :bg)
    recv
        _ -> ()
    after 30 -> ()
    beat()
spawn(beat)
`)

	before := h.n.Load()
	errCh := make(chan error, 1)
	go func() {
		_, err := h.s.Eval("trap\n    fn spin() -> spin()\n    spin()\n")
		errCh <- err
	}()
	deadline := time.Now().Add(2 * time.Second)
	for h.n.Load() < before+uint64(vm.DefaultSliceReductions) {
		if time.Now().After(deadline) {
			t.Fatalf("spin did not reach one slice (reds %d -> %d)", before, h.n.Load())
		}
		time.Sleep(time.Millisecond)
	}
	h.s.Interrupt()
	select {
	case err := <-errCh:
		if !errors.Is(err, vm.ErrInterrupted) {
			t.Fatalf("spin: %v, want interrupted (trap must not catch it)", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("infinite loop was not interrupted")
	}
	if got := h.m.ReductionsAfterInterrupt(); got > uint64(vm.DefaultSliceReductions) {
		t.Fatalf("reductions after interrupt: %d, want ≤ %d", got, vm.DefaultSliceReductions)
	}
	if x := h.eval(t, "x\n"); x.Inspect() != "1" {
		t.Fatalf("x after interrupt: %s, want 1", x.Inspect())
	}
	if again := h.eval(t, "self()\n"); again.Inspect() != pid.Inspect() {
		t.Fatalf("self() after interrupt: %s, want %s", again.Inspect(), pid.Inspect())
	}
	bg := h.eval(t, `recv
    :bg -> :ok
after 500 -> :timeout
`)
	if bg.Inspect() != ":ok" {
		t.Fatalf("background actor after interrupt: got %s, want :ok", bg.Inspect())
	}
}

// TestSessionRaiseKeepsSession — T-205: непойманный raise завершает ввод,
// но не актор сессии. Привязки предыдущих вводов и pid остаются.
func TestSessionRaiseKeepsSession(t *testing.T) {
	h := newSession(t)
	pid := h.eval(t, "self()\n")
	h.eval(t, "x = 1\n")
	res, err := h.s.Eval("a = 2\nraise(:boom)\nb = 3\n")
	if err == nil || len(res) != 1 || res[0].Name != "a" {
		t.Fatalf("raise: got %v, %v", res, err)
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("raise error: %v, want boom", err)
	}
	if again := h.eval(t, "self()\n"); again.Inspect() != pid.Inspect() {
		t.Fatalf("self() after raise: %s, want %s", again.Inspect(), pid.Inspect())
	}
	if x := h.eval(t, "x\n"); x.Inspect() != "1" {
		t.Fatalf("x after raise: %s, want 1", x.Inspect())
	}
	if a := h.eval(t, "a\n"); a.Inspect() != "2" {
		t.Fatalf("a after raise: %s, want 2", a.Inspect())
	}
	_, err = h.s.Eval("b\n")
	if err == nil {
		t.Fatal("b was bound after raise")
	}
	if y := h.eval(t, "y = x + 1\n"); y.Inspect() != "2" {
		t.Fatalf("next input after raise: %s, want 2", y.Inspect())
	}
}
