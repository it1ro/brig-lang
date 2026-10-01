package vm_test

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/vm"
)

// memHTTP — реализация HttpServer за интерфейсом vm.HTTPHub без сети:
// запросы в тест подаёт send, ответ читается из memReq (§12.12).
type memHTTP struct {
	failBind string // не пусто — Accept отвечает этой ошибкой адреса
	listened chan *memListener
}

func newMemHTTP() *memHTTP { return &memHTTP{listened: make(chan *memListener, 4)} }

func (h *memHTTP) Listen(addr string, emit func(vm.HTTPEvent)) vm.HTTPListener {
	l := &memListener{h: h, addr: addr, emit: emit}
	h.listened <- l
	return l
}

// memListener — очередь запросов до Brig и отданные, но не начатые.
type memListener struct {
	h    *memHTTP
	addr string
	emit func(vm.HTTPEvent)

	mu      sync.Mutex
	queue   []*memReq
	offered []*memReq
	want    bool
	closed  string // "", "close", "abort"
}

func (l *memListener) Accept() {
	l.mu.Lock()
	defer l.mu.Unlock()
	switch {
	case l.h.failBind != "":
		l.emit(vm.HTTPEvent{Err: l.h.failBind})
	case len(l.queue) > 0:
		r := l.queue[0]
		l.queue = l.queue[1:]
		l.offer(r)
	default:
		l.want = true
	}
}

func (l *memListener) offer(r *memReq) {
	l.offered = append(l.offered, r)
	l.emit(vm.HTTPEvent{Req: r, Head: r.head})
}

func (l *memListener) send(r *memReq) {
	l.mu.Lock()
	defer l.mu.Unlock()
	switch {
	case l.closed != "":
		r.reject()
	case l.want:
		l.want = false
		l.offer(r)
	default:
		l.queue = append(l.queue, r)
	}
}

// shut — очередь и отданные, но не начатые запросы получают 503.
func (l *memListener) shut(how string, done func()) {
	l.mu.Lock()
	l.closed = how
	for _, r := range append(l.queue, l.offered...) {
		r.reject()
	}
	l.queue = nil
	l.mu.Unlock()
	done()
}

func (l *memListener) Close(done func()) { l.shut("close", done) }
func (l *memListener) Abort(done func()) { l.shut("abort", done) }

// memReq — один запрос: тело порциями по chunk байт, ответ копится в
// status/headers/out; fin закрывается, когда ответ закончен (end),
// оборван (abort) или отклонён транспортом (reject, 503).
type memReq struct {
	head  vm.HTTPHead
	body  []byte
	chunk int

	mu      sync.Mutex
	emit    func(vm.StreamEvent)
	started bool
	start   chan struct{}
	ops     chan func()
	off     int
	status  int
	headers [][2]string
	out     []byte
	how     string
	fin     chan struct{}
}

func newMemReq(method, path string, body string) *memReq {
	r := &memReq{
		head: vm.HTTPHead{
			Method: method, Path: path, Query: "x=1",
			Headers: [][2]string{{"host", "example.test"}, {"x-a", "1"}},
			Host:    "10.0.0.1", Port: 5555,
		},
		body: []byte(body), chunk: 4,
		start: make(chan struct{}), ops: make(chan func(), 64), fin: make(chan struct{}),
	}
	go func() {
		for op := range r.ops {
			op()
		}
	}()
	return r
}

func (r *memReq) Start(emit func(vm.StreamEvent)) {
	r.mu.Lock()
	r.emit, r.started = emit, true
	r.mu.Unlock()
	close(r.start)
}

func (r *memReq) Read() {
	r.ops <- func() {
		b := r.body[r.off:]
		if len(b) == 0 {
			r.emit(vm.StreamEvent{EOF: true})
			return
		}
		b = b[:min(len(b), r.chunk)]
		r.off += len(b)
		r.emit(vm.StreamEvent{Data: append([]byte(nil), b...)})
	}
}

func (r *memReq) Respond(status int, headers [][2]string) {
	r.ops <- func() {
		r.mu.Lock()
		r.status, r.headers = status, headers
		r.mu.Unlock()
	}
}

func (r *memReq) Write(b []byte) {
	r.ops <- func() {
		r.mu.Lock()
		r.out = append(r.out, b...)
		r.mu.Unlock()
		r.emit(vm.StreamEvent{Written: len(b)})
	}
}

func (r *memReq) end(how string, done func()) {
	r.ops <- func() {
		r.mu.Lock()
		if r.status == 0 {
			r.status = 500
		}
		r.how = how
		r.mu.Unlock()
		close(r.fin)
		done()
	}
}

func (r *memReq) Close(done func()) { r.end("end", done) }
func (r *memReq) Abort(done func()) { r.end("abort", done) }

// reject — транспорт отвечает 503 запросу, который Brig не начал.
func (r *memReq) reject() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started {
		return
	}
	r.status, r.how = 503, "reject"
	close(r.fin)
}

// disconnect — клиент ушёл: ошибка без запроса (§12.12).
func (r *memReq) disconnect() {
	r.ops <- func() { r.emit(vm.StreamEvent{Err: "closed"}) }
}

func withHTTP(h vm.HTTPHub) func(*vm.VM) {
	return func(m *vm.VM) { m.SetHTTP(h) }
}

func (h *memHTTP) listener(t *testing.T) *memListener {
	t.Helper()
	select {
	case l := <-h.listened:
		return l
	case <-time.After(2 * time.Second):
		t.Fatal("HttpServer.listen not called within 2s")
		return nil
	}
}

// response ждёт конца ответа: статус, тело и как он кончился.
func (r *memReq) response(t *testing.T) (int, string, string) {
	t.Helper()
	select {
	case <-r.fin:
	case <-time.After(2 * time.Second):
		t.Fatalf("request %s not finished within 2s", r.head.Path)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status, string(r.out), r.how
}

func waitStarted(t *testing.T, r *memReq) {
	t.Helper()
	select {
	case <-r.start:
	case <-time.After(2 * time.Second):
		t.Fatalf("request %s not delivered within 2s", r.head.Path)
	}
}

func wantResponse(t *testing.T, r *memReq, status int, body, how string) {
	t.Helper()
	gs, gb, gh := r.response(t)
	if gs != status || gb != body || gh != how {
		t.Fatalf("%s: got (%d, %q, %s), want (%d, %q, %s)", r.head.Path, gs, gb, gh, status, body, how)
	}
}

// TestHttpRequestPullOneEvent — Port.request(listener) даёт ровно одно
// (:http_request, listener, req, head); повтор до ответа второго не даёт,
// остальные запросы ждут у транспорта (§12.12).
func TestHttpRequestPullOneEvent(t *testing.T) {
	h := newMemHTTP()
	m, done := startModuleWith(t, `module Main
fn main() ->
    l = HttpServer.listen("127.0.0.1:8080")
    Ok(()) = Port.request(l)
    Ok(()) = Port.request(l)
    (req, head) = recv
        (:http_request, p, r, hd) when p == l -> (r, hd)
    Global.put(:head, head)
    Global.put(:owner, Port.give(req, self()))
    left = recv
        msg -> msg
    after 50 -> :none
    Global.put(:left, left)
    Port.close(req)
    Ok(()) = Port.request(l)
    recv
        (:http_request, _, req2, head2) ->
            Global.put(:second, head2.path)
            Port.close(req2)
    Port.close(l)
`, withHTTP(h))
	l := h.listener(t)
	if l.addr != "127.0.0.1:8080" {
		t.Fatalf("addr = %q", l.addr)
	}
	r1, r2 := newMemReq("get", "/a", ""), newMemReq("post", "/b", "")
	l.send(r1)
	l.send(r2)
	if err := waitDone(t, done); err != nil {
		t.Fatalf("run: %v", err)
	}
	head := runtime.Record("", []runtime.RecordField{
		{Name: "method", Val: runtime.Atom("get")},
		{Name: "path", Val: runtime.Str("/a")},
		{Name: "query", Val: runtime.Str("x=1")},
		{Name: "headers", Val: runtime.List(
			runtime.Tuple(runtime.Str("host"), runtime.Str("example.test")),
			runtime.Tuple(runtime.Str("x-a"), runtime.Str("1")))},
		{Name: "remote", Val: runtime.Tuple(runtime.Str("10.0.0.1"), runtime.Int(5555))},
	})
	globalIs(t, m, "head", head)
	globalIs(t, m, "owner", runtime.Variant("Ok", runtime.Unit))
	globalIs(t, m, "left", runtime.Atom("none"))
	globalIs(t, m, "second", runtime.Str("/b"))
	wantResponse(t, r1, 500, "", "end")
	wantResponse(t, r2, 500, "", "end")
}

// TestHttpRespondThenWriteClose — respond задаёт статус и заголовки, тело
// пишет общий Port.write (iodata), Port.close заканчивает ответ (§12.12).
func TestHttpRespondThenWriteClose(t *testing.T) {
	h := newMemHTTP()
	m, done := startModuleWith(t, `module Main
fn main() ->
    l = HttpServer.listen(":8080")
    Ok(()) = Port.request(l)
    recv
        (:http_request, _, req, _) ->
            hs = [("content-type", "text/plain"), ("set-cookie", "a=1"), ("set-cookie", "b=2")]
            Global.put(:respond, HttpServer.respond(req, 201, hs))
            Global.put(:write, Port.write(req, ["hello", [b", ", "world"]]))
            Port.close(req)
            Global.put(:closed, HttpServer.respond(req, 200, []))
            Global.put(:closed_write, Port.write(req, "x"))
    Port.close(l)
`, withHTTP(h))
	r := newMemReq("get", "/", "")
	h.listener(t).send(r)
	if err := waitDone(t, done); err != nil {
		t.Fatalf("run: %v", err)
	}
	ok := runtime.Variant("Ok", runtime.Unit)
	closed := runtime.Variant("Error", runtime.Atom("closed"))
	globalIs(t, m, "respond", ok)
	globalIs(t, m, "write", ok)
	globalIs(t, m, "closed", closed)
	globalIs(t, m, "closed_write", closed)
	wantResponse(t, r, 201, "hello, world", "end")
	want := [][2]string{{"content-type", "text/plain"}, {"set-cookie", "a=1"}, {"set-cookie", "b=2"}}
	if len(r.headers) != len(want) {
		t.Fatalf("headers = %v", r.headers)
	}
	for i := range want {
		if r.headers[i] != want[i] {
			t.Fatalf("headers = %v, want %v", r.headers, want)
		}
	}
}

// TestHttpWriteBeforeRespond — неявного 200 нет: write до respond,
// повторный respond, тело у 204 и неверные аргументы — :type_error (§12.12).
func TestHttpWriteBeforeRespond(t *testing.T) {
	h := newMemHTTP()
	m, done := startModuleWith(t, `module Main
fn is_err(r, op, v) ->
    match r
        Error((:type_error, (o, x))) -> o == op and x == v
        _ -> false

fn main() ->
    assert(is_err(trap(HttpServer.listen(8080)), (:http_server, :listen), 8080))
    assert(is_err(trap(HttpServer.listen("nope")), (:http_server, :listen), "nope"))
    assert(is_err(trap(HttpServer.listen(":0")), (:http_server, :listen), ":0"))
    assert(is_err(trap(HttpServer.listen(":65536")), (:http_server, :listen), ":65536"))
    assert(is_err(trap(HttpServer.listen("a:b:80")), (:http_server, :listen), "a:b:80"))
    l = HttpServer.listen("[::1]:8080")
    assert(is_err(trap(Port.write(l, "x")), (:port, :write), l))
    assert(is_err(trap(HttpServer.respond(l, 200, [])), (:http_server, :respond), l))
    f = File.open("x", :read)
    assert(is_err(trap(HttpServer.respond(f, 200, [])), (:http_server, :respond), f))
    Ok(()) = Port.request(l)
    recv
        (:http_request, _, req, _) ->
            assert(is_err(trap(Port.write(req, "x")), (:port, :write), req))
            assert(is_err(trap(Port.write(req, [])), (:port, :write), req))
            assert(is_err(trap(HttpServer.respond(req, 199, [])), (:http_server, :respond), 199))
            assert(is_err(trap(HttpServer.respond(req, 600, [])), (:http_server, :respond), 600))
            assert(is_err(trap(HttpServer.respond(req, "200", [])), (:http_server, :respond), "200"))
            bad = [("bad name", "v")]
            assert(is_err(trap(HttpServer.respond(req, 200, bad)), (:http_server, :respond), bad))
            empty = [("", "v")]
            assert(is_err(trap(HttpServer.respond(req, 200, empty)), (:http_server, :respond), empty))
            crlf = [("x-a", "a\r\nb")]
            assert(is_err(trap(HttpServer.respond(req, 200, crlf)), (:http_server, :respond), crlf))
            assert(is_err(trap(HttpServer.respond(req, 200, [:x])), (:http_server, :respond), [:x]))
            assert(is_err(trap(HttpServer.respond(req, 200, :x)), (:http_server, :respond), :x))
            Ok(()) = HttpServer.respond(req, 204, [("x-a", "1")])
            assert(is_err(trap(HttpServer.respond(req, 200, [])), (:http_server, :respond), req))
            assert(is_err(trap(Port.write(req, "x")), (:port, :write), req))
            Port.close(req)
    Port.close(f)
    Port.close(l)
    Global.put(:ok, :ok)
`, withHTTP(h))
	r := newMemReq("get", "/", "")
	h.listener(t).send(r)
	if err := waitDone(t, done); err != nil {
		t.Fatalf("run: %v", err)
	}
	globalIs(t, m, "ok", runtime.Atom("ok"))
	wantResponse(t, r, 204, "", "end")
}

// TestHttpOwnerDeathGives500 — владелец req умер: ответ без respond —
// 500, начатый ответ обрывается, а не заканчивается (§12.12).
func TestHttpOwnerDeathGives500(t *testing.T) {
	h := newMemHTTP()
	_, done := startModuleWith(t, `module Main
fn crash_before() ->
    recv
        (:serve, _req) -> raise(:boom)

fn crash_after() ->
    recv
        (:serve, req) ->
            Ok(()) = HttpServer.respond(req, 200, [])
            Ok(()) = Port.write(req, "partial")
            raise(:boom)

fn hand(l, handler) ->
    (pid, ref) = spawn_watched(handler)
    Ok(()) = Port.request(l)
    req = recv
        (:http_request, _, r, _) -> r
    Ok(()) = Port.give(req, pid)
    send(pid, (:serve, req))
    recv
        (:down, x, _) when x == ref -> ()

fn main() ->
    l = HttpServer.listen(":8080")
    hand(l, crash_before)
    hand(l, crash_after)
    Port.close(l)
`, withHTTP(h))
	l := h.listener(t)
	r1, r2 := newMemReq("get", "/before", ""), newMemReq("get", "/after", "")
	l.send(r1)
	l.send(r2)
	if err := waitDone(t, done); err != nil {
		t.Fatalf("run: %v", err)
	}
	wantResponse(t, r1, 500, "", "abort")
	wantResponse(t, r2, 200, "partial", "abort")
}

// TestHttpGiveRequestToHandler — req отдаётся обработчику Port.give;
// прежний владелец им больше не распоряжается. Владелец req — тот, кто
// владеет listener в момент доставки (§12.12).
func TestHttpGiveRequestToHandler(t *testing.T) {
	h := newMemHTTP()
	m, done := startModuleWith(t, `module Main
fn handler(parent) ->
    recv
        (:serve, req, head) ->
            Ok(()) = HttpServer.respond(req, 200, [("content-type", "text/plain")])
            Ok(()) = Port.write(req, head.path)
            Port.close(req)
            send(parent, :served)

fn acceptor(parent) ->
    recv
        (:http_request, l, req, _) ->
            Ok(()) = HttpServer.respond(req, 202, [])
            Port.close(req)
            Port.close(l)
            send(parent, :accepted)

fn main() ->
    me = self()
    l = HttpServer.listen(":8080")
    Ok(()) = Port.request(l)
    recv
        (:http_request, _, req, head) ->
            pid = spawn(() -> handler(me))
            Global.put(:give, Port.give(req, pid))
            Global.put(:old_owner, trap(HttpServer.respond(req, 200, [])))
            send(pid, (:serve, req, head))
    recv
        :served -> ()
    Ok(()) = Port.request(l)
    a = spawn(() -> acceptor(me))
    Ok(()) = Port.give(l, a)
    _mark = HttpServer.listen(":9090")
    recv
        :accepted -> ()
`, withHTTP(h))
	l := h.listener(t)
	r1 := newMemReq("get", "/hello", "")
	l.send(r1)
	wantResponse(t, r1, 200, "/hello", "end")
	if mark := h.listener(t); mark.addr != ":9090" {
		t.Fatalf("mark addr = %q", mark.addr)
	}
	r2 := newMemReq("get", "/moved", "")
	l.send(r2)
	if err := waitDone(t, done); err != nil {
		t.Fatalf("run: %v", err)
	}
	wantResponse(t, r2, 202, "", "end")
	globalIs(t, m, "give", runtime.Variant("Ok", runtime.Unit))
	if got := m.Scheduler().GlobalGet(runtime.Atom("old_owner")); !strings.Contains(got.Inspect(), ":type_error") {
		t.Fatalf("old owner respond: %s", got.Inspect())
	}
}

// TestHttpBodyReadViaPortRequest — тело запроса читается общим
// Port.request порциями; после :port_eof порт открыт для ответа, и
// следующий запрос снова даёт :port_eof (§12.12).
func TestHttpBodyReadViaPortRequest(t *testing.T) {
	h := newMemHTTP()
	m, done := startModuleWith(t, `module Main
fn read_body(req, acc, n) ->
    Ok(()) = Port.request(req)
    Ok(()) = Port.request(req)
    recv
        (:port_data, p, chunk) when p == req -> read_body(req, Bytes.concat(acc, chunk), n + 1)
        (:port_eof, p) when p == req -> (acc, n)

fn main() ->
    l = HttpServer.listen(":8080")
    Ok(()) = Port.request(l)
    recv
        (:http_request, _, req, _) ->
            (body, n) = read_body(req, b"", 0)
            Global.put(:chunks, n)
            Ok(()) = Port.request(req)
            again = recv
                (:port_eof, p) when p == req -> :eof
            Global.put(:again, again)
            Ok(()) = HttpServer.respond(req, 200, [])
            Ok(()) = Port.write(req, body)
            Port.close(req)
    Port.close(l)
`, withHTTP(h))
	r := newMemReq("post", "/echo", "hello world")
	h.listener(t).send(r)
	if err := waitDone(t, done); err != nil {
		t.Fatalf("run: %v", err)
	}
	globalIs(t, m, "chunks", runtime.Int(3))
	globalIs(t, m, "again", runtime.Atom("eof"))
	wantResponse(t, r, 200, "hello world", "end")
}

// TestHttpClientDisconnect — клиент ушёл: (:port_error, req, :closed) без
// запроса, после него порт закрыт (§12.12).
func TestHttpClientDisconnect(t *testing.T) {
	h := newMemHTTP()
	m, done := startModuleWith(t, `module Main
fn main() ->
    l = HttpServer.listen(":8080")
    Ok(()) = Port.request(l)
    req = recv
        (:http_request, _, r, _) -> r
    reason = recv
        (:port_error, p, why) when p == req -> why
    after 2000 -> :timeout
    Global.put(:reason, reason)
    Global.put(:write, Port.write(req, "x"))
    Global.put(:respond, HttpServer.respond(req, 200, []))
    Global.put(:request, Port.request(req))
    Port.close(req)
    Port.close(l)
`, withHTTP(h))
	r := newMemReq("get", "/", "")
	h.listener(t).send(r)
	waitStarted(t, r)
	r.disconnect()
	if err := waitDone(t, done); err != nil {
		t.Fatalf("run: %v", err)
	}
	closed := runtime.Variant("Error", runtime.Atom("closed"))
	globalIs(t, m, "reason", runtime.Atom("closed"))
	globalIs(t, m, "write", closed)
	globalIs(t, m, "respond", closed)
	globalIs(t, m, "request", closed)
	wantResponse(t, r, 500, "", "end")
}

// TestHttpListenerQuietDoesNotHold — listener без запроса и req без
// запроса и записи не держат ожидание; взведённый listener держит (§15.2).
func TestHttpListenerQuietDoesNotHold(t *testing.T) {
	t.Run("main waits, listener idle", func(t *testing.T) {
		_, done := startModuleWith(t, `module Main
fn main() ->
    _l = HttpServer.listen(":8080")
    recv
        :never -> ()
`, withHTTP(newMemHTTP()))
		err := waitDone(t, done)
		if err == nil || !strings.Contains(err.Error(), "deadlock") {
			t.Fatalf("want deadlock, got %v", err)
		}
	})

	t.Run("main finished, idle owners", func(t *testing.T) {
		h := newMemHTTP()
		_, done := startModuleWith(t, `module Main
fn idle() ->
    recv
        _ -> idle()

fn main() ->
    l = HttpServer.listen(":8080")
    Ok(()) = Port.request(l)
    req = recv
        (:http_request, _, r, _) -> r
    w = spawn(idle)
    Ok(()) = Port.give(req, w)
    Ok(()) = Port.give(l, w)
    :done
`, withHTTP(h))
		l := h.listener(t)
		r1, r2 := newMemReq("get", "/idle", ""), newMemReq("get", "/queued", "")
		l.send(r1)
		l.send(r2)
		if err := waitDone(t, done); err != nil {
			t.Fatalf("run: %v", err)
		}
		wantResponse(t, r1, 500, "", "abort")
		wantResponse(t, r2, 503, "", "reject")
		if l.closed != "abort" {
			t.Fatalf("listener closed by %q, want abort", l.closed)
		}
	})

	t.Run("armed listener holds", func(t *testing.T) {
		h := newMemHTTP()
		m, done := startModuleWith(t, `module Main
fn main() ->
    l = HttpServer.listen(":8080")
    Ok(()) = Port.request(l)
    recv
        (:http_request, _, req, head) ->
            Global.put(:got, head.method)
            Ok(()) = HttpServer.respond(req, 200, [])
            Port.close(req)
    Port.close(l)
`, withHTTP(h))
		l := h.listener(t)
		assertRunning(t, done)
		r := newMemReq("delete", "/x", "")
		l.send(r)
		if err := waitDone(t, done); err != nil {
			t.Fatalf("run: %v", err)
		}
		globalIs(t, m, "got", runtime.Atom("delete"))
		wantResponse(t, r, 200, "", "end")
		if l.closed != "close" {
			t.Fatalf("listener closed by %q, want close", l.closed)
		}
	})
}

// TestHttpListenErrors — ошибка адреса — :port_error на первый запрос,
// после него listener закрыт; VM без сети — :enotsup (§12.12).
func TestHttpListenErrors(t *testing.T) {
	h := newMemHTTP()
	h.failBind = "eaddrinuse"
	m, done := startModuleWith(t, `module Main
fn main() ->
    l = HttpServer.listen(":8080")
    Ok(()) = Port.request(l)
    recv
        (:port_error, p, reason) when p == l -> Global.put(:reason, reason)
    Global.put(:after, Port.request(l))
`, withHTTP(h))
	if err := waitDone(t, done); err != nil {
		t.Fatalf("run: %v", err)
	}
	globalIs(t, m, "reason", runtime.Atom("eaddrinuse"))
	globalIs(t, m, "after", runtime.Variant("Error", runtime.Atom("closed")))

	m, done = startModule(t, `module Main
fn main() ->
    l = HttpServer.listen(":8080")
    Ok(()) = Port.request(l)
    recv
        (:port_error, p, reason) when p == l -> Global.put(:reason, reason)
`, nil)
	if err := waitDone(t, done); err != nil {
		t.Fatalf("run: %v", err)
	}
	globalIs(t, m, "reason", runtime.Atom("enotsup"))
}
