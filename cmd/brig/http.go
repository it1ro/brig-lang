package main

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/it1ro/brig-lang/internal/vm"
)

// osHTTP — порт HttpServer (§12.12) на net/http. Ядро VM о сети не знает
// (R14): cmd/brig подключает эту реализацию к машине (vm.SetHTTP).
// Соединения, keep-alive и разбор HTTP/1.1 живут в goroutine net/http;
// ResponseWriter и Request трогает только горутина своего обработчика,
// команды run-loop приходят ей через очередь без предела.
type osHTTP struct{ lim httpLimits }

// httpLimits — безопасные дефолты транспорта (§12.12, таблица лимитов).
type httpLimits struct {
	requests      int           // запросов одновременно: в очереди до Brig и в обработке
	headerBytes   int           // заголовки запроса
	body          int64         // тело запроса
	headerTimeout time.Duration // заголовки должны прийти за это время
	ioTimeout     time.Duration // одна порция тела, одна запись в соединение
	idleTimeout   time.Duration // keep-alive без запросов
	drain         time.Duration // дренаж при закрытии слушателя не через Port.close
	chunk         int           // порция тела для :port_data
}

var defaultHTTPLimits = httpLimits{
	requests:      1024,
	headerBytes:   64 << 10,
	body:          8 << 20,
	headerTimeout: 10 * time.Second,
	ioTimeout:     60 * time.Second,
	idleTimeout:   60 * time.Second,
	drain:         5 * time.Second,
	chunk:         64 << 10,
}

func newOsHTTP() osHTTP { return osHTTP{lim: defaultHTTPLimits} }

// Listen не ждёт ОС: адрес занимает goroutine слушателя, ошибка — Err в
// ответ на Accept (§12.12).
func (h osHTTP) Listen(addr string, emit func(vm.HTTPEvent)) vm.HTTPListener {
	l := &httpListener{
		lim:   h.lim,
		emit:  emit,
		slots: make(chan struct{}, h.lim.requests),
		shut:  make(chan struct{}),
		kill:  make(chan struct{}),
		ready: make(chan struct{}),
	}
	p := new(http.Protocols)
	p.SetHTTP1(true)
	l.srv = &http.Server{
		Handler:           l,
		Protocols:         p,
		ReadHeaderTimeout: h.lim.headerTimeout,
		IdleTimeout:       h.lim.idleTimeout,
		MaxHeaderBytes:    h.lim.headerBytes,
		ErrorLog:          log.New(io.Discard, "", 0),
	}
	go l.bind(addr)
	return l
}

// httpListener — ресурс слушателя: очередь запросов до Brig, лимит
// одновременных запросов, закрытие.
type httpListener struct {
	lim   httpLimits
	emit  func(vm.HTTPEvent)
	srv   *http.Server
	slots chan struct{} // семафор одновременных запросов
	shut  chan struct{} // закрыт Close/Abort: новые не принимаются, не начатые — 503
	kill  chan struct{} // закрыт, когда соединения закрыты принудительно
	ready chan struct{} // закрыт после попытки занять адрес

	mu      sync.Mutex
	addr    net.Addr   // занятый адрес; nil — не занят
	bindErr string     // не пусто — адрес занять не удалось
	want    bool       // Accept ждёт запроса
	closed  bool       // вызван Close или Abort
	queue   []*httpReq // запросы, которых Brig ещё не просил
}

func (l *httpListener) bind(addr string) {
	defer close(l.ready)
	ln, err := net.Listen("tcp", addr)
	l.mu.Lock()
	if err != nil {
		l.bindErr = bindReason(err)
		if l.want {
			l.want = false
			l.emit(vm.HTTPEvent{Err: l.bindErr})
		}
		l.mu.Unlock()
		return
	}
	if l.closed {
		l.mu.Unlock()
		_ = ln.Close()
		return
	}
	l.addr = ln.Addr()
	l.mu.Unlock()
	// Serve после Shutdown сразу возвращается и закрывает ln.
	go func() { _ = l.srv.Serve(ln) }()
}

// bindReason — атом причины :port_error слушателя (§12.12).
func bindReason(err error) string {
	var dns *net.DNSError
	var ae *net.AddrError
	switch {
	case errors.Is(err, syscall.EADDRINUSE):
		return "eaddrinuse"
	case errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM):
		return "eacces"
	case errors.Is(err, syscall.EADDRNOTAVAIL), errors.As(err, &dns), errors.As(err, &ae):
		return "eaddrnotavail"
	}
	return "eio"
}

// Accept — один запрос из очереди, иначе первый пришедший (§12.12).
func (l *httpListener) Accept() {
	l.mu.Lock()
	defer l.mu.Unlock()
	switch {
	case l.bindErr != "":
		l.emit(vm.HTTPEvent{Err: l.bindErr})
	case len(l.queue) > 0:
		q := l.queue[0]
		l.queue = l.queue[1:]
		l.emit(vm.HTTPEvent{Req: q, Head: q.head})
	default:
		l.want = true
	}
}

// offer ставит запрос в очередь или сразу отдаёт Brig; false — слушатель
// закрыт.
func (l *httpListener) offer(q *httpReq) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	switch {
	case l.closed:
		return false
	case l.want:
		l.want = false
		l.emit(vm.HTTPEvent{Req: q, Head: q.head})
	default:
		l.queue = append(l.queue, q)
	}
	return true
}

// withdraw убирает из очереди запрос, клиент которого ушёл; false —
// запрос уже отдан Brig.
func (l *httpListener) withdraw(q *httpReq) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	i := slices.Index(l.queue, q)
	if i < 0 {
		return false
	}
	l.queue = slices.Delete(l.queue, i, i+1)
	return true
}

// stop — новые запросы не принимаются; не начатые получат 503 (их
// горутины видят shut). false — уже закрыт.
func (l *httpListener) stop() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return false
	}
	l.closed = true
	l.want = false
	l.queue = nil
	close(l.shut)
	return true
}

// Close — Port.close: начатые запросы обслуживаются до конца.
func (l *httpListener) Close(done func()) {
	if !l.stop() {
		done()
		return
	}
	go func() {
		_ = l.srv.Shutdown(context.Background())
		done()
	}()
}

// Abort — слушатель закрыт не через Port.close: начатым запросам —
// не больше lim.drain, затем соединения закрываются (§12.12).
func (l *httpListener) Abort(done func()) {
	if !l.stop() {
		done()
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), l.lim.drain)
		defer cancel()
		if err := l.srv.Shutdown(ctx); err != nil {
			_ = l.srv.Close()
			close(l.kill)
		}
		done()
	}()
}

// ServeHTTP — горутина запроса: отказы транспорта до Brig, затем очередь
// и исполнение команд run-loop.
func (l *httpListener) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	// Дедлайн записи прошлого запроса на этом соединении больше не нужен.
	_ = rc.SetWriteDeadline(time.Time{})
	method, ok := httpMethod(r.Method)
	if !ok {
		l.plain(w, rc, http.StatusNotImplemented)
		return
	}
	head, ok := requestHead(r, method)
	if !ok {
		l.plain(w, rc, http.StatusBadRequest)
		return
	}
	if r.ContentLength > l.lim.body {
		l.plain(w, rc, http.StatusRequestEntityTooLarge)
		return
	}
	select {
	case l.slots <- struct{}{}:
		defer func() { <-l.slots }()
	default:
		l.plain(w, rc, http.StatusServiceUnavailable)
		return
	}
	q := &httpReq{
		l: l, w: w, r: r, rc: rc, head: head,
		body: http.MaxBytesReader(w, r.Body, l.lim.body),
		wake: make(chan struct{}, 1),
	}
	defer q.exit()
	if !l.offer(q) {
		l.plain(w, rc, http.StatusServiceUnavailable)
		return
	}
	q.serve()
}

// plain — ответ транспорта без тела.
func (l *httpListener) plain(w http.ResponseWriter, rc *http.ResponseController, code int) {
	_ = rc.SetWriteDeadline(time.Now().Add(l.lim.ioTimeout))
	w.WriteHeader(code)
}

// httpMethod — атом метода (§12.12); прочие методы — 501.
func httpMethod(m string) (string, bool) {
	lower := strings.ToLower(m)
	if strings.ToUpper(lower) != m || !slices.Contains(vm.HTTPMethods(), lower) {
		return "", false
	}
	return lower, true
}

// requestHead — head (§12.12): путь и query без декодирования, заголовки
// в нижнем регистре по имени, host возвращён; false — не UTF-8.
func requestHead(r *http.Request, method string) (vm.HTTPHead, bool) {
	h := vm.HTTPHead{Method: method, Path: r.URL.EscapedPath(), Query: r.URL.RawQuery}
	h.Headers = append(h.Headers, [2]string{"host", r.Host})
	for name, vals := range r.Header {
		lower := strings.ToLower(name)
		for _, v := range vals {
			h.Headers = append(h.Headers, [2]string{lower, v})
		}
	}
	slices.SortStableFunc(h.Headers, func(a, b [2]string) int { return strings.Compare(a[0], b[0]) })
	for _, kv := range h.Headers {
		if !utf8.ValidString(kv[0]) || !utf8.ValidString(kv[1]) {
			return h, false
		}
	}
	if !utf8.ValidString(h.Path) || !utf8.ValidString(h.Query) {
		return h, false
	}
	host, port, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		h.Host = host
		h.Port, _ = strconv.Atoi(port)
	} else {
		h.Host = r.RemoteAddr
	}
	return h, true
}

// httpCmd — команда run-loop горутине запроса.
type httpCmd struct {
	op      int
	data    []byte
	status  int
	headers [][2]string
	done    func()
}

const (
	cmdRead = iota
	cmdRespond
	cmdWrite
	cmdClose
	cmdAbort
)

// httpReq — ресурс порта запроса. Команды исполняет горутина обработчика
// net/http (serve) по порядку (G5); run-loop их только ставит в очередь.
type httpReq struct {
	l    *httpListener
	w    http.ResponseWriter
	r    *http.Request
	rc   *http.ResponseController
	head vm.HTTPHead
	body io.Reader
	wake chan struct{}

	mu      sync.Mutex
	emit    func(vm.StreamEvent)
	started bool
	exited  bool
	cmds    []httpCmd
	done    func() // от Close/Abort: зовётся, когда горутина вышла

	// Дальше — только горутина обработчика.
	sent     bool   // Brig задал статус (HttpServer.respond)
	answered bool   // ответил транспорт (413, 408, 400)
	fail     string // причина отданного :port_error; порт закрыт
	eof      bool   // тело прочитано до конца
}

func (q *httpReq) Start(emit func(vm.StreamEvent)) {
	q.mu.Lock()
	q.emit, q.started = emit, true
	q.mu.Unlock()
	q.signal()
}

func (q *httpReq) Read() { q.push(httpCmd{op: cmdRead}) }

func (q *httpReq) Respond(status int, headers [][2]string) {
	q.push(httpCmd{op: cmdRespond, status: status, headers: headers})
}

func (q *httpReq) Write(b []byte)    { q.push(httpCmd{op: cmdWrite, data: b}) }
func (q *httpReq) Close(done func()) { q.push(httpCmd{op: cmdClose, done: done}) }
func (q *httpReq) Abort(done func()) { q.push(httpCmd{op: cmdAbort, done: done}) }

func (q *httpReq) signal() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// push ставит команду; горутина уже вышла — done зовётся сразу.
func (q *httpReq) push(c httpCmd) {
	q.mu.Lock()
	if q.exited {
		q.mu.Unlock()
		if c.done != nil {
			c.done()
		}
		return
	}
	q.cmds = append(q.cmds, c)
	q.mu.Unlock()
	q.signal()
}

func (q *httpReq) isStarted() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.started
}

// exit — горутина запроса закончилась (в том числе паникой
// ErrAbortHandler): done и команды, пришедшие без исполнения, завершены.
func (q *httpReq) exit() {
	q.mu.Lock()
	q.exited = true
	dones := []func(){q.done}
	for _, c := range q.cmds {
		dones = append(dones, c.done)
	}
	q.cmds = nil
	q.mu.Unlock()
	for _, d := range dones {
		if d != nil {
			d()
		}
	}
}

// serve исполняет команды, пока run-loop не закроет порт (§12.12).
// Клиент ушёл — :port_error :closed (запрос из очереди просто снимается);
// слушатель закрыт, а запрос не начат — 503.
func (q *httpReq) serve() {
	gone, shut := q.r.Context().Done(), q.l.shut
	disconnected := false
	for {
		q.mu.Lock()
		started, cmds := q.started, q.cmds
		q.cmds = nil
		q.mu.Unlock()
		if started && disconnected {
			q.failWith("closed")
		}
		for _, c := range cmds {
			if q.exec(c) {
				return
			}
		}
		select {
		case <-q.wake:
		case <-gone:
			gone, disconnected = nil, true
			if !started && q.l.withdraw(q) {
				return
			}
			q.signal()
		case <-shut:
			shut = nil
			if !q.isStarted() {
				if !disconnected {
					q.l.plain(q.w, q.rc, http.StatusServiceUnavailable)
				}
				return
			}
		case <-q.l.kill:
			return
		}
	}
}

// exec исполняет команду; true — порт закрыт, горутина заканчивается.
func (q *httpReq) exec(c httpCmd) bool {
	switch c.op {
	case cmdRead:
		q.read()
	case cmdRespond:
		q.respond(c.status, c.headers)
	case cmdWrite:
		q.write(c.data)
	default:
		q.mu.Lock()
		q.done = c.done
		q.mu.Unlock()
		q.finish(c.op == cmdAbort)
		return true
	}
	return false
}

// failWith отдаёт :port_error один раз; ответ, который ещё можно дать,
// даёт транспорт (§12.12).
func (q *httpReq) failWith(reason string) {
	if q.fail != "" {
		return
	}
	q.fail = reason
	if !q.sent {
		code := map[string]int{
			"too_large": http.StatusRequestEntityTooLarge,
			"timeout":   http.StatusRequestTimeout,
			"eio":       http.StatusBadRequest,
		}[reason]
		if code != 0 {
			q.l.plain(q.w, q.rc, code)
			q.answered = true
		}
	}
	q.emit(vm.StreamEvent{Err: reason})
}

// read — одна порция тела: Data, EOF (и снова EOF) или Err.
func (q *httpReq) read() {
	if q.fail != "" {
		return
	}
	if q.eof {
		q.emit(vm.StreamEvent{EOF: true})
		return
	}
	hasBody := q.r.ContentLength != 0
	buf := make([]byte, q.l.lim.chunk)
	for range 100 {
		if hasBody {
			_ = q.rc.SetReadDeadline(time.Now().Add(q.l.lim.ioTimeout))
		}
		n, err := q.body.Read(buf)
		if hasBody {
			_ = q.rc.SetReadDeadline(time.Time{})
		}
		switch {
		case n > 0:
			q.eof = err == io.EOF
			q.emit(vm.StreamEvent{Data: buf[:n]})
			return
		case err == io.EOF:
			q.eof = true
			q.emit(vm.StreamEvent{EOF: true})
			return
		case err != nil:
			q.failWith(q.reason(err))
			return
		}
	}
	q.failWith("eio")
}

// respond — статус и заголовки. Content-Type не угадывается: не задан —
// его нет в ответе (§12.12).
func (q *httpReq) respond(status int, headers [][2]string) {
	if q.fail != "" {
		return
	}
	h := q.w.Header()
	for _, kv := range headers {
		h.Add(kv[0], kv[1])
	}
	if _, ok := h["Content-Type"]; !ok {
		h["Content-Type"] = nil
	}
	q.w.WriteHeader(status)
	q.sent = true
}

func (q *httpReq) write(b []byte) {
	if q.fail != "" {
		return
	}
	_ = q.rc.SetWriteDeadline(time.Now().Add(q.l.lim.ioTimeout))
	if _, err := q.w.Write(b); err != nil {
		q.failWith(q.reason(err))
		return
	}
	q.emit(vm.StreamEvent{Written: len(b)})
}

// finish — конец ответа. Без respond — 500; начатый ответ после отказа
// или при закрытии не через Port.close обрывается (§12.12).
func (q *httpReq) finish(abort bool) {
	switch {
	case q.answered || q.fail == "closed" && !q.sent:
	case !q.sent:
		q.l.plain(q.w, q.rc, http.StatusInternalServerError)
	case abort || q.fail != "":
		panic(http.ErrAbortHandler)
	default:
		// Дописать буфер net/http уже после выхода обработчика.
		_ = q.rc.SetWriteDeadline(time.Now().Add(q.l.lim.ioTimeout))
	}
}

// reason — атом причины :port_error запроса (§12.12).
func (q *httpReq) reason(err error) string {
	var mbe *http.MaxBytesError
	var ne net.Error
	switch {
	case errors.As(err, &mbe):
		return "too_large"
	case errors.Is(err, os.ErrDeadlineExceeded), errors.As(err, &ne) && ne.Timeout():
		return "timeout"
	case q.r.Context().Err() != nil, errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, net.ErrClosed),
		errors.Is(err, syscall.ECONNRESET), errors.Is(err, syscall.EPIPE):
		return "closed"
	}
	return "eio"
}
