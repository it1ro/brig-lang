//go:build unix

package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/it1ro/brig-lang/internal/vm"
)

// freeAddr — свободный локальный адрес: ОС выбирает порт, тест его
// отпускает и отдаёт программе через Sys.args.
func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

// waitServing опрашивает url, пока сервер не ответит (без sleep-гонок:
// готовность — первый ответ, предел — timeout).
func waitServing(t *testing.T, c *http.Client, url string, exited <-chan error) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := c.Get(url)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			return
		}
		select {
		case err := <-exited:
			t.Fatalf("brig exited before serving: %v", err)
		case <-time.After(20 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatalf("server at %s not ready within 10s: %v", url, err)
		}
	}
}

// TestHttpServerE2E — examples/http_hello.brig в `brig <file>`: GET / →
// 200, application/json, {"status":"ok"}; параллельные запросы; HEAD без
// тела; неизвестный метод — 501 до программы; SIGTERM — выход 0 (§12.12).
func TestHttpServerE2E(t *testing.T) {
	bin := buildBrig(t)
	addr := freeAddr(t)
	cmd := exec.Command(bin, filepath.Join(findModuleRoot(t), "examples", "http_hello.brig"), addr)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	exited := make(chan error, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			out.WriteString(sc.Text() + "\n")
		}
		exited <- cmd.Wait()
	}()
	defer func() { _ = cmd.Process.Kill() }()

	c := &http.Client{Timeout: 5 * time.Second}
	base := "http://" + addr
	waitServing(t, c, base+"/", exited)

	resp, err := c.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/json" || string(body) != `{"status":"ok"}` {
		t.Fatalf("GET / = %d %q %q", resp.StatusCode, resp.Header.Get("Content-Type"), body)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := range 32 {
		wg.Go(func() {
			resp, err := c.Get(fmt.Sprintf("%s/p/%d", base, i))
			if err != nil {
				errs <- err
				return
			}
			b, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode != 200 || string(b) != `{"status":"ok"}` {
				errs <- fmt.Errorf("GET /p/%d = %d %q", i, resp.StatusCode, b)
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	resp, err = c.Head(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 || resp.ContentLength != int64(len(`{"status":"ok"}`)) {
		t.Fatalf("HEAD / = %d, length %d", resp.StatusCode, resp.ContentLength)
	}

	req, _ := http.NewRequest("TRACE", base+"/", nil)
	resp, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("TRACE / = %d, want 501", resp.StatusCode)
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-exited:
		if err != nil {
			t.Fatalf("exit after SIGTERM: %v\nstderr:\n%s", err, stderr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("brig did not exit within 10s after SIGTERM")
	}
	if got := out.String(); got != "listening on "+addr+"\n" {
		t.Fatalf("stdout = %q", got)
	}
}

// hubServer — слушатель osHTTP без VM: события слушателя и запросов
// приходят в каналы теста.
type hubServer struct {
	t    *testing.T
	l    *httpListener
	evs  chan vm.HTTPEvent
	base string
}

func startHub(t *testing.T, lim httpLimits) *hubServer {
	t.Helper()
	evs := make(chan vm.HTTPEvent, 16)
	l := osHTTP{lim: lim}.Listen("127.0.0.1:0", func(ev vm.HTTPEvent) { evs <- ev }).(*httpListener)
	<-l.ready
	if l.addr == nil {
		t.Fatalf("bind failed: %s", l.bindErr)
	}
	h := &hubServer{t: t, l: l, evs: evs, base: "http://" + l.addr.String()}
	t.Cleanup(func() {
		done := make(chan struct{})
		l.Abort(func() { close(done) })
		<-done
	})
	return h
}

// accept — Accept и одно событие слушателя.
func (h *hubServer) accept() vm.HTTPEvent {
	h.t.Helper()
	h.l.Accept()
	select {
	case ev := <-h.evs:
		return ev
	case <-time.After(5 * time.Second):
		h.t.Fatal("no listener event within 5s")
		return vm.HTTPEvent{}
	}
}

// started — запрос начат, его события — в канал.
func started(r vm.HTTPRequest) chan vm.StreamEvent {
	ch := make(chan vm.StreamEvent, 16)
	r.Start(func(ev vm.StreamEvent) { ch <- ev })
	return ch
}

func next(t *testing.T, ch chan vm.StreamEvent) vm.StreamEvent {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(5 * time.Second):
		t.Fatal("no request event within 5s")
		return vm.StreamEvent{}
	}
}

// closed ждёт done от Close/Abort.
func closed(t *testing.T, f func(func())) {
	t.Helper()
	done := make(chan struct{})
	f(func() { close(done) })
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("done not called within 5s")
	}
}

// clientResult — ответ клиента из горутины.
type clientResult struct {
	status int
	header http.Header
	body   string
	err    error
}

func (h *hubServer) do(req *http.Request) <-chan clientResult {
	ch := make(chan clientResult, 1)
	go func() {
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			ch <- clientResult{err: err}
			return
		}
		b, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		ch <- clientResult{status: resp.StatusCode, header: resp.Header, body: string(b), err: err}
	}()
	return ch
}

func (h *hubServer) get(path string) <-chan clientResult {
	req, _ := http.NewRequest("GET", h.base+path, nil)
	req.Header.Set("X-B", "2")
	req.Header.Add("X-A", "1")
	req.Header.Add("X-A", "0")
	return h.do(req)
}

func result(t *testing.T, ch <-chan clientResult) clientResult {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("client got no response within 5s")
		return clientResult{}
	}
}

// TestHttpTransportHead — head: метод атомом, путь и query как есть,
// заголовки в нижнем регистре по имени, host добавлен; Content-Type не
// угадывается; Close без respond — 500 (§12.12).
func TestHttpTransportHead(t *testing.T) {
	h := startHub(t, defaultHTTPLimits)
	res := h.get("/a%2Fb?q=1&r")
	ev := h.accept()
	if ev.Err != "" || ev.Req == nil {
		t.Fatalf("event = %+v", ev)
	}
	hd := ev.Head
	if hd.Method != "get" || hd.Path != "/a%2Fb" || hd.Query != "q=1&r" || hd.Host != "127.0.0.1" || hd.Port == 0 {
		t.Fatalf("head = %+v", hd)
	}
	var names []string
	for _, kv := range hd.Headers {
		names = append(names, kv[0]+"="+kv[1])
	}
	got := strings.Join(names, " ")
	if !strings.HasPrefix(got, "accept-encoding=gzip host="+h.l.addr.String()+" user-agent=") || !strings.HasSuffix(got, "x-a=1 x-a=0 x-b=2") {
		t.Fatalf("headers = %s", got)
	}
	evs := started(ev.Req)
	ev.Req.Respond(200, [][2]string{{"x-reply", "yes"}})
	ev.Req.Write([]byte("<html><body>hi</body></html>"))
	if e := next(t, evs); e.Written != 28 {
		t.Fatalf("write event = %+v", e)
	}
	closed(t, ev.Req.Close)
	r := result(t, res)
	if r.err != nil || r.status != 200 || r.body != "<html><body>hi</body></html>" || r.header.Get("X-Reply") != "yes" {
		t.Fatalf("response = %+v", r)
	}
	if ct, ok := r.header["Content-Type"]; ok {
		t.Fatalf("Content-Type sniffed: %v", ct)
	}

	res = h.get("/none")
	ev = h.accept()
	started(ev.Req)
	closed(t, ev.Req.Close)
	if r := result(t, res); r.status != 500 || r.body != "" {
		t.Fatalf("close without respond = %+v", r)
	}
}

// TestHttpTransportLimits — сверх лимита запросов — 503, Content-Length
// больше лимита тела — 413 до Brig, поток больше лимита — :too_large и
// 413; неизвестный метод — 501 (§12.12).
func TestHttpTransportLimits(t *testing.T) {
	lim := defaultHTTPLimits
	lim.requests = 1
	lim.body = 8
	h := startHub(t, lim)

	first := h.get("/first")
	ev := h.accept()
	evs := started(ev.Req)
	if r := result(t, h.get("/second")); r.status != 503 {
		t.Fatalf("over request limit = %+v", r)
	}
	ev.Req.Respond(204, nil)
	closed(t, ev.Req.Close)
	if r := result(t, first); r.status != 204 {
		t.Fatalf("first = %+v", r)
	}
	select {
	case e := <-evs:
		t.Fatalf("unexpected event %+v", e)
	default:
	}

	req, _ := http.NewRequest("POST", h.base+"/big", strings.NewReader("0123456789"))
	if r := result(t, h.do(req)); r.status != 413 {
		t.Fatalf("Content-Length over limit = %+v", r)
	}

	pr, pw := io.Pipe()
	go func() {
		_, _ = pw.Write([]byte("0123456789abcdef"))
		_ = pw.Close()
	}()
	req, _ = http.NewRequest("POST", h.base+"/stream", pr)
	res := h.do(req)
	ev = h.accept()
	evs = started(ev.Req)
	var reason string
	for reason == "" {
		ev.Req.Read()
		e := next(t, evs)
		switch {
		case e.Err != "":
			reason = e.Err
		case e.EOF:
			t.Fatal("body over limit ended with EOF")
		}
	}
	if reason != "too_large" {
		t.Fatalf("reason = %q", reason)
	}
	closed(t, ev.Req.Close)
	if r := result(t, res); r.status != 413 {
		t.Fatalf("stream over limit = %+v", r)
	}

	req, _ = http.NewRequest("TRACE", h.base+"/", nil)
	if r := result(t, h.do(req)); r.status != 501 {
		t.Fatalf("TRACE = %+v", r)
	}
}

// TestHttpTransportBodyAndAbort — тело читается порциями до EOF, повтор —
// снова EOF; Abort начатого ответа обрывает соединение; Abort без
// respond — 500 (§12.12).
func TestHttpTransportBodyAndAbort(t *testing.T) {
	h := startHub(t, defaultHTTPLimits)
	req, _ := http.NewRequest("PUT", h.base+"/echo", strings.NewReader("hello"))
	res := h.do(req)
	ev := h.accept()
	if ev.Head.Method != "put" {
		t.Fatalf("method = %q", ev.Head.Method)
	}
	evs := started(ev.Req)
	var body []byte
	for {
		ev.Req.Read()
		e := next(t, evs)
		if e.EOF {
			break
		}
		if e.Err != "" {
			t.Fatalf("read error %s", e.Err)
		}
		body = append(body, e.Data...)
	}
	ev.Req.Read()
	if e := next(t, evs); !e.EOF {
		t.Fatalf("read after EOF = %+v", e)
	}
	if string(body) != "hello" {
		t.Fatalf("body = %q", body)
	}
	ev.Req.Respond(200, [][2]string{{"content-length", "100"}})
	ev.Req.Write(body)
	next(t, evs)
	closed(t, ev.Req.Abort)
	if r := result(t, res); r.err == nil {
		t.Fatalf("aborted response reached the client: %+v", r)
	}

	res = h.get("/abort")
	ev = h.accept()
	started(ev.Req)
	closed(t, ev.Req.Abort)
	if r := result(t, res); r.status != 500 {
		t.Fatalf("abort without respond = %+v", r)
	}
}

// TestHttpTransportClose — клиент ушёл — :port_error :closed; Close
// слушателя: не начатые запросы — 503, начатые дослуживаются (§12.12).
func TestHttpTransportClose(t *testing.T) {
	h := startHub(t, defaultHTTPLimits)

	conn, err := net.Dial("tcp", h.l.addr.String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintf(conn, "GET /gone HTTP/1.1\r\nHost: x\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	ev := h.accept()
	evs := started(ev.Req)
	_ = conn.Close()
	if e := next(t, evs); e.Err != "closed" {
		t.Fatalf("disconnect event = %+v", e)
	}
	closed(t, ev.Req.Close)

	active := h.get("/active")
	ev = h.accept()
	started(ev.Req)
	queued := h.get("/queued")
	// Запрос /queued должен дойти до очереди транспорта раньше Close.
	deadline := time.Now().Add(5 * time.Second)
	for {
		h.l.mu.Lock()
		n := len(h.l.queue)
		h.l.mu.Unlock()
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("request not queued within 5s")
		}
		time.Sleep(time.Millisecond)
	}
	listenerDone := make(chan struct{})
	h.l.Close(func() { close(listenerDone) })
	if r := result(t, queued); r.status != 503 {
		t.Fatalf("queued on close = %+v", r)
	}
	select {
	case <-listenerDone:
		t.Fatal("listener finished before the active request")
	default:
	}
	ev.Req.Respond(200, nil)
	ev.Req.Write([]byte("late"))
	closed(t, ev.Req.Close)
	if r := result(t, active); r.status != 200 || r.body != "late" {
		t.Fatalf("active on close = %+v", r)
	}
	select {
	case <-listenerDone:
	case <-time.After(5 * time.Second):
		t.Fatal("listener not closed within 5s")
	}
	if _, err := http.Get(h.base + "/"); err == nil {
		t.Fatal("closed listener still accepts")
	}
}

// TestHttpTransportBindError — занятый адрес — Err на Accept (§12.12).
func TestHttpTransportBindError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	evs := make(chan vm.HTTPEvent, 1)
	l := newOsHTTP().Listen(ln.Addr().String(), func(ev vm.HTTPEvent) { evs <- ev })
	l.Accept()
	select {
	case ev := <-evs:
		if ev.Err != "eaddrinuse" {
			t.Fatalf("event = %+v", ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no bind error within 5s")
	}
	closed(t, l.Close)
}
