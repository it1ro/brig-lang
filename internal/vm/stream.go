package vm

import (
	"errors"

	"github.com/it1ro/brig-lang/internal/runtime"
)

// ---- потоковые порты (§12.12, T-228) ----
//
// Протокол общий для всех видов: pull (Port.request — одно событие на
// запрос), запись с порогом (Port.write — Error(:busy), затем одно
// :port_ready), передача владения (Port.give). Вид порта (File, позже
// Proc, Stdin, Tcp) только открывает ресурс за интерфейсом Stream.
// Состояние протокола — флаг запроса и счётчик недописанного — ведёт
// только run-loop; ресурс сообщает о себе событиями inject-очереди.

// streamChunk — наибольшая порция чтения; streamHWM — порог буфера
// записи, с которого Port.write отвечает Error(:busy) (§12.12).
const (
	streamChunk = 64 << 10
	streamHWM   = 64 << 10
)

// StreamEvent — событие ресурса потокового порта. Ровно одно из: Data
// (порция чтения, непустая), EOF, Err (атом причины: "enoent", …),
// Written (столько байт из Write записано — служебное, владельцу не
// доставляется).
type StreamEvent struct {
	Data    []byte
	EOF     bool
	Err     string
	Written int
}

// Stream — ресурс потокового порта. Методы зовёт run-loop, и они не ждут
// ОС: работа идёт в goroutine ресурса, результат — через emit, в порядке
// возникновения (G5).
//
//   - Read — прочитать одну порцию: ровно одно событие Data, EOF или Err.
//     Run-loop не зовёт Read снова, пока не получил ответ.
//   - Write — записать b в порядке вызовов; после записи — Written(len(b)),
//     ошибка — Err. b принадлежит ресурсу.
//   - Close — дописать принятое и освободить ресурс; done зовётся ровно
//     один раз, когда ресурс закрыт. События после Close run-loop
//     отбрасывает.
type Stream interface {
	Read()
	Write(b []byte)
	Close(done func())
}

// FileHub — реализация вида File (§12.12): mode — "read", "write" или
// "append". Open не ждёт ОС: файл открывается в goroutine ресурса,
// ошибка открытия — Err в ответ на первый Read или Write.
type FileHub interface {
	Open(path, mode string, emit func(StreamEvent)) Stream
}

// SetFiles подключает реализацию File. Без неё File.open открывает порт,
// который на любой запрос отвечает :port_error с причиной :enotsup (VM без
// файловой системы).
func (vm *VM) SetFiles(h FileHub) { vm.files = h }

// streamPort — состояние протокола потокового порта.
type streamPort struct {
	res          Stream
	read, write  bool // что порт умеет сейчас
	armed        bool // Port.request ждёт ответа
	pending      int  // байт принято Port.write и ещё не записано
	promiseReady bool // после Error(:busy) обещан :port_ready
	// duplex — двусторонний порт: :port_eof его не закрывает, запись
	// после конца чтения возможна (§12.12, T-229).
	duplex bool
	// req — ресурс порта запроса HTTP (nil — порт другого вида);
	// responded — HttpServer.respond уже был, write разрешён по статусу.
	req       HTTPRequest
	responded bool
}

// newStreamPort открывает потоковый порт с владельцем a; open получает
// функцию, которой ресурс отдаёт события.
func (s *Scheduler) newStreamPort(a *Actor, read, write bool, open func(emit func(StreamEvent)) Stream) runtime.Value {
	p := s.addPort(a)
	sp := &streamPort{read: read, write: write}
	p.stream = sp
	h := p.h
	sp.res = open(func(ev StreamEvent) {
		s.inject.push(injectEvent{port: h, stream: &ev})
	})
	p.close = func() {
		s.closing.Add(1)
		sp.res.Close(s.closing.Done)
	}
	return runtime.Value{Kind: runtime.KindPort, Port: h}
}

// streamEvent превращает событие ресурса в сообщение владельцу (§12.12).
// :port_error закрывает порт; :port_eof — только порт, который лишь
// читает (двусторонний остаётся открытым для записи, T-229).
func (s *Scheduler) streamEvent(p *openPort, ev *StreamEvent) {
	if p == nil || p.stream == nil {
		return
	}
	sp, h := p.stream, p.h
	pv := runtime.Value{Kind: runtime.KindPort, Port: h}
	switch {
	case ev.Err != "":
		s.deliver(h, runtime.Tuple(runtime.Atom("port_error"), pv, runtime.Atom(ev.Err)))
		s.closePort(h)
	case ev.EOF:
		sp.armed = false
		s.deliver(h, runtime.Tuple(runtime.Atom("port_eof"), pv))
		if !sp.duplex {
			s.closePort(h)
		}
	case ev.Data != nil:
		sp.armed = false
		s.deliver(h, runtime.Tuple(runtime.Atom("port_data"), pv, runtime.Value{Kind: runtime.KindBytes, Bytes: ev.Data}))
	default:
		sp.pending -= ev.Written
		if sp.promiseReady && sp.pending < streamHWM {
			sp.promiseReady = false
			s.deliver(h, runtime.Tuple(runtime.Atom("port_ready"), pv))
		}
	}
}

// ownsPort — v порт, и исполняющий актор — его владелец.
func (s *Scheduler) ownsPort(v runtime.Value) bool {
	return v.Kind == runtime.KindPort && s.active != nil && v.Port.Owner == s.active.pid
}

var (
	okUnit      = runtime.Variant("Ok", runtime.Unit)
	errClosed   = runtime.Variant("Error", runtime.Atom("closed"))
	errBusy     = runtime.Variant("Error", runtime.Atom("busy"))
	errNotAlive = runtime.Variant("Error", runtime.Atom("not_alive"))
)

// installStreams регистрирует Port.request, Port.write, Port.give и
// File.open.
func installStreams(def func(name string, arity int, fn runtime.NativeFunc)) {
	def("Port.request", 1, func(c runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		s := c.(*VM).scheduler
		v := args[0]
		if !s.ownsPort(v) {
			return runtime.Unit, typeErr("request", v)
		}
		if v.Port.Closed {
			return errClosed, nil
		}
		sp := s.ports[v.Port.ID].stream
		if sp == nil || !sp.read {
			return runtime.Unit, typeErr("request", v)
		}
		if !sp.armed {
			sp.armed = true
			sp.res.Read()
		}
		return okUnit, nil
	})
	def("Port.write", 2, func(c runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		s := c.(*VM).scheduler
		v := args[0]
		if !s.ownsPort(v) {
			return runtime.Unit, typeErr("write", v)
		}
		b, ok := appendIOData(nil, args[1])
		if !ok {
			return runtime.Unit, typeErr("write", args[1])
		}
		if v.Port.Closed {
			return errClosed, nil
		}
		sp := s.ports[v.Port.ID].stream
		if sp == nil || !sp.write {
			return runtime.Unit, typeErr("write", v)
		}
		if sp.pending >= streamHWM {
			sp.promiseReady = true
			return errBusy, nil
		}
		if len(b) > 0 {
			sp.pending += len(b)
			sp.res.Write(b)
		}
		return okUnit, nil
	})
	def("Port.give", 2, func(c runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		s := c.(*VM).scheduler
		v, pid := args[0], args[1]
		if !s.ownsPort(v) {
			return runtime.Unit, typeErr("give", v)
		}
		if pid.Kind != runtime.KindPid {
			return runtime.Unit, typeErr("give", pid)
		}
		if v.Port.Closed {
			return errClosed, nil
		}
		to, ok := s.actors[pid.Pid]
		if !ok || to.status == actorDone || to.status == actorFailed {
			return errNotAlive, nil
		}
		if to != s.active {
			s.unownPort(v.Port)
			v.Port.Owner = to.pid
			to.ports = append(to.ports, v.Port)
		}
		return okUnit, nil
	})
	def("File.open", 2, func(c runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		m := c.(*VM)
		path, mode := args[0], args[1]
		if path.Kind != runtime.KindStr {
			return runtime.Unit, typeErr("open", path)
		}
		if mode.Kind != runtime.KindAtom || (mode.Atom != "read" && mode.Atom != "write" && mode.Atom != "append") {
			return runtime.Unit, typeErr("open", mode)
		}
		s := m.scheduler
		a := s.active
		if a == nil || a.pid < 0 {
			return runtime.Unit, errors.New("internal: File.open outside an actor")
		}
		hub := m.files
		read := mode.Atom == "read"
		return s.newStreamPort(a, read, !read, func(emit func(StreamEvent)) Stream {
			if hub == nil {
				return noStream{emit}
			}
			return hub.Open(path.Str, mode.Atom, emit)
		}), nil
	})
}

// appendIOData дописывает к b байты iodata (§12.12): Bytes, Str (UTF-8)
// или список iodata.
func appendIOData(b []byte, v runtime.Value) ([]byte, bool) {
	switch v.Kind {
	case runtime.KindBytes:
		return append(b, v.Bytes...), true
	case runtime.KindStr:
		return append(b, v.Str...), true
	case runtime.KindList:
		for _, e := range v.Elems() {
			var ok bool
			if b, ok = appendIOData(b, e); !ok {
				return nil, false
			}
		}
		return b, true
	}
	return nil, false
}

// noStream — ресурс без ОС: любой запрос и запись — :enotsup.
type noStream struct{ emit func(StreamEvent) }

func (n noStream) Read()             { n.emit(StreamEvent{Err: "enotsup"}) }
func (n noStream) Write([]byte)      { n.emit(StreamEvent{Err: "enotsup"}) }
func (n noStream) Close(done func()) { done() }
