package vm

import (
	"errors"
	"sort"

	"github.com/it1ro/brig-lang/internal/runtime"
)

// ---- интроспекция: Actor.list, Actor.info, Snapshot (§12.13, T-221) ----

// sessionInitialFn — initial_fn актора сессии: у него нет начальной
// функции, вводы приходят кадрами поверх пустого стека.
const sessionInitialFn = "<repl>"

// ActorSnapshot — запись об одном живом акторе: то же, что Actor.info
// (§12.13), в виде Go-структуры для консоли.
type ActorSnapshot struct {
	Pid int
	// Name — первое из ещё связанных имён по порядку register; HasName —
	// имя есть.
	Name    runtime.Value
	HasName bool
	// Status — "running", "recv" или "waiting".
	Status         string
	Mailbox        int
	Reductions     int64
	AllocBytes     int64
	TurnReductions int64
	TurnAllocBytes int64
	// Watchers, Watching — pid, каждый один раз, в порядке первого
	// взведённого (и не снятого) наблюдения.
	Watchers  []int
	Watching  []int
	InitialFn string
}

// liveActor — актор pid, если он жив; иначе nil. mainPid после выхода
// остаётся в таблице, но уже не жив.
func (s *Scheduler) liveActor(pid int) *Actor {
	a := s.actors[pid]
	if a == nil || a.status == actorDone || a.status == actorFailed {
		return nil
	}
	return a
}

// livePids — живые акторы по возрастанию pid.
func (s *Scheduler) livePids() []int {
	pids := make([]int, 0, len(s.actors))
	for pid := range s.actors {
		if s.liveActor(pid) != nil {
			pids = append(pids, pid)
		}
	}
	sort.Ints(pids)
	return pids
}

// watchEdge — живое наблюдение: from следит за to через ref.
type watchEdge struct{ ref, from, to int }

// watchEdges — все наблюдения между живыми акторами по возрастанию ref
// (ref выдаются по порядку взвода). Снятое unwatch в watchers уже нет.
func (s *Scheduler) watchEdges() []watchEdge {
	var es []watchEdge
	for _, a := range s.actors {
		if s.liveActor(a.pid) == nil {
			continue
		}
		for ref, from := range a.watchers {
			if s.liveActor(from) != nil {
				es = append(es, watchEdge{ref: ref, from: from, to: a.pid})
			}
		}
	}
	sort.Slice(es, func(i, j int) bool { return es[i].ref < es[j].ref })
	return es
}

// appendOnce добавляет pid, если его ещё нет.
func appendOnce(xs []int, pid int) []int {
	for _, x := range xs {
		if x == pid {
			return xs
		}
	}
	return append(xs, pid)
}

func (a *Actor) statusName() string {
	switch {
	case a.status != actorBlocked:
		return "running"
	case a.awaiting != nil:
		return "waiting"
	default:
		return "recv"
	}
}

// describe — запись живого актора a; es — watchEdges().
func (s *Scheduler) describe(a *Actor, es []watchEdge) ActorSnapshot {
	d := ActorSnapshot{
		Pid:            a.pid,
		Status:         a.statusName(),
		Mailbox:        len(a.mailbox),
		Reductions:     a.pastReds + a.turnReds,
		AllocBytes:     a.pastAlloc + a.turnAlloc,
		TurnReductions: a.turnReds,
		TurnAllocBytes: a.turnAlloc,
		Watchers:       []int{},
		Watching:       []int{},
		InitialFn:      a.initialFn,
	}
	for _, e := range s.names {
		if e.pid == a.pid {
			d.Name, d.HasName = e.name, true
			break
		}
	}
	for _, e := range es {
		if e.to == a.pid {
			d.Watchers = appendOnce(d.Watchers, e.from)
		}
		if e.from == a.pid {
			d.Watching = appendOnce(d.Watching, e.to)
		}
	}
	return d
}

// actorList — Actor.list(): живые акторы.
func (s *Scheduler) actorList() runtime.Value {
	pids := s.livePids()
	out := make([]runtime.Value, len(pids))
	for i, pid := range pids {
		out[i] = runtime.Value{Kind: runtime.KindPid, Pid: pid}
	}
	return runtime.List(out...)
}

// actorInfo — Actor.info(pid): Some(запись) живого актора, None — мёртвого
// или несуществующего. Для чужого актора значения racy, как mailbox_size.
func (s *Scheduler) actorInfo(pid int) runtime.Value {
	a := s.liveActor(pid)
	if a == nil {
		return runtime.Variant("None")
	}
	d := s.describe(a, s.watchEdges())
	name := runtime.Variant("None")
	if d.HasName {
		name = runtime.Variant("Some", d.Name)
	}
	return runtime.Variant("Some", runtime.Record("", []runtime.RecordField{
		{Name: "reductions", Val: runtime.Int(d.Reductions)},
		{Name: "alloc_bytes", Val: runtime.Int(d.AllocBytes)},
		{Name: "mailbox", Val: runtime.Int(int64(d.Mailbox))},
		{Name: "turn_reductions", Val: runtime.Int(d.TurnReductions)},
		{Name: "turn_alloc_bytes", Val: runtime.Int(d.TurnAllocBytes)},
		{Name: "name", Val: name},
		{Name: "status", Val: runtime.Atom(d.Status)},
		{Name: "watchers", Val: pidList(d.Watchers)},
		{Name: "watching", Val: pidList(d.Watching)},
		{Name: "initial_fn", Val: runtime.Str(d.InitialFn)},
	}))
}

func pidList(pids []int) runtime.Value {
	out := make([]runtime.Value, len(pids))
	for i, pid := range pids {
		out[i] = runtime.Value{Kind: runtime.KindPid, Pid: pid}
	}
	return runtime.List(out...)
}

// snapshot — записи всех живых акторов по возрастанию pid. Только с
// горутины планировщика.
func (s *Scheduler) snapshot() []ActorSnapshot {
	es := s.watchEdges()
	pids := s.livePids()
	out := make([]ActorSnapshot, len(pids))
	for i, pid := range pids {
		out[i] = s.describe(s.actors[pid], es)
	}
	return out
}

// Snapshot — снимок таблицы акторов для консоли. Его берёт горутина цикла
// сессии между слайсами, в том числе посреди долгого ввода; вызывающий
// ждёт не дольше слайса. Натив на горутине цикла зовёт SnapshotHere:
// Snapshot оттуда — deadlock. Без сессии — ошибка.
func (s *Scheduler) Snapshot() ([]ActorSnapshot, error) {
	if !s.session {
		return nil, errors.New("internal: repl session is not started")
	}
	reply := make(chan []ActorSnapshot, 1)
	select {
	case s.snaps <- reply:
	case <-s.stop:
		return nil, errSessionClosed
	}
	select {
	case snap := <-reply:
		return snap, nil
	case <-s.stop:
		return nil, errSessionClosed
	}
}

// SnapshotHere — Snapshot из натива, который исполняет цикл сессии.
func (s *Scheduler) SnapshotHere() []ActorSnapshot { return s.snapshot() }

// serveSnapshots отвечает на ждущие запросы Snapshot. Зовёт цикл сессии
// между слайсами; простаивающий цикл берёт запрос в waitSession.
func (s *Scheduler) serveSnapshots() {
	for {
		select {
		case reply := <-s.snaps:
			reply <- s.snapshot()
		default:
			return
		}
	}
}
