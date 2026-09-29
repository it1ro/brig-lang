package vm_test

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/vm"
)

// memFiles — реализация File за интерфейсом vm.FileHub без ОС: файлы в
// памяти, одна goroutine на порт исполняет операции по порядку (G5).
// gate, если задан, держит каждую операцию, пока тест его не закроет.
type memFiles struct {
	mu     sync.Mutex
	files  map[string][]byte
	chunk  int
	gate   chan struct{}
	opened chan string
}

func newMemFiles(files map[string]string) *memFiles {
	h := &memFiles{files: make(map[string][]byte), chunk: 4, opened: make(chan string, 16)}
	for p, c := range files {
		h.files[p] = []byte(c)
	}
	return h
}

func (h *memFiles) content(path string) (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	b, ok := h.files[path]
	return string(b), ok
}

type memStream struct {
	h    *memFiles
	path string
	emit func(vm.StreamEvent)
	ops  chan func()
	off  int
	err  string
}

func (h *memFiles) Open(path, mode string, emit func(vm.StreamEvent)) vm.Stream {
	st := &memStream{h: h, path: path, emit: emit, ops: make(chan func(), 1024)}
	gate := h.gate
	st.ops <- func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		switch _, ok := h.files[path]; {
		case mode == "read" && !ok:
			st.err = "enoent"
		case mode == "write" || !ok:
			h.files[path] = nil
		}
	}
	go func() {
		for op := range st.ops {
			if gate != nil {
				<-gate
			}
			op()
		}
	}()
	h.opened <- path
	return st
}

func (st *memStream) Read() {
	st.ops <- func() {
		if st.err != "" {
			st.emit(vm.StreamEvent{Err: st.err})
			return
		}
		st.h.mu.Lock()
		b := st.h.files[st.path][st.off:]
		st.h.mu.Unlock()
		if len(b) == 0 {
			st.emit(vm.StreamEvent{EOF: true})
			return
		}
		b = b[:min(len(b), st.h.chunk)]
		st.off += len(b)
		st.emit(vm.StreamEvent{Data: append([]byte(nil), b...)})
	}
}

func (st *memStream) Write(b []byte) {
	st.ops <- func() {
		if st.err != "" {
			st.emit(vm.StreamEvent{Err: st.err})
			return
		}
		st.h.mu.Lock()
		st.h.files[st.path] = append(st.h.files[st.path], b...)
		st.h.mu.Unlock()
		st.emit(vm.StreamEvent{Written: len(b)})
	}
}

func (st *memStream) Close(done func()) {
	st.ops <- done
	close(st.ops)
}

func withFiles(h vm.FileHub) func(*vm.VM) {
	return func(m *vm.VM) { m.SetFiles(h) }
}

func waitOpened(t *testing.T, h *memFiles, path string) {
	t.Helper()
	for {
		select {
		case p := <-h.opened:
			if p == path {
				return
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%s not opened within 2s", path)
		}
	}
}

// TestStreamRequestOneEvent — один Port.request даёт одно событие,
// повтор до ответа второго не даёт; после :port_eof порт закрыт (§12.12).
func TestStreamRequestOneEvent(t *testing.T) {
	h := newMemFiles(map[string]string{"in.txt": "hello world"})
	m, done := startModuleWith(t, `module Main
fn read_all(port, acc, n) ->
    Ok(()) = Port.request(port)
    Ok(()) = Port.request(port)
    recv
        (:port_data, p, chunk) when p == port -> read_all(port, Bytes.concat(acc, chunk), n + 1)
        (:port_eof, p) when p == port -> (acc, n)

fn main() ->
    port = File.open("in.txt", :read)
    (data, n) = read_all(port, b"", 0)
    Global.put(:data, data)
    Global.put(:chunks, n)
    Global.put(:after, Port.request(port))
    Global.put(:write, Port.write(port, "x"))
    left = recv
        msg -> msg
    after 50 -> :none
    Global.put(:left, left)
`, withFiles(h))
	if err := waitDone(t, done); err != nil {
		t.Fatalf("run: %v", err)
	}
	globalIs(t, m, "data", runtime.Bytes([]byte("hello world")))
	globalIs(t, m, "chunks", runtime.Int(3))
	closed := runtime.Variant("Error", runtime.Atom("closed"))
	globalIs(t, m, "after", closed)
	globalIs(t, m, "write", closed)
	globalIs(t, m, "left", runtime.Atom("none"))
}

// TestStreamWriteBusyReady — запись сверх порога — Error(:busy), затем
// ровно одно :port_ready; принятое дописывается по порядку (§12.12).
func TestStreamWriteBusyReady(t *testing.T) {
	h := newMemFiles(nil)
	h.gate = make(chan struct{})
	m, done := startModuleWith(t, `module Main
fn grow(d, 0) -> d
fn grow(d, n) -> grow([d, d], n - 1)

fn main() ->
    port = File.open("out.bin", :write)
    Global.put(:first, Port.write(port, grow(b"abcd", 14)))
    Global.put(:second, Port.write(port, "x"))
    Global.put(:third, Port.write(port, "x"))
    _mark = File.open("mark", :append)
    recv
        (:port_ready, p) when p == port -> ()
    Global.put(:fourth, Port.write(port, ["tail", [b"!"]]))
    extra = recv
        msg -> msg
    after 50 -> :none
    Global.put(:extra, extra)
    Port.close(port)
`, withFiles(h))
	waitOpened(t, h, "mark")
	assertRunning(t, done)
	close(h.gate)
	if err := waitDone(t, done); err != nil {
		t.Fatalf("run: %v", err)
	}
	ok := runtime.Variant("Ok", runtime.Unit)
	busy := runtime.Variant("Error", runtime.Atom("busy"))
	globalIs(t, m, "first", ok)
	globalIs(t, m, "second", busy)
	globalIs(t, m, "third", busy)
	globalIs(t, m, "fourth", ok)
	globalIs(t, m, "extra", runtime.Atom("none"))
	got, _ := h.content("out.bin")
	if want := strings.Repeat("abcd", 1<<14) + "tail!"; got != want {
		t.Fatalf("out.bin: %d bytes, suffix %q; want %d bytes", len(got), got[max(0, len(got)-8):], len(want))
	}
}

// TestStreamCloseFlushes — Port.close дописывает принятое: программа не
// выходит, пока ресурс не закрыт (§12.12).
func TestStreamCloseFlushes(t *testing.T) {
	h := newMemFiles(nil)
	h.gate = make(chan struct{})
	_, done := startModuleWith(t, `module Main
fn main() ->
    port = File.open("out.txt", :write)
    Ok(()) = Port.write(port, ["line ", b"1", "\n"])
    Port.close(port)
    :done
`, withFiles(h))
	waitOpened(t, h, "out.txt")
	assertRunning(t, done)
	close(h.gate)
	if err := waitDone(t, done); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got, _ := h.content("out.txt"); got != "line 1\n" {
		t.Fatalf("out.txt = %q", got)
	}
}

// TestStreamGiveMovesRequest — Port.give передаёт владение, активный
// запрос переходит с портом: ответ получает новый владелец (§12.12).
func TestStreamGiveMovesRequest(t *testing.T) {
	h := newMemFiles(map[string]string{"in.txt": "data"})
	h.gate = make(chan struct{})
	m, done := startModuleWith(t, `module Main
fn reader(parent) ->
    recv
        (:port_data, p, chunk) ->
            send(parent, (:got, chunk))
            Port.close(p)

fn main() ->
    me = self()
    port = File.open("in.txt", :read)
    Ok(()) = Port.request(port)
    r = spawn(() -> reader(me))
    Global.put(:give, Port.give(port, r))
    Global.put(:self_give, trap(Port.give(port, me)))
    _mark = File.open("mark", :append)
    recv
        (:got, chunk) -> Global.put(:got, chunk)
    left = recv
        msg -> msg
    after 50 -> :none
    Global.put(:left, left)
`, withFiles(h))
	waitOpened(t, h, "mark")
	close(h.gate)
	if err := waitDone(t, done); err != nil {
		t.Fatalf("run: %v", err)
	}
	globalIs(t, m, "give", runtime.Variant("Ok", runtime.Unit))
	globalIs(t, m, "got", runtime.Bytes([]byte("data")))
	globalIs(t, m, "left", runtime.Atom("none"))
	if got := m.Scheduler().GlobalGet(runtime.Atom("self_give")); !strings.Contains(got.Inspect(), ":type_error") {
		t.Fatalf("old owner give: %s", got.Inspect())
	}
}

// TestStreamQuietPortDoesNotHold — потоковый порт без запроса и без
// записи не держит ожидание: main ждёт — deadlock, main завершился —
// выход; с активным запросом программа ждёт (§15.2).
func TestStreamQuietPortDoesNotHold(t *testing.T) {
	t.Run("main waits, port idle", func(t *testing.T) {
		_, done := startModuleWith(t, `module Main
fn main() ->
    _port = File.open("in.txt", :read)
    recv
        :never -> ()
`, withFiles(newMemFiles(map[string]string{"in.txt": "x"})))
		err := waitDone(t, done)
		if err == nil || !strings.Contains(err.Error(), "deadlock") {
			t.Fatalf("want deadlock, got %v", err)
		}
	})

	t.Run("main finished, idle owner", func(t *testing.T) {
		m, done := startModuleWith(t, `module Main
fn idle() ->
    recv
        _ -> idle()

fn main() ->
    port = File.open("in.txt", :read)
    w = spawn(idle)
    Ok(()) = Port.give(port, w)
    :done
`, withFiles(newMemFiles(map[string]string{"in.txt": "x"})))
		if err := waitDone(t, done); err != nil {
			t.Fatalf("run: %v", err)
		}
		_ = m
	})

	t.Run("armed request holds", func(t *testing.T) {
		h := newMemFiles(map[string]string{"in.txt": "x"})
		h.gate = make(chan struct{})
		m, done := startModuleWith(t, `module Main
fn main() ->
    port = File.open("in.txt", :read)
    Ok(()) = Port.request(port)
    recv
        (:port_data, _, chunk) -> Global.put(:got, chunk)
`, withFiles(h))
		waitOpened(t, h, "in.txt")
		assertRunning(t, done)
		close(h.gate)
		if err := waitDone(t, done); err != nil {
			t.Fatalf("run: %v", err)
		}
		globalIs(t, m, "got", runtime.Bytes([]byte("x")))
	})
}

// TestStreamErrors — ошибка ресурса — :port_error и закрытие; неверные
// аргументы — :type_error; Error(:not_alive), Error(:closed) (§12.12).
func TestStreamErrors(t *testing.T) {
	m, done := startModuleWith(t, `module Main
fn is_err(r, op, v) ->
    match r
        Error((:type_error, (o, x))) -> o == op and x == v
        _ -> false

fn main() ->
    missing = File.open("nope.txt", :read)
    Ok(()) = Port.request(missing)
    recv
        (:port_error, p, reason) when p == missing -> Global.put(:reason, reason)
    Global.put(:closed, Port.give(missing, self()))

    rd = File.open("in.txt", :read)
    wr = File.open("out.txt", :append)
    sig = Signal.subscribe([:sigterm])
    assert(is_err(trap(Port.request(wr)), :request, wr))
    assert(is_err(trap(Port.request(sig)), :request, sig))
    assert(is_err(trap(Port.write(rd, "x")), :write, rd))
    assert(is_err(trap(Port.write(sig, "x")), :write, sig))
    assert(is_err(trap(Port.write(wr, 5)), :write, 5))
    assert(is_err(trap(Port.write(wr, ["a", :b])), :write, ["a", :b]))
    assert(is_err(trap(Port.give(wr, :nobody)), :give, :nobody))
    assert(is_err(trap(File.open(:path, :read)), :open, :path))
    assert(is_err(trap(File.open("x", :rw)), :open, :rw))
    r = make_ref()
    assert(is_err(trap(Port.request(r)), :request, r))
    (dead, ref) = spawn_watched(() -> ())
    recv
        (:down, x, _) when x == ref -> ()
    Global.put(:dead, Port.give(wr, dead))
    Ok(()) = Port.write(wr, [])
    Port.close(sig)
    :ok
`, withFiles(newMemFiles(map[string]string{"in.txt": "x"})))
	if err := waitDone(t, done); err != nil {
		t.Fatalf("run: %v", err)
	}
	globalIs(t, m, "reason", runtime.Atom("enoent"))
	globalIs(t, m, "closed", runtime.Variant("Error", runtime.Atom("closed")))
	globalIs(t, m, "dead", runtime.Variant("Error", runtime.Atom("not_alive")))
}

// TestStreamNoFileSystem — VM без реализации File: порт открывается, любой
// запрос и запись — :port_error с :enotsup (§12.12).
func TestStreamNoFileSystem(t *testing.T) {
	m, done := startModule(t, `module Main
fn main() ->
    port = File.open("any", :write)
    Ok(()) = Port.write(port, "x")
    recv
        (:port_error, p, reason) when p == port -> Global.put(:reason, reason)
`, nil)
	if err := waitDone(t, done); err != nil {
		t.Fatalf("run: %v", err)
	}
	globalIs(t, m, "reason", runtime.Atom("enotsup"))
}
