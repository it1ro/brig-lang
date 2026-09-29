package vm

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/it1ro/brig-lang/internal/runtime"
)

// ---- порты и внешние события (§12.12, §15.2; docs/02 §6, T-168) ----
//
// Порт принадлежит VM, а что он ждёт у ОС — знает только реализация его
// вида за интерфейсом (R14): ядро не импортирует os/signal и подобное,
// реализации подключает cmd/brig. Ресурс ждёт в своей goroutine и кладёт
// события в inject-очередь; состояние порта (владелец, закрыт ли) читает
// и пишет только run-loop, поэтому событие закрытого порта просто
// отбрасывается при разборе очереди.

// SignalHub — реализация вида порта Signal: сигналы ОС. Open начинает
// доставку сигналов names (:sigterm → "sigterm"): каждый пришедший —
// вызов emit(name) из goroutine ресурса, по порядку прихода (G5). Open
// зовётся с горутины run-loop; подписка перехватывает сигнал, пока не
// вызвана возвращённая функция закрытия. emit после закрытия допустим:
// run-loop такое событие отбросит.
type SignalHub interface {
	Open(names []string, emit func(name string)) (stop func())
}

// SetSignals подключает реализацию Signal. Без неё Signal.subscribe
// открывает порт, но сигналов он не получает (VM без ОС).
func (vm *VM) SetSignals(h SignalHub) { vm.signals = h }

// ErrHalt — программу остановил Sys.halt(Code) (§12.12). Не raise: trap
// его не ловит, ensure не исполняются.
type ErrHalt struct{ Code int }

func (e *ErrHalt) Error() string { return fmt.Sprintf("halt %d", e.Code) }

// signalNames — имена сигналов, на которые можно подписаться (§12.12).
var signalNames = []string{"sigterm", "sigint"}

// openPort — открытый порт в таблице планировщика.
type openPort struct {
	h     *runtime.PortHandle
	close func() // освобождает ресурс; nil — ресурса нет
}

// injectEvent — событие ресурса для владельца порта.
type injectEvent struct {
	port *runtime.PortHandle
	msg  runtime.Value
}

// injectQueue — единственный вход для событий, порождённых не акторами.
// push зовут goroutine ресурсов, take — run-loop. ready — сигнал «очередь
// не пуста» для ожидания в select; лишний сигнал безвреден.
type injectQueue struct {
	mu    sync.Mutex
	evs   []injectEvent
	ready chan struct{}
}

func (q *injectQueue) push(ev injectEvent) {
	q.mu.Lock()
	q.evs = append(q.evs, ev)
	q.mu.Unlock()
	select {
	case q.ready <- struct{}{}:
	default:
	}
}

func (q *injectQueue) take() []injectEvent {
	q.mu.Lock()
	evs := q.evs
	q.evs = nil
	q.mu.Unlock()
	return evs
}

// drainInject разбирает inject-очередь: событие открытого порта — в конец
// ящика владельца мимо HWM (§12.12), владелец будится; событие закрытого
// порта отбрасывается.
func (s *Scheduler) drainInject() {
	for _, ev := range s.inject.take() {
		if ev.port.Closed {
			continue
		}
		a, ok := s.actors[ev.port.Owner]
		if !ok {
			continue
		}
		a.mailbox = append(a.mailbox, ev.msg)
		s.wakeIfBlocked(a)
	}
}

// waitEvent — ready пуст: ждёт ближайший таймер или событие порта.
// false — ждать нечего (ни таймеров, ни открытых портов).
func (s *Scheduler) waitEvent() bool {
	next := s.nextDeadline()
	if next.IsZero() && len(s.ports) == 0 {
		return false
	}
	var timerC <-chan time.Time
	if !next.IsZero() {
		d := time.Until(next)
		if d <= 0 {
			s.wakeExpired()
			return true
		}
		t := time.NewTimer(d)
		defer t.Stop()
		timerC = t.C
	}
	select {
	case <-timerC:
		s.wakeExpired()
	case <-s.inject.ready:
	}
	return true
}

// newPort открывает порт, владелец — a. open получает функцию, которой
// ресурс отдаёт события, и возвращает закрытие ресурса.
func (s *Scheduler) newPort(a *Actor, open func(emit func(runtime.Value)) func()) runtime.Value {
	h := &runtime.PortHandle{ID: s.nextPort, Owner: a.pid}
	s.nextPort++
	p := &openPort{h: h}
	s.ports[h.ID] = p
	a.ports = append(a.ports, h)
	p.close = open(func(msg runtime.Value) {
		s.inject.push(injectEvent{port: h, msg: msg})
	})
	return runtime.Value{Kind: runtime.KindPort, Port: h}
}

// closePort закрывает порт: ресурс освобождается, события больше не
// доставляются. Закрытый порт не трогается.
func (s *Scheduler) closePort(h *runtime.PortHandle) {
	if h.Closed {
		return
	}
	h.Closed = true
	p := s.ports[h.ID]
	delete(s.ports, h.ID)
	if a, ok := s.actors[h.Owner]; ok {
		for i, x := range a.ports {
			if x == h {
				a.ports = append(a.ports[:i], a.ports[i+1:]...)
				break
			}
		}
	}
	if p != nil && p.close != nil {
		p.close()
	}
}

// closeActorPorts закрывает порты умершего актора — в том же шаге, что
// :down и снятие имён (§12.12).
func (s *Scheduler) closeActorPorts(a *Actor) {
	for len(a.ports) > 0 {
		s.closePort(a.ports[len(a.ports)-1])
	}
}

// closeAllPorts — выход из программы: ресурсы всех портов освобождаются.
func (s *Scheduler) closeAllPorts() {
	for _, p := range s.ports {
		s.closePort(p.h)
	}
}

// halting — ошибка актора — Sys.halt: планировщик останавливается, ensure
// не исполняются (§12.12).
func (s *Scheduler) halting(a *Actor) bool {
	var h *ErrHalt
	if !errors.As(a.err, &h) {
		return false
	}
	s.halt = h
	return true
}

// installPorts регистрирует Port.close, Signal.subscribe и Sys.halt.
// Порт создаёт и закрывает актор, который исполняет натив (s.active).
func installPorts(def func(name string, arity int, fn runtime.NativeFunc)) {
	def("Signal.subscribe", 1, func(c runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		m := c.(*VM)
		names, ok := subscribeNames(args[0])
		if !ok {
			return runtime.Unit, typeErr("subscribe", args[0])
		}
		s := m.scheduler
		a := s.active
		if a == nil || a.pid < 0 {
			return runtime.Unit, errors.New("internal: Signal.subscribe outside an actor")
		}
		hub := m.signals
		return s.newPort(a, func(emit func(runtime.Value)) func() {
			if hub == nil {
				return nil
			}
			return hub.Open(names, func(name string) {
				emit(runtime.Tuple(runtime.Atom("signal"), runtime.Atom(name)))
			})
		}), nil
	})
	def("Port.close", 1, func(c runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		s := c.(*VM).scheduler
		p := args[0]
		if p.Kind != runtime.KindPort || s.active == nil || p.Port.Owner != s.active.pid {
			return runtime.Unit, typeErr("close", p)
		}
		s.closePort(p.Port)
		return runtime.Unit, nil
	})
	def("Sys.halt", 1, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		code := args[0]
		if code.Kind != runtime.KindInt || !code.IsSmall || code.SmallInt < 0 || code.SmallInt > 255 {
			return runtime.Unit, typeErr("halt", code)
		}
		return runtime.Unit, &ErrHalt{Code: int(code.SmallInt)}
	})
}

// subscribeNames — непустой список атомов из signalNames, без повторов.
func subscribeNames(v runtime.Value) ([]string, bool) {
	if v.Kind != runtime.KindList || len(v.List) == 0 {
		return nil, false
	}
	var names []string
	for _, e := range v.List {
		if e.Kind != runtime.KindAtom || !slices.Contains(signalNames, e.Atom) {
			return nil, false
		}
		if !slices.Contains(names, e.Atom) {
			names = append(names, e.Atom)
		}
	}
	return names, true
}
