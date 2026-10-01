package vm_test

import (
	"errors"
	goparser "go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/parser"
	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/vm"
)

// fakeSignals — реализация Signal за интерфейсом vm.SignalHub без ОС:
// сигнал «приходит», когда тест зовёт fire.
type fakeSignals struct {
	mu     sync.Mutex
	subs   map[int]*fakeSub
	next   int
	opened chan []string
	closed chan []string
}

type fakeSub struct {
	names []string
	emit  func(string)
}

func newFakeSignals() *fakeSignals {
	return &fakeSignals{
		subs:   make(map[int]*fakeSub),
		opened: make(chan []string, 16),
		closed: make(chan []string, 16),
	}
}

func (h *fakeSignals) Open(names []string, emit func(string)) func() {
	h.mu.Lock()
	id := h.next
	h.next++
	h.subs[id] = &fakeSub{names: names, emit: emit}
	h.mu.Unlock()
	h.opened <- names
	return func() {
		h.mu.Lock()
		delete(h.subs, id)
		h.mu.Unlock()
		h.closed <- names
	}
}

// fire доставляет сигнал всем открытым подпискам на name.
func (h *fakeSignals) fire(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, s := range h.subs {
		if slices.Contains(s.names, name) {
			s.emit(name)
		}
	}
}

// open — число открытых подписок.
func (h *fakeSignals) open() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

func waitNames(t *testing.T, ch <-chan []string, what string) []string {
	t.Helper()
	select {
	case names := <-ch:
		return names
	case <-time.After(2 * time.Second):
		t.Fatalf("no %s within 2s", what)
		return nil
	}
}

// startModule компилирует модуль и запускает main в отдельной goroutine.
func startModule(t *testing.T, src string, hub vm.SignalHub) (*vm.VM, <-chan error) {
	t.Helper()
	return startModuleWith(t, src, func(m *vm.VM) {
		if hub != nil {
			m.SetSignals(hub)
		}
	})
}

// startModuleWith — startModule, setup подключает реализации портов.
func startModuleWith(t *testing.T, src string, setup func(*vm.VM)) (*vm.VM, <-chan error) {
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
	setup(m)
	for name, fn := range img.Functions {
		m.DefineGlobal(name, vm.FuncValue(fn))
	}
	done := make(chan error, 1)
	main := m.Global("main")
	go func() {
		_, err := m.RunMain(main)
		done <- err
	}()
	return m, done
}

func waitDone(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("program did not finish within 3s")
		return nil
	}
}

func assertRunning(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		t.Fatalf("program finished while a port is open: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
}

func globalIs(t *testing.T, m *vm.VM, name string, want runtime.Value) {
	t.Helper()
	got := m.Scheduler().GlobalGet(runtime.Atom(name))
	if !runtime.Equal(got, runtime.Variant("Some", want)) {
		t.Fatalf("Global.get(:%s) = %s, want Some(%s)", name, got.Inspect(), want.Inspect())
	}
}

// TestRunLoopWaitsForInject — ready пуст, но порт открыт: run-loop ждёт
// событие, а не выходит и не сообщает deadlock (§15.2).
func TestRunLoopWaitsForInject(t *testing.T) {
	t.Run("main finished, guard holds the port", func(t *testing.T) {
		hub := newFakeSignals()
		m, done := startModule(t, `module Main
fn guard(parent) ->
    port = Signal.subscribe([:sigterm])
    send(parent, :subscribed)
    recv
        (:signal, p, :sigterm) when p == port ->
            Global.put(:got, :sigterm)
            Port.close(port)

fn main() ->
    me = self()
    _pid = spawn(() -> guard(me))
    recv
        :subscribed -> :started
`, hub)
		waitNames(t, hub.opened, "subscription")
		assertRunning(t, done)
		hub.fire("sigterm")
		if err := waitDone(t, done); err != nil {
			t.Fatalf("run: %v", err)
		}
		globalIs(t, m, "got", runtime.Atom("sigterm"))
	})

	t.Run("main waits in recv without timers", func(t *testing.T) {
		hub := newFakeSignals()
		m, done := startModule(t, `module Main
fn main() ->
    _port = Signal.subscribe([:sigint])
    recv
        (:signal, _, name) -> Global.put(:got, name)
`, hub)
		waitNames(t, hub.opened, "subscription")
		assertRunning(t, done)
		hub.fire("sigint")
		if err := waitDone(t, done); err != nil {
			t.Fatalf("run: %v", err)
		}
		globalIs(t, m, "got", runtime.Atom("sigint"))
		// Выход закрывает порт main.
		waitNames(t, hub.closed, "close")
	})

	t.Run("finished main is a dead pid", func(t *testing.T) {
		// Программа пережила main: send ему теряется, watch — сразу :noproc.
		hub := newFakeSignals()
		m, done := startModule(t, `module Main
fn guard(parent) ->
    port = Signal.subscribe([:sigterm])
    send(parent, :subscribed)
    recv
        (:signal, _, _) ->
            Global.put(:send, send(parent, :late))
            ref = watch(parent)
            recv
                (:down, r, reason) -> Global.put(:down, (r == ref, reason))
            after 1000 -> Global.put(:down, :none)
            Port.close(port)

fn main() ->
    me = self()
    _pid = spawn(() -> guard(me))
    recv
        :subscribed -> ()
`, hub)
		waitNames(t, hub.opened, "subscription")
		assertRunning(t, done)
		hub.fire("sigterm")
		if err := waitDone(t, done); err != nil {
			t.Fatalf("run: %v", err)
		}
		globalIs(t, m, "send", runtime.Variant("Ok", runtime.Unit))
		globalIs(t, m, "down", runtime.Tuple(runtime.Bool(true), runtime.Atom("noproc")))
	})

	t.Run("timer and port together", func(t *testing.T) {
		hub := newFakeSignals()
		m, done := startModule(t, `module Main
fn main() ->
    port = Signal.subscribe([:sigterm])
    first = recv
        (:signal, _, _) -> :signal
    after 30 -> :timeout
    Global.put(:first, first)
    recv
        (:signal, _, name) -> Global.put(:got, name)
    Port.close(port)
`, hub)
		waitNames(t, hub.opened, "subscription")
		time.Sleep(80 * time.Millisecond)
		hub.fire("sigterm")
		if err := waitDone(t, done); err != nil {
			t.Fatalf("run: %v", err)
		}
		globalIs(t, m, "first", runtime.Atom("timeout"))
		globalIs(t, m, "got", runtime.Atom("sigterm"))
	})
}

// TestRunLoopExitsWithoutPorts — без открытых портов условие завершения
// прежнее: main закончился — программа закончилась; все ждут и событий
// ждать неоткуда — deadlock (§15.2).
func TestRunLoopExitsWithoutPorts(t *testing.T) {
	t.Run("main returns, idle actor left", func(t *testing.T) {
		_, done := startModule(t, `module Main
fn idle() ->
    recv
        _ -> idle()

fn main() ->
    _pid = spawn(idle)
    :done
`, newFakeSignals())
		if err := waitDone(t, done); err != nil {
			t.Fatalf("run: %v", err)
		}
	})

	t.Run("deadlock without timers and ports", func(t *testing.T) {
		_, done := startModule(t, `module Main
fn main() ->
    recv
        :never -> ()
`, newFakeSignals())
		err := waitDone(t, done)
		if err == nil || !strings.Contains(err.Error(), "deadlock") {
			t.Fatalf("want deadlock, got %v", err)
		}
	})

	t.Run("closed port does not hold the program", func(t *testing.T) {
		hub := newFakeSignals()
		_, done := startModule(t, `module Main
fn main() ->
    port = Signal.subscribe([:sigterm, :sigint])
    assert(Port.close(port) == ())
    assert(Port.close(port) == ())
    :done
`, hub)
		if err := waitDone(t, done); err != nil {
			t.Fatalf("run: %v", err)
		}
		waitNames(t, hub.closed, "close")
		select {
		case names := <-hub.closed:
			t.Fatalf("port closed twice: %v", names)
		default:
		}
	})

	t.Run("no signal implementation", func(t *testing.T) {
		// Без реализации порт открыт, но событий нет: закрытие — как обычно.
		_, done := startModule(t, `module Main
fn main() ->
    port = Signal.subscribe([:sigterm])
    Port.close(port)
`, nil)
		if err := waitDone(t, done); err != nil {
			t.Fatalf("run: %v", err)
		}
	})
}

// TestSignalDeliveredToOwner — событие (:signal, port, name) получает владелец
// каждого открытого порта, подписанного на name; остальные — нет (§12.12).
func TestSignalDeliveredToOwner(t *testing.T) {
	hub := newFakeSignals()
	m, done := startModule(t, `module Main
fn listen(parent, names) ->
    port = Signal.subscribe(names)
    send(parent, :subscribed)
    recv
        (:signal, p, name) when p == port ->
            send(parent, (:got, self(), name))
            Port.close(port)

fn subscribed() ->
    recv
        :subscribed -> ()

fn got(a, b) ->
    m = recv
        msg -> msg
    m == (:got, a, :sigterm) or m == (:got, b, :sigterm)

fn main() ->
    me = self()
    a = spawn(() -> listen(me, [:sigterm]))
    b = spawn(() -> listen(me, [:sigint, :sigterm]))
    c = spawn(() -> listen(me, [:sigint]))
    subscribed()
    subscribed()
    subscribed()
    Global.put(:ready, true)
    first = got(a, b)
    second = got(a, b)
    Global.put(:both, first and second)
    rest = recv
        m -> m
    after 50 -> :none
    Global.put(:rest, rest)
    exit(c, :kill)
`, hub)
	for range 3 {
		waitNames(t, hub.opened, "subscription")
	}
	hub.fire("sigterm")
	if err := waitDone(t, done); err != nil {
		t.Fatalf("run: %v", err)
	}
	globalIs(t, m, "both", runtime.Bool(true))
	globalIs(t, m, "rest", runtime.Atom("none"))
	if n := hub.open(); n != 0 {
		t.Fatalf("%d subscriptions left open", n)
	}
}

// TestSignalOrderOnePort — события одного порта приходят в порядке
// возникновения (G5).
func TestSignalOrderOnePort(t *testing.T) {
	hub := newFakeSignals()
	m, done := startModule(t, `module Main
fn take() ->
    recv
        (:signal, _, name) -> name

fn main() ->
    port = Signal.subscribe([:sigterm, :sigint])
    Global.put(:order, [take(), take(), take()])
    Port.close(port)
`, hub)
	waitNames(t, hub.opened, "subscription")
	hub.fire("sigint")
	hub.fire("sigterm")
	hub.fire("sigint")
	if err := waitDone(t, done); err != nil {
		t.Fatalf("run: %v", err)
	}
	globalIs(t, m, "order", runtime.List(runtime.Atom("sigint"), runtime.Atom("sigterm"), runtime.Atom("sigint")))
}

// TestPortClosedOnOwnerDeath — смерть владельца закрывает порт в том же
// шаге, что и :down: программа не ждёт мёртвый порт, реализация сигнала
// освобождена, закрыть порт чужому нельзя (§12.12).
func TestPortClosedOnOwnerDeath(t *testing.T) {
	for _, tc := range []struct{ name, death string }{
		{"normal", "()"},
		{"raise", "raise(:boom)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hub := newFakeSignals()
			m, done := startModule(t, `module Main
fn owner(parent) ->
    send(parent, (:port, Signal.subscribe([:sigterm])))
    recv
        :die -> `+tc.death+`

fn is_close(r, port) ->
    match r
        Error((:type_error, ((:port, :close), p))) -> p == port
        _ -> false

fn main() ->
    me = self()
    o = spawn(() -> owner(me))
    ref = watch(o)
    port = recv
        (:port, p) -> p
    Global.put(:foreign, is_close(trap(Port.close(port)), port))
    send(o, :die)
    recv
        (:down, r, _) -> assert(r == ref)
    Global.put(:dead, is_close(trap(Port.close(port)), port))
`, hub)
			if err := waitDone(t, done); err != nil {
				t.Fatalf("run: %v", err)
			}
			globalIs(t, m, "foreign", runtime.Bool(true))
			globalIs(t, m, "dead", runtime.Bool(true))
			waitNames(t, hub.closed, "close")
		})
	}

	t.Run("exit kill", func(t *testing.T) {
		hub := newFakeSignals()
		_, done := startModule(t, `module Main
fn owner(parent) ->
    _port = Signal.subscribe([:sigterm])
    send(parent, :subscribed)
    recv
        _ -> ()

fn main() ->
    me = self()
    o = spawn(() -> owner(me))
    recv
        :subscribed -> ()
    exit(o, :kill)
`, hub)
		if err := waitDone(t, done); err != nil {
			t.Fatalf("run: %v", err)
		}
		waitNames(t, hub.closed, "close")
	})
}

// TestPortEventAfterCloseDropped — событие, которое реализация успела
// отдать после закрытия порта, владельцу не доставляется.
func TestPortEventAfterCloseDropped(t *testing.T) {
	hub := newFakeSignals()
	var mu sync.Mutex
	var stale func(string)
	spy := spySignals{hub: hub, seen: func(emit func(string)) {
		mu.Lock()
		stale = emit
		mu.Unlock()
	}}
	m, done := startModule(t, `module Main
fn main() ->
    port = Signal.subscribe([:sigterm])
    Port.close(port)
    got = recv
        m -> m
    after 300 -> :none
    Global.put(:got, got)
`, spy)
	waitNames(t, hub.closed, "close")
	mu.Lock()
	stale("sigterm")
	mu.Unlock()
	if err := waitDone(t, done); err != nil {
		t.Fatalf("run: %v", err)
	}
	globalIs(t, m, "got", runtime.Atom("none"))
}

// spySignals запоминает emit подписки, чтобы позвать его после закрытия.
type spySignals struct {
	hub  *fakeSignals
	seen func(func(string))
}

func (s spySignals) Open(names []string, emit func(string)) func() {
	s.seen(emit)
	return s.hub.Open(names, emit)
}

// TestPortValue — Port: identity-равенство, после Ref в term order, не
// сериализуется (§7.4, §14.8); неверные аргументы — :type_error.
func TestPortValue(t *testing.T) {
	runModuleSync(t, `module Main
fn is_err(r, op, v) ->
    match r
        Error((:type_error, (o, x))) -> o == op and x == v
        _ -> false

fn main() ->
    p = Signal.subscribe([:sigterm])
    q = Signal.subscribe([:sigterm])
    assert(p == p)
    assert(p != q)
    assert(p < q)
    assert(make_ref() < p)
    assert(is_err(trap(Signal.subscribe([])), (:signal, :subscribe), []))
    assert(is_err(trap(Signal.subscribe([:sighup])), (:signal, :subscribe), [:sighup]))
    assert(is_err(trap(Signal.subscribe(:sigterm)), (:signal, :subscribe), :sigterm))
    assert(is_err(trap(Signal.subscribe([:sigterm, 1])), (:signal, :subscribe), [:sigterm, 1]))
    r = make_ref()
    assert(is_err(trap(Port.close(r)), (:port, :close), r))
    Port.close(p)
    Port.close(q)
    :ok
`)
}

// TestSysHalt — Sys.halt завершает программу сразу с кодом: ensure не
// выполняются, открытые порты закрываются (§12.12).
func TestSysHalt(t *testing.T) {
	hub := newFakeSignals()
	m, done := startModule(t, `module Main
fn worker() ->
    _port = Signal.subscribe([:sigterm])
    trap
        ensure Global.put(:ensure, true)
        Sys.halt(3)

fn main() ->
    _pid = spawn(worker)
    recv
        _ -> ()
`, hub)
	err := waitDone(t, done)
	var h *vm.ErrHalt
	if !errors.As(err, &h) || h.Code != 3 {
		t.Fatalf("want ErrHalt{3}, got %v", err)
	}
	if got := m.Scheduler().GlobalGet(runtime.Atom("ensure")); !runtime.Equal(got, runtime.Variant("None")) {
		t.Fatalf("ensure ran on halt: %s", got.Inspect())
	}
	waitNames(t, hub.closed, "close")

	runModuleSync(t, `module Main
fn is_halt(r, v) ->
    match r
        Error((:type_error, ((:sys, :halt), x))) -> x == v
        _ -> false

fn main() ->
    assert(is_halt(trap(Sys.halt(-1)), -1))
    assert(is_halt(trap(Sys.halt(256)), 256))
    assert(is_halt(trap(Sys.halt(:ok)), :ok))
    :ok
`)
}

// TestVMCoreNoOSPorts — ядро VM не импортирует то, что ждёт ОС: реализации
// портов живут за интерфейсом (R14, docs/02 §6).
func TestVMCoreNoOSPorts(t *testing.T) {
	// Сеть — тоже за интерфейсом (HTTPHub, T-229): ни net, ни net/http.
	banned := []string{"os/signal", "os/exec", "net", "net/http"}
	// Файлы — тоже за интерфейсом (FileHub, T-228): ядро не открывает их само.
	fileCalls := regexp.MustCompile(`\bos\.(Open|OpenFile|Create|ReadFile|WriteFile|ReadDir|Stat)\b`)
	for _, dir := range []string{".", "../runtime"} {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			ast, err := goparser.ParseFile(token.NewFileSet(), f, nil, goparser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			if loc := fileCalls.Find(src); loc != nil {
				t.Errorf("%s calls %s", f, loc)
			}
			for _, imp := range ast.Imports {
				path := strings.Trim(imp.Path.Value, `"`)
				if slices.Contains(banned, path) || strings.HasPrefix(path, "net/") {
					t.Errorf("%s imports %s", f, path)
				}
			}
		}
	}
}
