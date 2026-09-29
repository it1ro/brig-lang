package repl_test

import (
	"errors"
	"io"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/it1ro/brig-lang/internal/repl"
	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/vm"
)

// signalStub — vm.SignalHub без ОС: fire доставляет сигнал подпискам.
type signalStub struct {
	mu     sync.Mutex
	subs   map[int]*stubSub
	next   int
	opened chan struct{}
	closed chan struct{}
}

type stubSub struct {
	names []string
	emit  func(string)
}

func newSignalStub() *signalStub {
	return &signalStub{
		subs:   make(map[int]*stubSub),
		opened: make(chan struct{}, 16),
		closed: make(chan struct{}, 16),
	}
}

func (h *signalStub) Open(names []string, emit func(string)) func() {
	h.mu.Lock()
	id := h.next
	h.next++
	h.subs[id] = &stubSub{names: names, emit: emit}
	h.mu.Unlock()
	h.opened <- struct{}{}
	return func() {
		h.mu.Lock()
		delete(h.subs, id)
		h.mu.Unlock()
		h.closed <- struct{}{}
	}
}

func (h *signalStub) fire(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, s := range h.subs {
		if slices.Contains(s.names, name) {
			s.emit(name)
		}
	}
}

func waitStub(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("no %s within 2s", what)
	}
}

func newSignalSession(t *testing.T) (*repl.Session, *signalStub) {
	t.Helper()
	m := vm.New()
	hub := newSignalStub()
	m.SetSignals(hub)
	s := repl.New(m, io.Discard)
	t.Cleanup(s.Close)
	return s, hub
}

// TestSessionSignalPort — порт в сессии: событие ждёт в ящике до recv
// следующего ввода; фоновый владелец получает своё между вводами;
// закрытие сессии освобождает подписки (§12.12, §11.4).
func TestSessionSignalPort(t *testing.T) {
	s, hub := newSignalSession(t)
	h := &sessionHarness{s: s}
	h.eval(t, "me = self()\n")
	h.eval(t, "port = Signal.subscribe([:sigterm])\n")
	waitStub(t, hub.opened, "session subscription")
	h.eval(t, `fn bg() ->
    _p = Signal.subscribe([:sigterm])
    send(me, :subscribed)
    recv
        (:signal, _, name) -> send(me, (:bg, name))
`)
	h.eval(t, "_bg = spawn(bg)\n")
	waitStub(t, hub.opened, "background subscription")
	if got := h.eval(t, "recv\n    :subscribed -> :ok\n"); !runtime.Equal(got, runtime.Atom("ok")) {
		t.Fatalf("subscribed: %s", got.Inspect())
	}
	hub.fire("sigterm")
	// Фоновый владелец умер после ответа — его порт закрыт.
	waitStub(t, hub.closed, "background close")
	got := h.eval(t, "[recv\n    (:signal, _, n) -> n\n, recv\n    (:bg, n) -> n\n]\n")
	want := runtime.List(runtime.Atom("sigterm"), runtime.Atom("sigterm"))
	if !runtime.Equal(got, want) {
		t.Fatalf("got %s, want %s", got.Inspect(), want.Inspect())
	}
	s.Close()
	waitStub(t, hub.closed, "session close")
}

// TestSessionHalt — Sys.halt в сессии: ввод получает ErrHalt с кодом,
// следующий ввод — тоже; порты закрыты (§12.12).
func TestSessionHalt(t *testing.T) {
	s, hub := newSignalSession(t)
	h := &sessionHarness{s: s}
	h.eval(t, "_port = Signal.subscribe([:sigint])\n")
	waitStub(t, hub.opened, "subscription")
	var halt *vm.ErrHalt
	if _, err := s.Eval("Sys.halt(4)\n"); !errors.As(err, &halt) || halt.Code != 4 {
		t.Fatalf("want ErrHalt{4}, got %v", err)
	}
	waitStub(t, hub.closed, "close on halt")
	if _, err := s.Eval("1 + 1\n"); !errors.As(err, &halt) {
		t.Fatalf("input after halt: want ErrHalt, got %v", err)
	}
}
