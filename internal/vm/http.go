package vm

import (
	"errors"
	"strings"

	"github.com/it1ro/brig-lang/internal/runtime"
)

// ---- HTTP-сервер как вид порта (§12.12, T-229) ----
//
// Транспорт — реализация за интерфейсом HTTPHub (в cmd/brig — Go
// net/http в своих goroutine); ядро VM о сети не знает (R14). Слушатель —
// потоковый порт: Port.request просит один запрос, ответ ресурса —
// HTTPEvent, из которого run-loop создаёт порт запроса. Порт запроса —
// двусторонний потоковый: тело запроса читает Port.request, тело ответа
// пишет Port.write после HttpServer.respond. Состояние портов — только
// run-loop; команды ресурсу не ждут ОС.

// HTTPHub — реализация вида HttpServer. Listen не ждёт ОС: адрес
// занимается в goroutine ресурса, ошибка — Err в ответ на первый Accept.
type HTTPHub interface {
	Listen(addr string, emit func(HTTPEvent)) HTTPListener
}

// HTTPListener — ресурс слушателя. Методы зовёт run-loop, они не ждут.
//
//   - Accept — отдать один запрос: ровно одно событие (Req или Err).
//     Run-loop не зовёт Accept снова, пока не получил ответ.
//   - Close — Port.close: новые запросы не принимаются, запросы, которые
//     run-loop не начал (HTTPRequest.Start), получают 503, начатые
//     обслуживаются до конца. done — ровно один раз, когда всё кончено.
//   - Abort — порт закрыт иначе (смерть владельца, выход программы): как
//     Close, но начатым запросам даётся ограниченное время (§12.12).
//
// События после Close/Abort run-loop отбрасывает.
type HTTPListener interface {
	Accept()
	Close(done func())
	Abort(done func())
}

// HTTPEvent — ответ слушателя на Accept: новый запрос (Req и Head) или
// ошибка (Err — атом причины: "eaddrinuse", …).
type HTTPEvent struct {
	Req  HTTPRequest
	Head HTTPHead
	Err  string
}

// HTTPHead — то, из чего run-loop строит запись head (§12.12). Method —
// имя атома ("get", …); Headers — имена в нижнем регистре, по имени;
// Host, Port — адрес клиента.
type HTTPHead struct {
	Method  string
	Path    string
	Query   string
	Headers [][2]string
	Host    string
	Port    int
}

// HTTPRequest — ресурс порта запроса. Методы зовёт run-loop, они не ждут.
//
//   - Start — запрос доставлен: ресурс шлёт события через emit. До Start
//     ресурс событий не шлёт; запрос, который не начали, пока слушатель
//     не закрылся, ресурс сам закрывает ответом 503.
//   - Read — одна порция тела: Data, EOF или Err; после EOF каждый Read —
//     снова EOF.
//   - Respond — статус и заголовки ответа; Write — тело ответа (Written
//     или Err); run-loop зовёт Write только после Respond.
//   - Close — Port.close: ответ закончен (без Respond — 500).
//   - Abort — порт закрыт иначе: без Respond — 500, иначе соединение
//     обрывается.
//
// Close и Abort зовутся ровно один раз на начатый запрос; done — ровно
// один раз.
type HTTPRequest interface {
	Stream
	Start(emit func(StreamEvent))
	Respond(status int, headers [][2]string)
	Abort(done func())
}

// SetHTTP подключает реализацию HttpServer. Без неё HttpServer.listen
// открывает порт, который на запрос отвечает :port_error с :enotsup (VM
// без сети).
func (vm *VM) SetHTTP(h HTTPHub) { vm.http = h }

// httpMethods — методы, которые доходят до программы (§12.12).
var httpMethods = []string{"get", "head", "post", "put", "patch", "delete", "options"}

// HTTPMethods — методы запроса, которые реализация отдаёт программе;
// на прочие она отвечает 501 сама.
func HTTPMethods() []string { return append([]string(nil), httpMethods...) }

// listenerStream — слушатель за интерфейсом Stream: Read — Accept.
type listenerStream struct{ l HTTPListener }

func (ls listenerStream) Read()             { ls.l.Accept() }
func (ls listenerStream) Write([]byte)      {}
func (ls listenerStream) Close(done func()) { ls.l.Close(done) }

// httpEvent — ответ слушателя (§12.12): ошибка закрывает порт, запрос
// становится портом запроса у владельца слушателя в момент доставки.
func (s *Scheduler) httpEvent(p *openPort, ev *HTTPEvent) {
	if p == nil || p.stream == nil {
		return
	}
	h := p.h
	lv := runtime.Value{Kind: runtime.KindPort, Port: h}
	if ev.Err != "" {
		s.deliver(h, runtime.Tuple(runtime.Atom("port_error"), lv, runtime.Atom(ev.Err)))
		s.closePort(h)
		return
	}
	owner, ok := s.actors[h.Owner]
	if !ok || ev.Req == nil {
		return
	}
	p.stream.armed = false
	req := s.newRequestPort(owner, ev.Req)
	s.deliver(h, runtime.Tuple(runtime.Atom("http_request"), lv, req, headValue(ev.Head)))
}

// newRequestPort открывает порт запроса с владельцем a: читает тело,
// пишет — только после respond; :port_eof его не закрывает.
func (s *Scheduler) newRequestPort(a *Actor, r HTTPRequest) runtime.Value {
	p := s.addPort(a)
	p.stream = &streamPort{res: r, read: true, duplex: true, req: r}
	h := p.h
	r.Start(func(ev StreamEvent) {
		s.inject.push(injectEvent{port: h, stream: &ev})
	})
	p.close = func() {
		s.closing.Add(1)
		r.Close(s.closing.Done)
	}
	p.abort = func() {
		s.closing.Add(1)
		r.Abort(s.closing.Done)
	}
	return runtime.Value{Kind: runtime.KindPort, Port: h}
}

// headValue — анонимная запись { method, path, query, headers, remote }.
func headValue(h HTTPHead) runtime.Value {
	hs := make([]runtime.Value, len(h.Headers))
	for i, kv := range h.Headers {
		hs[i] = runtime.Tuple(runtime.Str(kv[0]), runtime.Str(kv[1]))
	}
	return runtime.Record("", []runtime.RecordField{
		{Name: "method", Val: runtime.Atom(h.Method)},
		{Name: "path", Val: runtime.Str(h.Path)},
		{Name: "query", Val: runtime.Str(h.Query)},
		{Name: "headers", Val: runtime.List(hs...)},
		{Name: "remote", Val: runtime.Tuple(runtime.Str(h.Host), runtime.Int(int64(h.Port)))},
	})
}

// installHTTP регистрирует HttpServer.listen и HttpServer.respond.
func installHTTP(def func(name string, arity int, fn runtime.NativeFunc)) {
	def("HttpServer.listen", 1, func(c runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		m := c.(*VM)
		addr := args[0]
		if addr.Kind != runtime.KindStr || !validListenAddr(addr.Str) {
			return runtime.Unit, modTypeErr("http_server", "listen", addr)
		}
		s := m.scheduler
		a := s.active
		if a == nil || a.pid < 0 {
			return runtime.Unit, errors.New("internal: HttpServer.listen outside an actor")
		}
		p := s.addPort(a)
		h := p.h
		emit := func(ev HTTPEvent) {
			s.inject.push(injectEvent{port: h, http: &ev})
		}
		var l HTTPListener = noHTTP{emit}
		if m.http != nil {
			l = m.http.Listen(addr.Str, emit)
		}
		p.stream = &streamPort{res: listenerStream{l}, read: true}
		p.close = func() {
			s.closing.Add(1)
			l.Close(s.closing.Done)
		}
		p.abort = func() {
			s.closing.Add(1)
			l.Abort(s.closing.Done)
		}
		return runtime.Value{Kind: runtime.KindPort, Port: h}, nil
	})
	def("HttpServer.respond", 3, func(c runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		s := c.(*VM).scheduler
		v, status := args[0], args[1]
		if !s.ownsPort(v) {
			return runtime.Unit, modTypeErr("http_server", "respond", v)
		}
		if status.Kind != runtime.KindInt || !status.IsSmall || status.SmallInt < 200 || status.SmallInt > 599 {
			return runtime.Unit, modTypeErr("http_server", "respond", status)
		}
		headers, ok := responseHeaders(args[2])
		if !ok {
			return runtime.Unit, modTypeErr("http_server", "respond", args[2])
		}
		if v.Port.Closed {
			return errClosed, nil
		}
		sp := s.ports[v.Port.ID].stream
		if sp == nil || sp.req == nil || sp.responded {
			return runtime.Unit, modTypeErr("http_server", "respond", v)
		}
		code := int(status.SmallInt)
		sp.responded = true
		sp.write = code != 204 && code != 304
		sp.req.Respond(code, headers)
		return okUnit, nil
	})
}

// validListenAddr — host:port, порт 1..65535; host пуст, имя, IPv4 или
// IPv6 в скобках (§12.12). Разрешает имя и занимает адрес реализация.
func validListenAddr(addr string) bool {
	i := strings.LastIndexByte(addr, ':')
	if i < 0 {
		return false
	}
	host, port := addr[:i], addr[i+1:]
	if len(port) == 0 || len(port) > 5 {
		return false
	}
	n := 0
	for _, c := range port {
		if c < '0' || c > '9' {
			return false
		}
		n = n*10 + int(c-'0')
	}
	if n < 1 || n > 65535 {
		return false
	}
	if strings.HasPrefix(host, "[") {
		inner, ok := strings.CutSuffix(host[1:], "]")
		return ok && inner != "" && !strings.ContainsAny(inner, "[]/ \t\r\n")
	}
	return !strings.ContainsAny(host, ":[]/ \t\r\n")
}

// responseHeaders — List<(Str, Str)>: имя — token (RFC 9110), значение
// без CR, LF и NUL (§12.12).
func responseHeaders(v runtime.Value) ([][2]string, bool) {
	if v.Kind != runtime.KindList {
		return nil, false
	}
	out := make([][2]string, 0, v.Len())
	for _, e := range v.Elems() {
		if e.Kind != runtime.KindTuple || len(e.Tuple) != 2 {
			return nil, false
		}
		name, val := e.Tuple[0], e.Tuple[1]
		if name.Kind != runtime.KindStr || val.Kind != runtime.KindStr ||
			!isToken(name.Str) || strings.ContainsAny(val.Str, "\r\n\x00") {
			return nil, false
		}
		out = append(out, [2]string{name.Str, val.Str})
	}
	return out, true
}

// isToken — непустой token RFC 9110: буквы, цифры и !#$%&'*+-.^_`|~.
func isToken(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z') || ('0' <= c && c <= '9') {
			continue
		}
		if !strings.ContainsRune("!#$%&'*+-.^_`|~", rune(c)) {
			return false
		}
	}
	return true
}

// noHTTP — слушатель без сети: любой запрос — :enotsup.
type noHTTP struct{ emit func(HTTPEvent) }

func (n noHTTP) Accept()           { n.emit(HTTPEvent{Err: "enotsup"}) }
func (n noHTTP) Close(done func()) { done() }
func (n noHTTP) Abort(done func()) { done() }
