package vm

import (
	"errors"

	"github.com/it1ro/brig-lang/internal/runtime"
)

// ---- Telemetry (§12.14, T-222) ----
//
// Реестр подписок — таблица планировщика, общая для всех акторов, как
// Global, но через Global.get не видна. Пока подписок нет, emit и события
// VM не аллоцируют: одна проверка длины реестра.
//
// Доставка — кадр teleRun на акторе-излучателе. Обработчики идут в порядке
// attach; набор фиксируется в начале emit. Непойманный raise снимает
// подписку и вкладывает [:telemetry, :handler, :failed]; exit кадр не ловит.
//
// [:vm, :spawn] и [:vm, :mailbox, :hwm] от send доставляются в кадре
// создателя или отправителя (Frame.dropResult: callDst вызывающего не
// затирается). Смерть и потерянный Timer.send_after кладутся в teleQ:
// умерший уже не исполняется, а :kill не должен исполнять в нём ничего.
// Очередь читает служебный актор (telePid); своей смерти он не излучает.

// Пути событий VM и сбоя обработчика. Срез пакета, не variadic: вызов
// teleWant на горячем пути не должен аллоцировать.
var (
	pathSpawn = []string{"vm", "spawn"}
	pathCrash = []string{"vm", "actor", "crash"}
	pathDown  = []string{"vm", "actor", "down"}
	pathHWM   = []string{"vm", "mailbox", "hwm"}
	pathFail  = []string{"telemetry", "handler", "failed"}
)

// teleSub — одна подписка. prefix — атомы пути; пустой совпадает со всем.
type teleSub struct {
	id      runtime.Value
	prefix  []string
	handler runtime.Value
}

// teleEvent — событие в очереди служебного актора.
type teleEvent struct {
	event, meas, meta runtime.Value
}

// teleRun — доставка одного emit (или цикл служебного актора).
// Реализует nativeCont: каждый resume вызывает следующий обработчик.
type teleRun struct {
	s        *Scheduler
	loop     bool // служебный актор: после события берёт следующее из teleQ
	handlers []runtime.Value
	ids      []runtime.Value
	i        int
	event    runtime.Value
	meas     runtime.Value
	meta     runtime.Value
	args     [3]runtime.Value
	curID    runtime.Value
}

func (r *teleRun) resume(runtime.Value) (nativeStep, error) {
	for {
		if r.i < len(r.handlers) {
			h := r.handlers[r.i]
			r.curID = r.ids[r.i]
			r.i++
			r.args[0], r.args[1], r.args[2] = r.event, r.meas, r.meta
			return nativeStep{fn: h, args: r.args[:]}, nil
		}
		if !r.loop {
			return nativeStep{done: true, res: runtime.Unit}, nil
		}
		if len(r.s.teleQ) == 0 {
			return nativeStep{block: true}, nil
		}
		ev := r.s.teleQ[0]
		r.s.teleQ[0] = teleEvent{}
		r.s.teleQ = r.s.teleQ[1:]
		r.handlers, r.ids = r.s.matchHandlers(ev.event)
		r.i = 0
		r.event, r.meas, r.meta = ev.event, ev.meas, ev.meta
	}
}

// teleWant — есть подписка, чей префикс совпадает с path.
// len == 0 — выход без аллокаций и без сборки события.
func (s *Scheduler) teleWant(path []string) bool {
	if len(s.tele) == 0 {
		return false
	}
	for i := range s.tele {
		if s.tele[i].matchesPath(path) {
			return true
		}
	}
	return false
}

func (sub *teleSub) matchesPath(path []string) bool {
	if len(sub.prefix) > len(path) {
		return false
	}
	for i, p := range sub.prefix {
		if p != path[i] {
			return false
		}
	}
	return true
}

func (sub *teleSub) matches(event []runtime.Value) bool {
	if len(sub.prefix) > len(event) {
		return false
	}
	for i, p := range sub.prefix {
		el := event[i]
		if el.Kind != runtime.KindAtom || el.Atom != p {
			return false
		}
	}
	return true
}

// matchHandlers — снимок совпавших обработчиков в порядке attach.
// nil, nil — совпадений нет и ничего не аллоцировано.
func (s *Scheduler) matchHandlers(event runtime.Value) (hs, ids []runtime.Value) {
	ev := event.Elems()
	for i := range s.tele {
		if !s.tele[i].matches(ev) {
			continue
		}
		hs = append(hs, s.tele[i].handler)
		ids = append(ids, s.tele[i].id)
	}
	return hs, ids
}

func (s *Scheduler) teleIndex(id runtime.Value) int {
	for i := range s.tele {
		if runtime.KeyEqual(s.tele[i].id, id) {
			return i
		}
	}
	return -1
}

func (s *Scheduler) teleDetachID(id runtime.Value) {
	i := s.teleIndex(id)
	if i < 0 {
		return
	}
	s.tele = append(s.tele[:i], s.tele[i+1:]...)
}

// teleAttach — Telemetry.attach. Занятый id не меняет прежнюю подписку.
func (s *Scheduler) teleAttach(id, prefix, handler runtime.Value) (runtime.Value, error) {
	if !runtime.KeyEqual(id, id) {
		return runtime.Unit, typeErr("attach", id)
	}
	if err := checkTelePrefix(prefix); err != nil {
		return runtime.Unit, err
	}
	if !exactArity3(handler) {
		return runtime.Unit, typeErr("attach", handler)
	}
	if s.teleIndex(id) >= 0 {
		return runtime.Variant("Error", runtime.Atom("already_exists")), nil
	}
	pref := make([]string, prefix.Len())
	for i, a := range prefix.Elems() {
		pref[i] = a.Atom
	}
	s.tele = append(s.tele, teleSub{id: id, prefix: pref, handler: handler})
	return runtime.Variant("Ok", runtime.Unit), nil
}

// teleDetach — Telemetry.detach. Неизвестный id — (), идемпотентно.
func (s *Scheduler) teleDetach(id runtime.Value) runtime.Value {
	s.teleDetachID(id)
	return runtime.Unit
}

func checkTelePrefix(p runtime.Value) error {
	if p.Kind != runtime.KindList || !allAtoms(p.Elems()) {
		return typeErr("attach", p)
	}
	return nil
}

func checkEmitArgs(event, meas, meta runtime.Value) error {
	if event.Kind != runtime.KindList || event.Len() == 0 || !allAtoms(event.Elems()) {
		return typeErr("emit", event)
	}
	if meas.Kind != runtime.KindRecord {
		return typeErr("emit", meas)
	}
	if meta.Kind != runtime.KindRecord {
		return typeErr("emit", meta)
	}
	return nil
}

func allAtoms(xs []runtime.Value) bool {
	for _, x := range xs {
		if x.Kind != runtime.KindAtom {
			return false
		}
	}
	return true
}

// exactArity3 — функция или замыкание арности 3, не вариадик.
func exactArity3(v runtime.Value) bool {
	switch v.Kind {
	case runtime.KindFunction:
		f := v.Func
		if f == nil || f.Arity != 3 {
			return false
		}
		if ch, ok := f.Body.(*Chunk); ok && ch.Variadic {
			return false
		}
		return true
	case runtime.KindClosure:
		c := v.ClosureVal
		if c == nil || c.Arity != 3 {
			return false
		}
		if ch, ok := c.Func.(*Chunk); ok && ch.Variadic {
			return false
		}
		return true
	default:
		return false
	}
}

// prepEmit проверяет аргументы и, если есть совпавшие подписки, строит
// кадр доставки. Без подписок и без совпадений возвращает nil: кадр не
// нужен, аллокаций нет (ошибка типа аллоцирует только свой путь).
func (s *Scheduler) prepEmit(args []runtime.Value) (*teleRun, runtime.Value, error) {
	event, meas, meta := args[0], args[1], args[2]
	if err := checkEmitArgs(event, meas, meta); err != nil {
		return nil, runtime.Unit, err
	}
	if len(s.tele) == 0 {
		return nil, runtime.Unit, nil
	}
	hs, ids := s.matchHandlers(event)
	if hs == nil {
		return nil, runtime.Unit, nil
	}
	return &teleRun{
		s: s, handlers: hs, ids: ids,
		event: event, meas: meas, meta: meta,
	}, runtime.Unit, nil
}

// beginEmit — Telemetry.emit из CALL: кадр на текущем акторе либо
// готовый () без кадра.
func (s *Scheduler) beginEmit(a *Actor, args []runtime.Value) (*Frame, runtime.Value, error) {
	run, res, err := s.prepEmit(args)
	if err != nil || run == nil {
		return nil, res, err
	}
	return a.pushNative("Telemetry.emit", run), runtime.Unit, nil
}

// teleEmitSync — Telemetry.emit через vm.Call, вне кадра актора.
// Байткод сюда не попадает: enterCall перехватывает teleEmit раньше.
func (s *Scheduler) teleEmitSync(args []runtime.Value) (runtime.Value, error) {
	run, res, err := s.prepEmit(args)
	if err != nil || run == nil {
		return res, err
	}
	return runtime.Unit, errors.New("internal: Telemetry.emit with subscribers outside an actor frame")
}

// catchTele ловит непойманный raise обработчика: подписка снимается,
// [:telemetry, :handler, :failed] доставляется вложенным кадром, внешний
// emit продолжается со следующего обработчика.
func (s *Scheduler) catchTele(a *Actor, r *teleRun, reason runtime.Value) {
	s.teleDetachID(r.curID)
	a.err = nil
	a.result = runtime.Unit
	if !s.teleWant(pathFail) {
		return
	}
	ev := teleFailEvent(r.curID, r.event, reason)
	hs, ids := s.matchHandlers(ev.event)
	if hs == nil {
		return
	}
	a.pushNative("Telemetry.emit", &teleRun{
		s: s, handlers: hs, ids: ids,
		event: ev.event, meas: ev.meas, meta: ev.meta,
	})
}

func teleFailEvent(id, event, reason runtime.Value) teleEvent {
	return teleEvent{
		event: atomList(pathFail),
		meas:  runtime.Record("", nil),
		meta: runtime.Record("", []runtime.RecordField{
			{Name: "id", Val: id},
			{Name: "event", Val: event},
			{Name: "reason", Val: reason},
		}),
	}
}

// telePushInline ставит кадр доставки поверх текущего и просит не писать
// его результат в callDst (опкод уже положил в регистр свой результат).
func (s *Scheduler) telePushInline(a *Actor, event, meas, meta runtime.Value) bool {
	hs, ids := s.matchHandlers(event)
	if hs == nil {
		return false
	}
	f := a.pushNative("Telemetry.emit", &teleRun{
		s: s, handlers: hs, ids: ids,
		event: event, meas: meas, meta: meta,
	})
	f.dropResult = true
	return true
}

// teleInlineSpawn — [:vm, :spawn] в кадре создателя, после взвода
// наблюдения и записи результата spawn, до следующей инструкции.
func (s *Scheduler) teleInlineSpawn(a, child *Actor) bool {
	if !s.teleWant(pathSpawn) {
		return false
	}
	event := atomList(pathSpawn)
	meas := runtime.Record("", []runtime.RecordField{teleMono()})
	meta := runtime.Record("", []runtime.RecordField{
		{Name: "pid", Val: pidVal(child.pid)},
		{Name: "parent", Val: pidVal(a.pid)},
		{Name: "initial_fn", Val: runtime.Str(child.initialFn)},
	})
	return s.telePushInline(a, event, meas, meta)
}

// teleInlineHWM — [:vm, :mailbox, :hwm] от send, в кадре отправителя.
func (s *Scheduler) teleInlineHWM(a *Actor, to int) bool {
	if !s.teleWant(pathHWM) {
		return false
	}
	ev, meas, meta := s.hwmEvent(to, runtime.Variant("Some", pidVal(a.pid)))
	return s.telePushInline(a, ev, meas, meta)
}

// teleTimerHWM — сообщение Timer.send_after не принято: from = None,
// доставляет служебный актор.
func (s *Scheduler) teleTimerHWM(to int) {
	if !s.teleWant(pathHWM) {
		return
	}
	ev, meas, meta := s.hwmEvent(to, runtime.Variant("None"))
	s.teleEnqueue(teleEvent{event: ev, meas: meas, meta: meta})
}

func (s *Scheduler) hwmEvent(to int, from runtime.Value) (event, meas, meta runtime.Value) {
	mbox, hwm := 0, defaultHWM
	if t := s.actors[to]; t != nil {
		mbox, hwm = len(t.mailbox), t.hwm
	}
	event = atomList(pathHWM)
	meas = runtime.Record("", []runtime.RecordField{
		teleMono(),
		{Name: "mailbox", Val: runtime.Int(int64(mbox))},
		{Name: "hwm", Val: runtime.Int(int64(hwm))},
	})
	meta = runtime.Record("", []runtime.RecordField{
		{Name: "pid", Val: pidVal(to)},
		{Name: "from", Val: from},
	})
	return event, meas, meta
}

// teleName — имя актора на момент смерти, как в Actor.info. Без подписок
// на смерть ничего не строит.
func (s *Scheduler) teleName(pid int) runtime.Value {
	if !s.teleWant(pathCrash) && !s.teleWant(pathDown) {
		return runtime.Unit
	}
	return s.nameOption(pid)
}

func (s *Scheduler) nameOption(pid int) runtime.Value {
	for _, e := range s.names {
		if e.pid == pid {
			return runtime.Variant("Some", e.name)
		}
	}
	return runtime.Variant("None")
}

// actorDied — :down наблюдателям, затем события телеметрии, затем reap.
// Имя снимается в notifyWatchers, поэтому снимок имени — до него.
func (s *Scheduler) actorDied(a *Actor, reason runtime.Value, crash bool, trace []TraceFrame) {
	name := runtime.Unit
	if a.pid != s.telePid {
		name = s.teleName(a.pid)
	}
	s.notifyWatchers(a, reason)
	s.teleOnDeath(a, name, reason, crash, trace)
	s.reapActor(a)
}

// teleOnDeath ставит [:vm, :actor, :crash] (только raise) и затем
// [:vm, :actor, :down]. Смерть служебного актора событий не излучает.
func (s *Scheduler) teleOnDeath(a *Actor, name, reason runtime.Value, crash bool, trace []TraceFrame) {
	if a.pid == s.telePid {
		s.telePid = -1
		if len(s.tele) == 0 {
			s.teleQ = nil
			return
		}
		if len(s.teleQ) > 0 {
			s.ensureTeleActor()
		}
		return
	}
	wantCrash := crash && s.teleWant(pathCrash)
	wantDown := s.teleWant(pathDown)
	if !wantCrash && !wantDown {
		return
	}
	mono := monotonicMillis()
	reds := a.pastReds + a.turnReds
	alloc := a.pastAlloc + a.turnAlloc
	pid := pidVal(a.pid)
	if wantCrash {
		s.teleEnqueue(teleEvent{
			event: atomList(pathCrash),
			meas: runtime.Record("", []runtime.RecordField{
				{Name: "monotonic_ms", Val: runtime.Int(mono)},
				{Name: "reductions", Val: runtime.Int(reds)},
				{Name: "alloc_bytes", Val: runtime.Int(alloc)},
			}),
			meta: runtime.Record("", []runtime.RecordField{
				{Name: "pid", Val: pid},
				{Name: "name", Val: name},
				{Name: "reason", Val: reason},
				{Name: "trace", Val: traceList(trace)},
			}),
		})
	}
	if !wantDown {
		return
	}
	life := mono - a.bornMs
	if life < 0 {
		life = 0
	}
	s.teleEnqueue(teleEvent{
		event: atomList(pathDown),
		meas: runtime.Record("", []runtime.RecordField{
			{Name: "monotonic_ms", Val: runtime.Int(mono)},
			{Name: "reductions", Val: runtime.Int(reds)},
			{Name: "alloc_bytes", Val: runtime.Int(alloc)},
			{Name: "lifetime_ms", Val: runtime.Int(life)},
		}),
		meta: runtime.Record("", []runtime.RecordField{
			{Name: "pid", Val: pid},
			{Name: "name", Val: name},
			{Name: "reason", Val: reason},
		}),
	})
}

func (s *Scheduler) teleEnqueue(ev teleEvent) {
	s.ensureTeleActor()
	s.teleQ = append(s.teleQ, ev)
	if a := s.liveActor(s.telePid); a != nil {
		s.wakeIfBlocked(a)
	}
}

// ensureTeleActor создаёт служебный актор при первом событии очереди.
// Это не spawn: [:vm, :spawn] для него не излучается. initial_fn —
// «<telemetry>», имени в реестре нет.
func (s *Scheduler) ensureTeleActor() {
	if s.liveActor(s.telePid) != nil {
		return
	}
	a := &Actor{
		hwm:       defaultHWM,
		watchers:  make(map[int]int),
		watching:  make(map[int]int),
		status:    actorReady,
		bornMs:    monotonicMillis(),
		initialFn: "<telemetry>",
	}
	a.pushNative("<telemetry>", &teleRun{s: s, loop: true})
	pid := s.nextPid
	s.nextPid++
	a.pid = pid
	s.actors[pid] = a
	s.ready = append(s.ready, a)
	s.telePid = pid
}

func atomList(path []string) runtime.Value {
	xs := make([]runtime.Value, len(path))
	for i, p := range path {
		xs[i] = runtime.Atom(p)
	}
	return runtime.List(xs...)
}

func teleMono() runtime.RecordField {
	return runtime.RecordField{Name: "monotonic_ms", Val: runtime.Int(monotonicMillis())}
}

func pidVal(pid int) runtime.Value {
	return runtime.Value{Kind: runtime.KindPid, Pid: pid}
}

func traceList(tr []TraceFrame) runtime.Value {
	xs := make([]runtime.Value, len(tr))
	for i, fr := range tr {
		xs[i] = runtime.Record("", []runtime.RecordField{
			{Name: "fn", Val: runtime.Str(fr.Func)},
			{Name: "file", Val: runtime.Str(fr.File)},
			{Name: "line", Val: runtime.Int(int64(fr.Pos.Line))},
			{Name: "col", Val: runtime.Int(int64(fr.Pos.Col))},
		})
	}
	return runtime.List(xs...)
}

func isBusyResult(v runtime.Value) bool {
	if v.Kind != runtime.KindVariant || v.Variant == nil || v.Variant.Tag != "Error" || len(v.Variant.Args) != 1 {
		return false
	}
	a := v.Variant.Args[0]
	return a.Kind == runtime.KindAtom && a.Atom == "busy"
}

// installTelemetry регистрирует Telemetry.attach/detach/emit (§12.14).
// Указатель emit сохраняется: enterCall узнаёт его без сравнения строк.
func installTelemetry(vm *VM) {
	emit := &runtime.FuncValue{
		Name: "Telemetry.emit", Arity: 3, IsNative: true,
		Native: func(c runtime.Caller, args []runtime.Value) (runtime.Value, error) {
			return c.(*VM).scheduler.teleEmitSync(args)
		},
	}
	vm.teleEmit = emit
	vm.globals["Telemetry.emit"] = runtime.Func(emit)
	vm.globals["Telemetry.attach"] = runtime.Func(&runtime.FuncValue{
		Name: "Telemetry.attach", Arity: 3, IsNative: true,
		Native: func(c runtime.Caller, args []runtime.Value) (runtime.Value, error) {
			return c.(*VM).scheduler.teleAttach(args[0], args[1], args[2])
		},
	})
	vm.globals["Telemetry.detach"] = runtime.Func(&runtime.FuncValue{
		Name: "Telemetry.detach", Arity: 1, IsNative: true,
		Native: func(c runtime.Caller, args []runtime.Value) (runtime.Value, error) {
			return c.(*VM).scheduler.teleDetach(args[0]), nil
		},
	})
}
