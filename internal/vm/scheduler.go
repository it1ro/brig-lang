package vm

import (
	"container/heap"
	"errors"
	"fmt"
	"math"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/it1ro/brig-lang/internal/runtime"
)

const (
	defaultHWM        = 64
	defaultReductions = 1000
)

// actorStatus — состояние актора.
type actorStatus int

const (
	actorReady actorStatus = iota
	actorBlocked
	actorDone
	actorFailed
)

// stepOutcome — результат одного шага stepFrame.
type stepOutcome int

const (
	stepContinue stepOutcome = iota
	stepYield
	stepBlock
	stepDone
	stepFailed
	// stepExit — сигнал exit нужно обработать сейчас: exit себе или конец
	// ensure-блока, в который вошёл unwind (§12.7).
	stepExit
)

// ---- Frame (§2) ----

// Frame — кадр вызова регистровой ВМ.
//
// Значения живут в regs; стека операндов нет. Параметры лежат в
// R0..R(NumParams-1), за ними локали и временные.
type Frame struct {
	chunk    *Chunk
	name     string
	ip       int
	regs     []runtime.Value
	captures []runtime.Value
	handlers []trapHandler
	callDst  int
	// cont != nil — кадр возобновляемого натива (T-58): chunk == nil,
	// regs[0] принимает результат колбэка (callDst == 0).
	cont nativeCont
	// dropResult — кадр доставки Telemetry, вставленный опкодом (spawn,
	// send): завершение не пишет callDst вызывающего (§12.14).
	dropResult bool
	// cleanups — уровни trap с ensure, чьи кадры заменены TAILCALLENS
	// (doc 02 §5.1); LIFO, переживают TAILCALL. Непустой — завершение
	// кадра (RETURN, raise без handlers, exit) идёт через drain.
	cleanups []cleanupRec
}

// catch пытается поймать *ErrRaise активным trap-регионом. При успехе
// снимает верхний handler, кладёт значение в errReg и переводит ip
// на адрес обработчика. Возвращает true, если ошибка была поймана.
func (f *Frame) catch(err error) bool {
	var rerr *ErrRaise
	if !errors.As(err, &rerr) {
		return false
	}
	if len(f.handlers) == 0 {
		return false
	}
	h := f.handlers[len(f.handlers)-1]
	f.handlers = f.handlers[:len(f.handlers)-1]
	f.regs[h.errReg] = rerr.Val
	f.ip = h.ip
	return true
}

// trapHandler — активный trap (§2, §5).
type trapHandler struct {
	ip     int // абсолютный индекс инструкции-обработчика
	errReg int // регистр, куда кладётся значение raise
	// ensure — handler ensure-блока (TRAPENSURE): unwind от exit входит
	// в него, прочие handlers пропускает (§12.7).
	ensure bool
}

// callee — разобранный Function/Closure с байткод-телом (§2).
type callee struct {
	chunk    *Chunk
	name     string
	captures []runtime.Value
}

// resolveCallee разбирает Function/Closure с байткод-телом. Не-функция —
// ловимый raise (:type_error, (:call, fn)); Function/Closure без тела —
// нарушение инварианта VM (internal:).
func resolveCallee(fn runtime.Value) (callee, error) {
	switch fn.Kind {
	case runtime.KindFunction:
		if fn.Func == nil {
			return callee{}, errors.New("internal: call of nil function")
		}
		ch, ok := fn.Func.Body.(*Chunk)
		if !ok {
			return callee{}, typeErr("call", fn)
		}
		return callee{chunk: ch, name: fn.Func.Name}, nil
	case runtime.KindClosure:
		if fn.ClosureVal == nil {
			return callee{}, errors.New("internal: call of nil closure")
		}
		ch, ok := fn.ClosureVal.Func.(*Chunk)
		if !ok {
			return callee{}, typeErr("call", fn)
		}
		return callee{
			chunk:    ch,
			name:     fn.ClosureVal.Name,
			captures: fn.ClosureVal.Captures,
		}, nil
	}
	return callee{}, typeErr("call", fn)
}

// functionClause — ловимый raise (:function_clause, [args]) (§6.1, §10.4):
// ни один клоз/арность не подошли. args копируется: он может быть окном
// регистров вызывающего.
func functionClause(args []runtime.Value) error {
	return &ErrRaise{Val: runtime.Tuple(
		runtime.Atom("function_clause"),
		runtime.List(append([]runtime.Value(nil), args...)...))}
}

// checkArity: variadic — len(args) >= NumParams-1, иначе == NumParams.
func checkArity(c callee, args []runtime.Value) error {
	argc := len(args)
	if c.chunk.Variadic {
		if argc < c.chunk.NumParams-1 {
			return functionClause(args)
		}
		return nil
	}
	if argc != c.chunk.NumParams {
		return functionClause(args)
	}
	return nil
}

// bindArgs раскладывает args по регистрам параметров и НЕ удерживает args.
// Безопасна при перекрытии args и regs (TAILCALL): rest копируется первым.
// Вариадическая ветка — отдельной функцией: bindArgs стоит на каждом CALL
// и должна оставаться встраиваемой.
func bindArgs(regs []runtime.Value, ch *Chunk, args []runtime.Value) {
	if !ch.Variadic {
		copy(regs, args) // copy == memmove; перекрытие допустимо
		return
	}
	bindVariadic(regs, ch.NumParams-1, args)
}

//go:noinline
func bindVariadic(regs []runtime.Value, fixed int, args []runtime.Value) {
	rest := runtime.List(args[fixed:]...) // копирует args до записи в regs
	copy(regs, args[:fixed])
	regs[fixed] = rest
}

// spreadArgs разворачивает последний аргумент-List в отдельные аргументы;
// результат — свежий срез, не пересекающийся с regs.
func spreadArgs(args []runtime.Value) ([]runtime.Value, error) {
	last := args[len(args)-1]
	if last.Kind != runtime.KindList {
		return nil, typeErr("spread", last)
	}
	out := make([]runtime.Value, 0, len(args)-1+last.Len())
	out = append(out, args[:len(args)-1]...)
	for e := range last.Items() {
		out = append(out, e)
	}
	return out, nil
}

// pushCall кладёт на стек актора кадр вызова fn(args). args может быть
// окном регистров вызывающего: окно нового кадра с ним не пересекается.
func (a *Actor) pushCall(fn runtime.Value, args []runtime.Value) (*Frame, error) {
	c, err := resolveCallee(fn)
	if err != nil {
		return nil, err
	}
	if err := checkArity(c, args); err != nil {
		return nil, err
	}
	regs := a.regs.alloc(c.chunk.NumRegs)
	bindArgs(regs, c.chunk, args)
	f := a.pushFrame()
	f.chunk, f.name, f.regs, f.captures = c.chunk, c.name, regs, c.captures
	return f, nil
}

// pushNative кладёт на стек актора кадр возобновляемого натива name с
// состоянием k.
func (a *Actor) pushNative(name string, k nativeCont) *Frame {
	regs := a.regs.alloc(1)
	f := a.pushFrame()
	f.name, f.regs, f.cont = name, regs, k
	return f
}

// pushFrame отдаёт чистый кадр на вершине a.frames, переиспользуя снятый
// ранее: он остаётся в a.frames за len (см. popFrame).
func (a *Actor) pushFrame() *Frame {
	n := len(a.frames)
	if n < cap(a.frames) {
		a.frames = a.frames[:n+1]
		if f := a.frames[n]; f != nil {
			return f
		}
	} else {
		a.frames = append(a.frames, nil)
	}
	f := &Frame{}
	a.frames[n] = f
	return f
}

// popFrame снимает верхний кадр: окно регистров обнуляется и возвращается
// в стек, кадр очищается и ждёт следующего pushFrame.
func (a *Actor) popFrame() {
	n := len(a.frames) - 1
	f := a.frames[n]
	a.regs.free(f.regs)
	*f = Frame{handlers: f.handlers[:0]}
	a.frames = a.frames[:n]
}

// shed отдаёт GC запас стеков актора — запасные сегменты регистров и
// снятые кадры: заблокированный актор не держит память под вызовы.
func (a *Actor) shed() {
	a.regs.shrink()
	clear(a.frames[len(a.frames):cap(a.frames)])
}

// enterCall готовит вызов fn(args) из кадра актора: кладёт на стек кадр
// байткод-функции или возобновляемого натива (G3, T-58) либо сразу
// возвращает результат обычного натива, который редукций не тратит (K-4).
// args может быть окном регистров вызывающего: нативу уходит свежая копия
// (K-5).
func (s *Scheduler) enterCall(a *Actor, fn runtime.Value, args []runtime.Value) (*Frame, runtime.Value, error) {
	if fn.Kind == runtime.KindFunction && fn.Func != nil && fn.Func.IsNative {
		if ar := fn.Func.Arity; ar >= 0 && ar != len(args) {
			return nil, runtime.Unit, functionClause(args)
		}
		fresh := append([]runtime.Value(nil), args...)
		if fn.Func == s.vm.teleEmit {
			f, r, err := s.beginEmit(a, fresh)
			if err == nil && f == nil {
				s.charge(a, &r)
			}
			return f, r, err
		}
		if start, ok := s.vm.resumable[fn.Func]; ok {
			k, err := start(fresh)
			if err != nil {
				return nil, runtime.Unit, err
			}
			return a.pushNative(fn.Func.Name, k), runtime.Unit, nil
		}
		r, err := fn.Func.Native(s.vm, fresh)
		if err == nil {
			s.charge(a, &r)
		}
		return nil, r, err
	}
	nf, err := a.pushCall(fn, args)
	return nf, runtime.Unit, err
}

// ---- стек регистров актора (T-103) ----

// minRegSeg — нижняя граница размера сегмента после первого.
const minRegSeg = 16

// regStack — окна регистров кадров актора. Окно — срез одного сегмента с
// cap == размеру окна; сегменты не переезжают, поэтому f.regs валиден, пока
// кадр на стеке. Свободная часть сегментов всегда обнулена: alloc отдаёт
// чистое окно, free обнуляет освобождённое — снятый кадр не удерживает
// значения. Первый сегмент — ровно под кадр spawn (простаивающий актор не
// платит за запас), следующие растут вдвое.
type regStack struct {
	segs  [][]runtime.Value
	used  []int // занято в segs[i]
	depth int   // сегментов в работе: вершина — segs[depth-1]
}

// alloc выделяет окно из n регистров на вершине стека.
func (st *regStack) alloc(n int) []runtime.Value {
	if st.depth == 0 || n > len(st.segs[st.depth-1])-st.used[st.depth-1] {
		st.advance(n)
	}
	i := st.depth - 1
	u := st.used[i]
	st.used[i] = u + n
	return st.segs[i][u : u+n : u+n]
}

// advance делает вершиной следующий сегмент, вмещающий n регистров:
// сохранённый с прошлого раза или новый.
func (st *regStack) advance(n int) {
	if st.depth == 1 && st.used[0] == 0 {
		// Первый сегмент пуст (TAILCALL из кадра spawn): подгоняем его под
		// новый кадр, а не держим рядом второй.
		st.segs[0] = make([]runtime.Value, n)
		return
	}
	i := st.depth
	st.depth++
	if i < len(st.segs) && len(st.segs[i]) >= n {
		return
	}
	size := n
	if i > 0 {
		size = max(n, 2*len(st.segs[i-1]), minRegSeg)
	}
	seg := make([]runtime.Value, size)
	if i < len(st.segs) {
		st.segs[i] = seg
		return
	}
	st.segs = append(st.segs, seg)
	st.used = append(st.used, 0)
}

// pop снимает окно w с вершины стека, не обнуляя его: w ещё читают
// (аргументы TAILCALL), обнуляет вызывающий. Обычный путь — free.
func (st *regStack) pop(w []runtime.Value) {
	i := st.depth - 1
	st.used[i] -= cap(w)
	if st.used[i] > 0 || i == 0 {
		return
	}
	// Сегмент опустел: вершина — предыдущий. Опустевший остаётся запасным
	// (вызовы на границе сегментов не аллоцируют), более дальние отдаются GC.
	st.depth--
	clear(st.segs[i+1:])
	st.segs, st.used = st.segs[:i+1], st.used[:i+1]
}

// shrink отдаёт GC запасные сегменты за вершиной.
func (st *regStack) shrink() {
	clear(st.segs[st.depth:])
	st.segs, st.used = st.segs[:st.depth], st.used[:st.depth]
}

// free снимает окно w с вершины стека и обнуляет его.
func (st *regStack) free(w []runtime.Value) {
	clear(w[:cap(w)])
	st.pop(w)
}

// ---- Actor / Scheduler ----

// Actor — процесс в планировщике.
type Actor struct {
	pid int
	// frames — стек кадров; за len лежат снятые кадры для переиспользования.
	frames []*Frame
	regs   regStack

	mailbox  []runtime.Value
	downMsgs []runtime.Value
	hwm      int

	watchers map[int]int
	watching map[int]int

	status actorStatus
	result runtime.Value
	err    error

	recvDeadline time.Time
	// timerSeq — порядковый номер взвода таймера: разрешает равные
	// recvDeadline в wakeExpired (§15.4).
	timerSeq uint64
	// timerPos — позиция в Scheduler.timers плюс 1; 0 — таймер не в куче.
	timerPos int
	// awaiting — слот, ответа в который актор ждёт в await (§12.9); таймер
	// await — тот же recvDeadline. nil — актор не в await.
	awaiting *runtime.RefSlot

	// exit — полученный сигнал exit (§12.7); nil — сигнала нет.
	exit *exitSig

	// budget — счётчики и лимиты хода (§12.10).
	budget

	// ports — открытые порты, которыми актор владеет (§12.12).
	ports []*runtime.PortHandle

	// initialFn — имя начальной функции в виде trace (Actor.info, §12.13).
	initialFn string
	// bornMs — Time.monotonic_ms в момент создания (lifetime_ms, §12.14).
	bornMs int64
}

// Scheduler — единый run-loop (§15.2).
type Scheduler struct {
	vm     *VM
	actors map[int]*Actor
	// names — реестр имён (§12.8) в порядке регистрации; ключи сравниваются
	// как ключи Map (runtime.KeyEqual).
	names []nameEntry
	// globalTable — Global.put/get (§12.11): имя → значение на планировщике,
	// не на акторе. Ключи — runtime.KeyEqual; удаление записи не предусмотрено.
	globalTable []globalEntry
	// tele — подписки Telemetry (§12.14) в порядке attach. Не Global.
	tele []teleSub
	// teleQ — события смерти и потерянного Timer.send_after. Обычный ящик
	// им не служит: HWM не должен их глотать.
	teleQ []teleEvent
	// telePid — служебный актор телеметрии; -1, пока его нет.
	telePid int
	nextPid int
	nextRef int
	nextSeq uint64
	// timers — min-куча таймеров по (deadline, seq): recv … after, await
	// и Timer.send_after. Обслуживание не обходит s.actors (T-102, T-166).
	timers timerHeap
	// sends — взведённые Timer.send_after по ref. Срабатывание и cancel
	// снимают запись. Таймер принадлежит VM и переживает актора.
	sends map[int]*timerEntry
	ready []*Actor
	reds  int
	// main — актор main программы (runMain). Умерший main снят с таблицы,
	// как любой актор: программа с открытым портом живёт дольше него
	// (§15.2), и send/watch ему — как мёртвому pid. Итог читается отсюда.
	main *Actor
	// active — актор, чей кадр исполняется: владелец порта для нативов
	// Signal.subscribe, File.open, HttpServer.* и Port.*.
	active *Actor
	// ports — открытые порты по ID (§12.12); их число держит программу
	// после завершения main (§15.2).
	ports    map[int]*openPort
	nextPort int
	// inject — события портов из goroutine ресурсов (docs/02 §6).
	inject injectQueue
	// closing — ресурсы закрытых потоковых портов, которые ещё дописывают
	// принятое; выход из программы их ждёт (§12.12, T-228).
	closing sync.WaitGroup
	// halt — вызван Sys.halt: run-loop выходит на ближайшей итерации.
	halt *ErrHalt
	// timerVisits — сколько записей кучи осмотрели nextDeadline/wakeExpired
	// (якорь сложности таймеров, T-102).
	timerVisits uint64

	// Режим сессии REPL (§11.4, T-205). sessionPid == -1 — режим выключен;
	// акторы по-прежнему крутит только runMain, по одному слайсу, без
	// фоновой горутины. Поля ниже читает и пишет горутина sessionLoop,
	// кроме jobs/wake/stop/interrupt: их трогает ещё поток, который сдаёт
	// ввод и шлёт прерывание.
	session        bool
	sessionPid     int
	jobs           chan *sessionJob
	snaps          chan chan []ActorSnapshot
	wake           chan struct{}
	stop           chan struct{}
	loopDone       chan struct{}
	current        *sessionJob
	pending        *sessionJob
	interrupt      atomic.Bool
	afterInterrupt atomic.Uint64
	stopOnce       sync.Once
	// redCount — необязательный счётчик редукций (тест границы прерывания).
	// Указатель пишется до старта sessionLoop.
	redCount *atomic.Uint64
}

// NewScheduler создаёт планировщик.
func NewScheduler(vm *VM) *Scheduler {
	return &Scheduler{
		vm:         vm,
		actors:     make(map[int]*Actor),
		sends:      make(map[int]*timerEntry),
		ports:      make(map[int]*openPort),
		inject:     injectQueue{ready: make(chan struct{}, 1)},
		reds:       defaultReductions,
		sessionPid: -1,
		telePid:    -1,
	}
}

// Spawn регистрирует новый актор с fn и args.
func (s *Scheduler) Spawn(fn runtime.Value, args []runtime.Value) (int, error) {
	a := &Actor{
		hwm:      defaultHWM,
		watchers: make(map[int]int),
		watching: make(map[int]int),
		status:   actorReady,
	}
	f, err := a.pushCall(fn, args)
	if err != nil {
		return 0, err
	}
	a.initialFn = f.name
	a.bornMs = monotonicMillis()
	pid := s.nextPid
	s.nextPid++
	a.pid = pid
	s.actors[pid] = a
	s.ready = append(s.ready, a)
	return pid, nil
}

// ---- send / watch / unwatch ----

// Send доставляет сообщение. Возвращает Result<(), Atom>.
func (s *Scheduler) Send(to int, msg runtime.Value) runtime.Value {
	a, ok := s.actors[to]
	if !ok {
		return runtime.Variant("Ok", runtime.Unit)
	}
	if len(a.mailbox) >= a.hwm {
		return runtime.Variant("Error", runtime.Atom("busy"))
	}
	a.mailbox = append(a.mailbox, msg)
	s.wakeIfBlocked(a)
	return runtime.Variant("Ok", runtime.Unit)
}

func (s *Scheduler) sendDown(to int, down runtime.Value) {
	a, ok := s.actors[to]
	if !ok {
		return
	}
	a.downMsgs = append(a.downMsgs, down)
	s.wakeIfBlocked(a)
}

func (s *Scheduler) wakeIfBlocked(a *Actor) {
	if a.status != actorBlocked {
		return
	}
	// Простаивающий актор сессии не крутится из-за почты: сообщения копятся
	// в ящике до следующего ввода. Иначе каждый send будил бы пустой стек.
	if s.sessionIdle(a) {
		return
	}
	// Ждущего в await будят только ответ, таймер и exit: почта копится
	// в ящике до следующего recv (§12.9).
	if a.awaiting != nil {
		return
	}
	a.status = actorReady
	s.ready = append(s.ready, a)
}

// Watch регистрирует наблюдение. Возвращает ref.
func (s *Scheduler) Watch(watcherPid, targetPid int) int {
	ref := s.nextRef
	s.nextRef++

	target, ok := s.actors[targetPid]
	if !ok {
		s.sendDown(watcherPid, runtime.Tuple(
			runtime.Atom("down"),
			runtime.Value{Kind: runtime.KindRef, Ref: ref},
			runtime.Atom("noproc"),
		))
		return ref
	}

	target.watchers[ref] = watcherPid
	if w, ok := s.actors[watcherPid]; ok {
		w.watching[targetPid] = ref
	}
	return ref
}

// Unwatch снимает наблюдение.
func (s *Scheduler) Unwatch(watcherPid, ref int) {
	for _, target := range s.actors {
		if wp, ok := target.watchers[ref]; ok && wp == watcherPid {
			delete(target.watchers, ref)
			if w, ok := s.actors[watcherPid]; ok {
				delete(w.watching, target.pid)
			}
			return
		}
	}
}

func (s *Scheduler) notifyWatchers(a *Actor, reason runtime.Value) {
	// Имена снимаются и порты закрываются в той же редукции, что и
	// ставится :down (§12.8, §12.12).
	s.dropNames(a.pid)
	s.closeActorPorts(a)
	for ref, watcherPid := range a.watchers {
		s.sendDown(watcherPid, runtime.Tuple(
			runtime.Atom("down"),
			runtime.Value{Kind: runtime.KindRef, Ref: ref},
			reason,
		))
	}
}

// reapActor удаляет завершённый актор из таблицы (I-F9).
func (s *Scheduler) reapActor(a *Actor) {
	s.clearTimer(a)
	delete(s.actors, a.pid)
}

// downRaiseReason — причина :down при actorFailed: значение из *ErrRaise,
// а не a.result (fail() обнуляет result в Unit; I-F10).
func downRaiseReason(a *Actor) runtime.Value {
	val := runtime.Unit
	var rerr *ErrRaise
	if errors.As(a.err, &rerr) {
		val = rerr.Val
	}
	return runtime.Tuple(runtime.Atom("raise"), val)
}

// ---- RunMain ----

// RunMain запускает main как актор без аргументов.
func (s *Scheduler) RunMain(mainFn runtime.Value) (runtime.Value, error) {
	return s.runMain(mainFn, nil)
}

// RunMainWithArgs — вариант с аргументами (Sprint 6.2, REPL).
func (s *Scheduler) RunMainWithArgs(mainFn runtime.Value, args []runtime.Value) (runtime.Value, error) {
	return s.runMain(mainFn, args)
}

func (s *Scheduler) runMain(mainFn runtime.Value, args []runtime.Value) (runtime.Value, error) {
	if s.session {
		return runtime.Unit, errors.New("internal: RunMain during repl session")
	}
	pid, err := s.Spawn(mainFn, args)
	if err != nil {
		return runtime.Unit, err
	}
	s.main = s.actors[pid]
	defer s.finishPorts()

	// Выход (§15.2): main завершился и открытых портов нет; main упал;
	// Sys.halt. Пока от порта или таймера можно ждать событие, пустая
	// ready — ожидание; ждать неоткуда — выход после main или deadlock.
	for {
		if s.halt != nil {
			return runtime.Unit, s.halt
		}
		s.drainInject()
		if m := s.main; m.status == actorDone && len(s.ports) == 0 {
			return m.result, nil
		} else if m.status == actorFailed {
			return runtime.Unit, m.err
		}

		if len(s.ready) == 0 {
			if !s.waitEvent() {
				if m := s.main; m.status == actorDone {
					return m.result, nil
				}
				return runtime.Unit, fmt.Errorf("deadlock: all actors blocked")
			}
			continue
		}

		a := s.ready[0]
		s.ready = s.ready[1:]

		if a.status == actorDone || a.status == actorFailed {
			continue
		}
		a.status = actorReady
		s.runSlice(a)
	}
}

// armTimer взводит таймер recv … after или await актора: новый seq,
// позиция в общей куче.
func (s *Scheduler) armTimer(a *Actor, deadline time.Time) {
	a.recvDeadline = deadline
	a.timerSeq = s.nextSeq
	s.nextSeq++
	if a.timerPos > 0 {
		e := s.timers[a.timerPos-1]
		e.deadline = deadline
		e.seq = a.timerSeq
		heap.Fix(&s.timers, a.timerPos-1)
		return
	}
	heap.Push(&s.timers, &timerEntry{
		deadline: deadline,
		seq:      a.timerSeq,
		actor:    a,
	})
}

// armSend взводит Timer.send_after: запись VM в той же куче, что recv/await.
// ref без слота ответа — await на нём это :type_error (§12.11).
func (s *Scheduler) armSend(pid int, msg runtime.Value, deadline time.Time) runtime.Value {
	ref := s.nextRef
	s.nextRef++
	e := &timerEntry{
		deadline: deadline,
		seq:      s.nextSeq,
		pid:      pid,
		msg:      msg,
		ref:      ref,
	}
	s.nextSeq++
	s.sends[ref] = e
	heap.Push(&s.timers, e)
	return runtime.Value{Kind: runtime.KindRef, Ref: ref}
}

// sendAfter — Timer.send_after(ms, pid, msg). ms не Int, < 0 или вне
// Duration, либо pid не Pid — (:type_error, (:send_after, arg)).
func (s *Scheduler) sendAfter(msVal, pidVal, msg runtime.Value) (runtime.Value, error) {
	ms, ok := nonNegMillis(msVal)
	if !ok {
		return runtime.Unit, typeErr("send_after", msVal)
	}
	if pidVal.Kind != runtime.KindPid {
		return runtime.Unit, typeErr("send_after", pidVal)
	}
	d, ok := recvTimerDuration(ms)
	if !ok {
		return runtime.Unit, typeErr("send_after", msVal)
	}
	return s.armSend(pidVal.Pid, msg, time.Now().Add(d)), nil
}

// cancelTimer — Timer.cancel(ref). true, только если таймер ещё в куче.
func (s *Scheduler) cancelTimer(refVal runtime.Value) (runtime.Value, error) {
	if refVal.Kind != runtime.KindRef {
		return runtime.Unit, typeErr("cancel", refVal)
	}
	e, ok := s.sends[refVal.Ref]
	if !ok || e.pos == 0 {
		return runtime.Bool(false), nil
	}
	delete(s.sends, refVal.Ref)
	heap.Remove(&s.timers, e.pos-1)
	return runtime.Bool(true), nil
}

// nonNegMillis — Int ≥ 0, помещающийся в int64.
func nonNegMillis(v runtime.Value) (int64, bool) {
	if v.Kind != runtime.KindInt {
		return 0, false
	}
	var ms int64
	if v.IsSmall {
		ms = v.SmallInt
	} else if b := v.AsBig(); b.IsInt64() {
		ms = b.Int64()
	} else {
		return 0, false
	}
	if ms < 0 {
		return 0, false
	}
	return ms, true
}

// clearTimer снимает таймер актора (сообщение или ответ пришли раньше,
// таймаут сработал, актор завершился или ожидание прервано) и вместе с ним
// ожидание await.
func (s *Scheduler) clearTimer(a *Actor) {
	a.recvDeadline = time.Time{}
	a.awaiting = nil
	if a.timerPos > 0 {
		heap.Remove(&s.timers, a.timerPos-1)
	}
}

// nextDeadline — ближайший взведённый таймер. Зовётся при пустой ready.
func (s *Scheduler) nextDeadline() time.Time {
	if len(s.timers) == 0 {
		return time.Time{}
	}
	s.timerVisits++
	return s.timers[0].deadline
}

// wakeExpired обслуживает истёкшие таймеры в порядке (deadline, seq)
// (§15.2 G4): recv/await будит актора, send_after доставляет сообщение.
// Снятый с кучи актор сохраняет recvDeadline: по нему RECVTAKE уходит
// в ветку after.
func (s *Scheduler) wakeExpired() {
	now := time.Now()
	for len(s.timers) > 0 {
		s.timerVisits++
		e := s.timers[0]
		if e.deadline.After(now) {
			return
		}
		heap.Pop(&s.timers)
		if e.actor != nil {
			a := e.actor
			if a.status == actorBlocked {
				a.status = actorReady
				s.ready = append(s.ready, a)
			}
			continue
		}
		delete(s.sends, e.ref)
		// Как send: мёртвый pid и полный ящик — сообщение теряется.
		// Отправителя нет, Error(:busy) некому вернуть. Потеря из-за HWM
		// излучается служебным актором (§12.14).
		if isBusyResult(s.Send(e.pid, e.msg)) {
			s.teleTimerHWM(e.pid)
		}
	}
}

// timerEntry — запись общей кучи. actor != nil — таймер recv/await;
// иначе доставка Timer.send_after (pid, msg, ref).
type timerEntry struct {
	deadline time.Time
	seq      uint64
	pos      int
	actor    *Actor
	pid      int
	msg      runtime.Value
	ref      int
}

func (e *timerEntry) setPos(p int) {
	e.pos = p
	if e.actor != nil {
		e.actor.timerPos = p
	}
}

// timerHeap — min-куча по (deadline, seq). Позиция для heap.Fix/Remove
// лежит в timerEntry.pos и, для таймера актора, в Actor.timerPos.
type timerHeap []*timerEntry

func (h timerHeap) Len() int { return len(h) }

func (h timerHeap) Less(i, j int) bool {
	a, b := h[i], h[j]
	if !a.deadline.Equal(b.deadline) {
		return a.deadline.Before(b.deadline)
	}
	return a.seq < b.seq
}

func (h timerHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].setPos(i + 1)
	h[j].setPos(j + 1)
}

func (h *timerHeap) Push(x any) {
	e := x.(*timerEntry)
	e.setPos(len(*h) + 1)
	*h = append(*h, e)
}

func (h *timerHeap) Pop() any {
	old := *h
	n := len(old)
	e := old[n-1]
	old[n-1] = nil
	e.setPos(0)
	*h = old[:n-1]
	return e
}

// monoEpoch — точка отсчёта Time.monotonic_ms. Дедлайны кучи тоже
// считаются от time.Now, а сравнение time.Time использует монотонные
// часы: разности monotonic_ms и сроки таймеров — одна шкала.
var monoEpoch = time.Now()

func monotonicMillis() int64 {
	return time.Since(monoEpoch).Milliseconds()
}

// ---- runSlice ----

func (s *Scheduler) runSlice(a *Actor) {
	if s.sessionInterrupted(a) {
		s.abortSession(a)
		return
	}
	// Вложенный слайс (awaitNested) возвращает active внешнему актору.
	prev := s.active
	s.active = a
	defer func() { s.active = prev }()
	reds := s.reds
	for reds > 0 {
		if a.exitPending() && s.exitStep(a) {
			return
		}
		if len(a.frames) == 0 {
			if s.parkSession(a) {
				return
			}
			a.status = actorDone
			a.result = runtime.Unit
			s.actorDied(a, runtime.Atom("normal"), false, nil)
			return
		}

		f := a.frames[len(a.frames)-1]
		outcome := s.stepFrame(a, f)
		switch outcome {
		case stepDone:
			drop := f.dropResult
			a.popFrame()
			s.burn(a, &reds)
			if len(a.frames) == 0 {
				if s.finishSessionJob(a, nil) {
					return
				}
				a.status = actorDone
				s.actorDied(a, runtime.Atom("normal"), false, nil)
				return
			}
			if !drop {
				caller := a.frames[len(a.frames)-1]
				caller.regs[caller.callDst] = a.result
			}

		case stepExit:
			// обработка — в начале цикла

		case stepFailed:
			if s.halting(a) {
				return
			}
			if a.exit != nil {
				// Ошибка, не пойманная ensure при unwind от exit, причину
				// не меняет: unwind продолжается с текущего места.
				a.exit.unwinding = false
				continue
			}
			if !s.raiseCatchable(a) {
				attachTrace(a)
			}
			if s.tryUnwindRaise(a) {
				s.burn(a, &reds)
				continue
			}
			if s.finishSessionJob(a, a.err) {
				return
			}
			a.status = actorFailed
			reason := downRaiseReason(a)
			var tr []TraceFrame
			crash := false
			var rerr *ErrRaise
			if errors.As(a.err, &rerr) {
				crash = true
				tr = rerr.Trace
			}
			s.actorDied(a, reason, crash, tr)
			return

		case stepBlock:
			a.status = actorBlocked
			a.shed()
			return

		case stepYield, stepContinue:
			s.burn(a, &reds)
		}
	}

	a.status = actorReady
	s.ready = append(s.ready, a)
}

// ---- stepFrame ----

// stepFrame исполняет верхний кадр актора. Кадр с cleanups (§5.1) не
// завершается сразу: RETURN и непойманный raise превращают его в drain.
func (s *Scheduler) stepFrame(a *Actor, f *Frame) stepOutcome {
	out := s.execFrame(a, f)
	if len(f.cleanups) == 0 {
		return out
	}
	switch out {
	case stepDone:
		s.startDrain(a, f, a.result, false)
		return stepContinue
	case stepFailed:
		var rerr *ErrRaise
		if a.exit == nil && errors.As(a.err, &rerr) {
			s.startDrain(a, f, rerr.Val, true)
			return stepContinue
		}
	}
	return out
}

func (s *Scheduler) execFrame(a *Actor, f *Frame) stepOutcome {
	if f.cont != nil {
		return s.stepNative(a, f)
	}
	regs := f.regs
	code := f.chunk.Code
	consts := f.chunk.Constants

	fail := func(err error) stepOutcome {
		a.err = err
		a.result = runtime.Unit
		return stepFailed
	}

	for f.ip < len(code) {
		in := code[f.ip]
		op := in.Op()

		switch op {
		case LOADK:
			regs[in.A()] = consts[in.Bx()]
			f.ip++

		case MOVE:
			regs[in.A()] = regs[in.B()]
			f.ip++

		case GETGLOBAL:
			name := consts[in.Bx()].Str
			g, ok := s.vm.globals[name]
			if !ok {
				return fail(fmt.Errorf("undefined: %s", name))
			}
			regs[in.A()] = g
			f.ip++

		case SETGLOBAL:
			name := consts[in.Bx()].Str
			s.vm.globals[name] = regs[in.A()]
			f.ip++

		case GETUPVAL:
			idx := in.B()
			if idx >= len(f.captures) {
				return fail(fmt.Errorf("internal: upvalue %d out of range", idx))
			}
			regs[in.A()] = f.captures[idx]
			f.ip++

		case ADD:
			r, err := add(regs[in.B()], regs[in.C()])
			if err != nil {
				if f.catch(err) {
					continue
				}
				return fail(err)
			}
			regs[in.A()] = r
			f.ip++

		case CONCAT:
			// компилятор кладёт сюда интерполяцию (всегда Str) и исходный
			// оператор `<>` — операнды последнего произвольны, поэтому
			// несоответствие типа — ловимый (:type_error, (:concat, (a, b))).
			if regs[in.B()].Kind != runtime.KindStr || regs[in.C()].Kind != runtime.KindStr {
				err := arithErr(regs[in.B()], regs[in.C()], "concat")
				if f.catch(err) {
					continue
				}
				return fail(err)
			}
			regs[in.A()] = runtime.Str(regs[in.B()].Str + regs[in.C()].Str)
			s.charge(a, &regs[in.A()])
			f.ip++

		case SUB:
			r, err := sub(regs[in.B()], regs[in.C()])
			if err != nil {
				if f.catch(err) {
					continue
				}
				return fail(err)
			}
			regs[in.A()] = r
			f.ip++

		case MUL:
			r, err := mul(regs[in.B()], regs[in.C()])
			if err != nil {
				if f.catch(err) {
					continue
				}
				return fail(err)
			}
			regs[in.A()] = r
			f.ip++

		case DIV:
			r, err := div(regs[in.B()], regs[in.C()])
			if err != nil {
				if f.catch(err) {
					continue
				}
				return fail(err)
			}
			regs[in.A()] = r
			f.ip++

		case INTDIV:
			r, err := intDiv(regs[in.B()], regs[in.C()])
			if err != nil {
				if f.catch(err) {
					continue
				}
				return fail(err)
			}
			regs[in.A()] = r
			f.ip++

		case REM:
			r, err := rem(regs[in.B()], regs[in.C()])
			if err != nil {
				if f.catch(err) {
					continue
				}
				return fail(err)
			}
			regs[in.A()] = r
			f.ip++

		case POW:
			r, err := pow(regs[in.B()], regs[in.C()])
			if err != nil {
				if f.catch(err) {
					continue
				}
				return fail(err)
			}
			regs[in.A()] = r
			f.ip++

		case NEG:
			r, err := neg(regs[in.B()])
			if err != nil {
				if f.catch(err) {
					continue
				}
				return fail(err)
			}
			regs[in.A()] = r
			f.ip++

		case NOT:
			v := regs[in.B()]
			if v.Kind != runtime.KindBool {
				err := typeErr("not", v)
				if f.catch(err) {
					continue
				}
				return fail(err)
			}
			regs[in.A()] = runtime.Bool(!v.Bool)
			f.ip++

		case EQ, NEQ:
			av := regs[in.B()]
			bv := regs[in.C()]
			if err := checkMixedEq(av, bv); err != nil {
				if f.catch(err) {
					continue
				}
				return fail(err)
			}
			eq := runtime.Equal(av, bv)
			if op == NEQ {
				eq = !eq
			}
			regs[in.A()] = runtime.Bool(eq)
			f.ip++

		case LT, GT, LE, GE:
			av := regs[in.B()]
			bv := regs[in.C()]
			if err := checkMixedCmp(av, bv); err != nil {
				if f.catch(err) {
					continue
				}
				return fail(err)
			}
			if runtime.IsNaNOperand(av, bv) {
				// NaN не упорядочен: <, >, <=, >= — false (§7.4).
				regs[in.A()] = runtime.Bool(false)
				f.ip++
				break
			}
			c, err := runtime.Compare(av, bv)
			if err != nil {
				// Несравнимые значения (§7.4: Function, вложенный
				// Decimal×Float) — ловимый raise с исходными операндами.
				err = typeErr("compare", runtime.Tuple(av, bv))
				if f.catch(err) {
					continue
				}
				return fail(err)
			}
			var res bool
			switch op {
			case LT:
				res = c < 0
			case GT:
				res = c > 0
			case LE:
				res = c <= 0
			case GE:
				res = c >= 0
			}
			regs[in.A()] = runtime.Bool(res)
			f.ip++

		case JMP:
			f.ip += 1 + in.SBx()

		case JMPIFNOT:
			v := regs[in.A()]
			if v.Kind != runtime.KindBool {
				err := notBoolErr(v)
				if f.catch(err) {
					continue
				}
				return fail(err)
			}
			if !v.Bool {
				f.ip += 1 + in.SBx()
			} else {
				f.ip++
			}

		case JMPIF:
			v := regs[in.A()]
			if v.Kind != runtime.KindBool {
				err := notBoolErr(v)
				if f.catch(err) {
					continue
				}
				return fail(err)
			}
			if v.Bool {
				f.ip += 1 + in.SBx()
			} else {
				f.ip++
			}

		case CALL, CALLSPREAD:
			base, argc, dst := in.A(), in.B(), in.C()
			cv := regs[base]
			args := regs[base+1 : base+1+argc]
			if op == CALLSPREAD {
				var serr error
				if args, serr = spreadArgs(args); serr != nil {
					if f.catch(serr) {
						continue
					}
					return fail(serr)
				}
			}

			nf, r, err := s.enterCall(a, cv, args)
			if err != nil {
				if f.catch(err) {
					continue
				}
				return fail(err)
			}
			if nf == nil {
				regs[dst] = r
				f.ip++
				continue
			}
			f.callDst = dst
			f.ip++
			return stepContinue

		case TAILCALL, TAILCALLSPREAD, TAILCALLENS:
			base, argc := in.A(), in.B()
			cv := regs[base]
			args := regs[base+1 : base+1+argc]
			if op == TAILCALLENS {
				// Запись — до разбора callee: его ошибку ловит этот уровень.
				ens := regs[base+1+argc : base+1+argc+in.C()]
				if err := s.pushCleanup(a, f, ens); err != nil {
					return fail(err)
				}
			}
			if op == TAILCALLSPREAD {
				var serr error
				if args, serr = spreadArgs(args); serr != nil {
					return fail(serr)
				}
				argc = len(args)
			}

			if len(f.handlers) != 0 {
				return fail(errors.New("internal: TAILCALL under active trap"))
			}

			if cv.Kind == runtime.KindFunction && cv.Func != nil && cv.Func.IsNative {
				if ar := cv.Func.Arity; ar >= 0 && ar != argc {
					return fail(functionClause(args))
				}
				fresh := append([]runtime.Value(nil), args...)
				if cv.Func == s.vm.teleEmit {
					run, r, err := s.prepEmit(fresh)
					if err != nil {
						return fail(err)
					}
					if run == nil {
						s.charge(a, &r)
						a.result = r
						return stepDone
					}
					// Кадр становится кадром emit: результат — туда же, куда ушёл бы у f.
					clear(f.regs[:cap(f.regs)])
					f.regs, f.chunk, f.name, f.captures, f.ip, f.cont =
						f.regs[:1], nil, "Telemetry.emit", nil, 0, run
					return stepContinue
				}
				if start, ok := s.vm.resumable[cv.Func]; ok {
					// Кадр становится кадром натива: колбэки пойдут поверх
					// него, результат — туда же, куда ушёл бы у f.
					k, err := start(fresh)
					if err != nil {
						return fail(err)
					}
					clear(f.regs[:cap(f.regs)])
					f.regs, f.chunk, f.name, f.captures, f.ip, f.cont =
						f.regs[:1], nil, cv.Func.Name, nil, 0, k
					return stepContinue
				}
				r, err := cv.Func.Native(s.vm, fresh)
				if err != nil {
					return fail(err)
				}
				s.charge(a, &r)
				a.result = r
				return stepDone
			}

			c, err := resolveCallee(cv)
			if err == nil {
				err = checkArity(c, args)
			}
			if err != nil {
				return fail(err)
			}

			nregs := c.chunk.NumRegs
			if nregs <= cap(f.regs) {
				regs = f.regs[:nregs]
				bindArgs(regs, c.chunk, args) // args ⊂ старого regs: memmove-безопасно
				clear(regs[c.chunk.NumParams:cap(regs)])
			} else {
				// Окно кадра — вершина стека: снимаем его и берём больше.
				// Новое окно либо начинается там же (старое ⊂ новое), либо
				// не пересекается со старым; args читаются до обнуления.
				old := f.regs
				a.regs.pop(old)
				regs = a.regs.alloc(nregs)
				bindArgs(regs, c.chunk, args)
				if &regs[0] == &old[0] {
					clear(regs[c.chunk.NumParams:])
				} else {
					clear(old[:cap(old)])
				}
			}
			f.regs, f.chunk, f.name, f.captures, f.ip =
				regs, c.chunk, c.name, c.captures, 0
			// f.callDst НЕ трогаем: результат уйдёт туда же, куда ушёл бы
			// у исходного вызова.
			return stepContinue

		case RETURN:
			a.result = regs[in.A()]
			return stepDone

		case TUPLE, LIST, VECTOR:
			n := in.C()
			base := in.B()
			elems := make([]runtime.Value, n)
			copy(elems, regs[base:base+n])
			switch op {
			case TUPLE:
				regs[in.A()] = runtime.Tuple(elems...)
			case LIST:
				regs[in.A()] = runtime.List(elems...)
			case VECTOR:
				regs[in.A()] = runtime.Vector(elems...)
			}
			s.charge(a, &regs[in.A()])
			f.ip++

		case MAP:
			n := in.C()
			base := in.B()
			// Повторный ключ (KeyEqual: 1 и 1.0 — один ключ, §4.8)
			// перезаписывает значение, как Map.put (§5.2).
			m := runtime.Map(nil)
			for i := 0; i < n; i++ {
				m = m.MapPut(regs[base+2*i], regs[base+2*i+1])
			}
			regs[in.A()] = m
			s.charge(a, &regs[in.A()])
			f.ip++

		case LISTSPREAD, VECSPREAD:
			b, n := in.B(), in.C()
			end := b + 2*n
			if end > len(regs) {
				return fail(fmt.Errorf("internal: %s window", op))
			}
			r, shared, err := vmSpreadSeq(regs[b:end], op == VECSPREAD)
			if err != nil {
				if f.catch(err) {
					continue
				}
				return fail(err)
			}
			regs[in.A()] = r
			// Разделённый хвост списка учтён, когда его строили.
			s.chargeBytes(a, sizeEstimate(&r)-int64(shared)*valueSize)
			f.ip++

		case MAPSPREAD:
			b, n := in.B(), in.C()
			end := b + 3*n
			if end > len(regs) {
				return fail(fmt.Errorf("internal: MAPSPREAD window"))
			}
			segs := append([]runtime.Value(nil), regs[b:end]...)
			r, err := vmSpreadMap(segs)
			if err != nil {
				if f.catch(err) {
					continue
				}
				return fail(err)
			}
			regs[in.A()] = r
			s.charge(a, &r)
			f.ip++

		case RANGE:
			r, err := vmMakeRange(regs[in.B()], regs[in.C()])
			if err != nil {
				if f.catch(err) {
					continue
				}
				return fail(err)
			}
			regs[in.A()] = r
			f.ip++

		case INDEX:
			r, err := vmIndex(regs[in.B()], regs[in.C()])
			if err != nil {
				if f.catch(err) {
					continue
				}
				return fail(err)
			}
			regs[in.A()] = r
			f.ip++

		case RECORD:
			b, n := in.B(), in.C()
			r, err := vmMakeRecord(regs[b], regs[b+1:b+1+n])
			if err != nil {
				if f.catch(err) {
					continue
				}
				return fail(err)
			}
			regs[in.A()] = r
			s.charge(a, &r)
			f.ip++

		case GETFIELD:
			r, err := vmGetField(regs[in.B()], regs[in.C()])
			if err != nil {
				if f.catch(err) {
					continue
				}
				return fail(err)
			}
			regs[in.A()] = r
			f.ip++

		case RAISE:
			rerr := &ErrRaise{Val: regs[in.A()]}
			if f.catch(rerr) {
				continue
			}
			return fail(rerr)

		case MAKECLOSURE:
			fnVal := regs[in.B()]
			if fnVal.Kind != runtime.KindFunction || fnVal.Func == nil {
				return fail(fmt.Errorf("internal: MAKECLOSURE expects function"))
			}
			n := in.C()
			caps := make([]runtime.Value, n)
			copy(caps, regs[in.B()+1:in.B()+1+n])
			regs[in.A()] = runtime.MakeClosure(
				fnVal.Func.Name, fnVal.Func.Arity, fnVal.Func.Body, caps)
			s.charge(a, &regs[in.A()])
			f.ip++

		case TRAPBEGIN, TRAPENSURE:
			f.handlers = append(f.handlers, trapHandler{
				ip:     f.ip + 1 + in.SBx(),
				errReg: in.A(),
				ensure: op == TRAPENSURE,
			})
			f.ip++

		case ENSEND:
			f.ip++
			if a.atEnsureEnd(f) {
				a.exit.unwinding = false
				return stepExit
			}

		case TRAPEND:
			if len(f.handlers) == 0 {
				return fail(fmt.Errorf("internal: TRAPEND without handler"))
			}
			f.handlers = f.handlers[:len(f.handlers)-1]
			f.ip++

		case MAKEOK:
			regs[in.A()] = runtime.Variant("Ok", regs[in.B()])
			s.charge(a, &regs[in.A()])
			f.ip++

		case MAKEERROR:
			regs[in.A()] = runtime.Variant("Error", regs[in.B()])
			s.charge(a, &regs[in.A()])
			f.ip++

		// ---- Акторные опкоды (§6) ----

		case SPAWN:
			base, mode := in.A(), in.C()&spawnModeMask
			fn := regs[in.B()]
			var limReds, limAlloc int64
			var err error
			if in.C()&SpawnLimits != 0 {
				limReds, limAlloc, err = parseLimits(regs[in.B()+1])
			}
			var pid int
			if err == nil {
				pid, err = s.Spawn(fn, nil)
			}
			if err != nil {
				if f.catch(err) {
					continue
				}
				return fail(err)
			}
			child := s.actors[pid]
			child.limitReds, child.limitAlloc = limReds, limAlloc
			pidVal := runtime.Value{Kind: runtime.KindPid, Pid: pid}
			switch mode {
			case 1: // linked
				s.Watch(a.pid, pid)
				regs[base] = pidVal
			case 2: // watched: наблюдение взведено до первой редукции ребёнка
				ref := s.Watch(a.pid, pid)
				regs[base] = runtime.Tuple(pidVal, runtime.Value{Kind: runtime.KindRef, Ref: ref})
			default:
				regs[base] = pidVal
			}
			f.ip++
			if s.teleInlineSpawn(a, child) {
				return stepContinue
			}

		case EXIT:
			pidVal := regs[in.B()]
			if pidVal.Kind != runtime.KindPid {
				err := typeErr("exit", pidVal)
				if f.catch(err) {
					continue
				}
				return fail(err)
			}
			regs[in.A()] = runtime.Variant("Ok", runtime.Unit)
			f.ip++
			if pidVal.Pid == a.pid {
				s.signalExit(a, regs[in.C()])
				return stepExit
			}
			s.Exit(pidVal.Pid, regs[in.C()])

		case REGISTER:
			pidVal := regs[in.C()]
			if pidVal.Kind != runtime.KindPid {
				err := typeErr("register", pidVal)
				if f.catch(err) {
					continue
				}
				return fail(err)
			}
			regs[in.A()] = s.Register(regs[in.B()], pidVal.Pid)
			f.ip++

		case UNREGISTER:
			s.Unregister(regs[in.B()])
			regs[in.A()] = runtime.Unit
			f.ip++

		case WHEREIS:
			regs[in.A()] = s.Whereis(regs[in.B()])
			f.ip++

		case SEND:
			pidVal := regs[in.B()]
			msg := regs[in.C()]
			if pidVal.Kind != runtime.KindPid {
				err := typeErr("send", pidVal)
				if f.catch(err) {
					continue
				}
				return fail(err)
			}
			regs[in.A()] = s.Send(pidVal.Pid, msg)
			f.ip++
			if isBusyResult(regs[in.A()]) && s.teleInlineHWM(a, pidVal.Pid) {
				return stepContinue
			}

		case SELF:
			regs[in.A()] = runtime.Value{Kind: runtime.KindPid, Pid: a.pid}
			f.ip++

		case MAKEREF:
			regs[in.A()] = s.makeRef(a)
			f.ip++

		case AWAIT:
			res, block, err := s.await(a, regs[in.B()], regs[in.C()])
			if err != nil {
				if f.catch(err) {
					continue
				}
				return fail(err)
			}
			if block {
				return stepBlock
			}
			regs[in.A()] = res
			f.ip++

		case REPLY:
			b := in.B()
			pidVal, refVal := regs[b], regs[b+1]
			if pidVal.Kind != runtime.KindPid || refVal.Kind != runtime.KindRef {
				bad := pidVal
				if pidVal.Kind == runtime.KindPid {
					bad = refVal
				}
				err := typeErr("reply", bad)
				if f.catch(err) {
					continue
				}
				return fail(err)
			}
			s.Reply(pidVal.Pid, refVal, regs[b+2])
			regs[in.A()] = runtime.Unit
			f.ip++

		case WATCH:
			pidVal := regs[in.B()]
			if pidVal.Kind != runtime.KindPid {
				err := typeErr("watch", pidVal)
				if f.catch(err) {
					continue
				}
				return fail(err)
			}
			ref := s.Watch(a.pid, pidVal.Pid)
			regs[in.A()] = runtime.Value{Kind: runtime.KindRef, Ref: ref}
			f.ip++

		case UNWATCH:
			refVal := regs[in.B()]
			if refVal.Kind != runtime.KindRef {
				err := typeErr("unwatch", refVal)
				if f.catch(err) {
					continue
				}
				return fail(err)
			}
			s.Unwatch(a.pid, refVal.Ref)
			regs[in.A()] = runtime.Unit
			f.ip++

		case MAILBOXSIZE:
			pidVal := regs[in.B()]
			if pidVal.Kind != runtime.KindPid {
				err := typeErr("mailbox_size", pidVal)
				if f.catch(err) {
					continue
				}
				return fail(err)
			}
			if t, ok := s.actors[pidVal.Pid]; ok {
				regs[in.A()] = runtime.Int(int64(len(t.mailbox)))
			} else {
				regs[in.A()] = runtime.Int(0)
			}
			f.ip++

		case RECVTIMER:
			msVal := regs[in.A()]
			if msVal.Kind != runtime.KindInt {
				err := typeErr("after", msVal)
				if f.catch(err) {
					continue
				}
				return fail(err)
			}
			var ms int64
			if msVal.IsSmall {
				ms = msVal.SmallInt
			} else {
				if !msVal.AsBig().IsInt64() {
					err := typeErr("after", msVal)
					if f.catch(err) {
						continue
					}
					return fail(err)
				}
				ms = msVal.AsBig().Int64()
			}
			d, ok := recvTimerDuration(ms)
			if !ok {
				err := typeErr("after", msVal)
				if f.catch(err) {
					continue
				}
				return fail(err)
			}
			s.armTimer(a, time.Now().Add(d))
			f.ip++

		case RECVTAKE:
			slot := in.A()
			sbx := in.SBx()

			if len(a.downMsgs) > 0 {
				msg := a.downMsgs[0]
				a.downMsgs = a.downMsgs[1:]
				s.clearTimer(a)
				a.newTurn()
				regs[slot] = msg
				f.ip++
				continue
			}
			if len(a.mailbox) > 0 {
				msg := a.mailbox[0]
				a.mailbox = a.mailbox[1:]
				s.clearTimer(a)
				a.newTurn()
				regs[slot] = msg
				f.ip++
				continue
			}

			if !a.recvDeadline.IsZero() && !time.Now().Before(a.recvDeadline) {
				s.clearTimer(a)
				if sbx != 0 {
					a.newTurn()
					f.ip += 1 + sbx
					continue
				}
				// after не задан: проваливаемся к stepBlock.
			}

			return stepBlock

		case MATCHLOCAL:
			slot := in.A()
			patIdx := in.Bx()
			if patIdx >= len(f.chunk.Patterns) {
				return fail(fmt.Errorf(
					"internal: pattern %d out of range", patIdx))
			}
			pat := f.chunk.Patterns[patIdx]
			if MatchPattern(regs[slot], pat, regs) {
				f.ip += 2 // пропустить MATCHLOCAL и следующую JMP
			} else {
				f.ip++ // встать на JMP (fail-переход)
			}

		case YIELD:
			f.ip++
			return stepYield

		default:
			return fail(fmt.Errorf(
				"internal: unknown opcode %d at %d in %s", op, f.ip, f.name))
		}
	}

	return fail(fmt.Errorf("internal: fell off end of %s", f.name))
}

// stepNative — ход кадра возобновляемого натива (T-58): отдаёт cont
// результат предыдущего колбэка из regs[0]. Байткод-колбэк (и вложенный
// возобновляемый натив) уходит новым кадром поверх — так он тратит
// редукции и может быть вытеснен (G3); обычный натив вызывается на месте.
// Raise колбэка всплывает сквозь этот кадр (handlers у него нет) к trap
// вызывающего, как было при синхронном вызове.
func (s *Scheduler) stepNative(a *Actor, f *Frame) stepOutcome {
	fail := func(err error) stepOutcome {
		a.err = err
		a.result = runtime.Unit
		return stepFailed
	}

	for {
		st, err := f.cont.resume(f.regs[0])
		if err != nil {
			return fail(err)
		}
		if st.block {
			return stepBlock
		}
		if st.exit {
			// drain в режиме exit исполнил все ensure: unwind продолжается.
			a.exit.unwinding = false
			return stepExit
		}
		if st.done {
			s.charge(a, &st.res)
			a.result = st.res
			return stepDone
		}
		nf, r, err := s.enterCall(a, st.fn, st.args)
		if err != nil {
			if d, ok := f.cont.(*drainRun); ok {
				var rerr *ErrRaise
				if errors.As(err, &rerr) {
					d.catch(rerr.Val)
					continue
				}
			}
			if run, ok := f.cont.(*teleRun); ok {
				var rerr *ErrRaise
				if errors.As(err, &rerr) {
					before := len(a.frames)
					s.catchTele(a, run, rerr.Val)
					if len(a.frames) > before {
						return stepContinue
					}
					continue
				}
			}
			return fail(err)
		}
		if nf == nil {
			f.regs[0] = r
			continue
		}
		f.callDst = 0
		return stepContinue
	}
}

// recvTimerDuration converts after-ms to a Duration without overflowing
// int64 nanoseconds. Rejects ms outside [MinInt64/1e6, MaxInt64/1e6].
func recvTimerDuration(ms int64) (time.Duration, bool) {
	const unit = int64(time.Millisecond)
	if ms > math.MaxInt64/unit || ms < math.MinInt64/unit {
		return 0, false
	}
	return time.Duration(ms) * time.Millisecond, true
}

// ---- helpers for RANGE / INDEX ----

func vmMakeRange(startV, endV runtime.Value) (runtime.Value, error) {
	if startV.Kind != runtime.KindInt || endV.Kind != runtime.KindInt {
		return runtime.Unit, typeErr("range", runtime.Tuple(startV, endV))
	}
	sb := startV.AsBig()
	eb := endV.AsBig()
	if !sb.IsInt64() || !eb.IsInt64() {
		return runtime.Unit, &ErrRaise{Val: runtime.Tuple(
			runtime.Atom("range_error"),
			runtime.Tuple(startV, endV))}
	}
	return runtime.Range(sb.Int64(), eb.Int64()), nil
}

func vmIndex(obj, idx runtime.Value) (runtime.Value, error) {
	switch obj.Kind {
	case runtime.KindList:
		i, err := indexToInt(idx)
		if err != nil {
			return runtime.Unit, err
		}
		if i < 0 || i >= int64(obj.Len()) {
			return runtime.Unit, &ErrRaise{Val: runtime.Tuple(
				runtime.Atom("index_out_of_bounds"),
				runtime.Tuple(idx, runtime.Int(int64(obj.Len()))))}
		}
		return obj.At(int(i)), nil

	case runtime.KindVector:
		i, err := indexToInt(idx)
		if err != nil {
			return runtime.Unit, err
		}
		if i < 0 || i >= int64(obj.Len()) {
			return runtime.Unit, &ErrRaise{Val: runtime.Tuple(
				runtime.Atom("index_out_of_bounds"),
				runtime.Tuple(idx, runtime.Int(int64(obj.Len()))))}
		}
		return obj.At(int(i)), nil

	case runtime.KindStr:
		i, err := indexToInt(idx)
		if err != nil {
			return runtime.Unit, err
		}
		runes := []rune(obj.Str)
		if i < 0 || i >= int64(len(runes)) {
			return runtime.Unit, &ErrRaise{Val: runtime.Tuple(
				runtime.Atom("index_out_of_bounds"),
				runtime.Tuple(idx, runtime.Int(int64(len(runes)))))}
		}
		return runtime.Str(string(runes[i])), nil

	case runtime.KindBytes:
		i, err := indexToInt(idx)
		if err != nil {
			return runtime.Unit, err
		}
		if i < 0 || i >= int64(len(obj.Bytes)) {
			return runtime.Unit, &ErrRaise{Val: runtime.Tuple(
				runtime.Atom("index_out_of_bounds"),
				runtime.Tuple(idx, runtime.Int(int64(len(obj.Bytes)))))}
		}
		return runtime.Int(int64(obj.Bytes[i])), nil

	case runtime.KindTuple:
		i, err := indexToInt(idx)
		if err != nil {
			return runtime.Unit, err
		}
		if i < 0 || i >= int64(len(obj.Tuple)) {
			return runtime.Unit, &ErrRaise{Val: runtime.Tuple(
				runtime.Atom("index_out_of_bounds"),
				runtime.Tuple(idx, runtime.Int(int64(len(obj.Tuple)))))}
		}
		return obj.Tuple[i], nil

	case runtime.KindMap:
		if v, ok := obj.MapGet(idx); ok {
			return runtime.Variant("Some", v), nil
		}
		return runtime.Variant("None"), nil
	}
	return runtime.Unit, typeErr("index", obj)
}

// vmSpreadSeq собирает список или вектор из сегментов (§5.2).
// Сегмент — пара (Bool, значение): false — один элемент, true — спред.
// Список принимает только List; вектор — List или Vector. Последний
// спред списка становится хвостом без копирования: `[x, ..xs]` — O(1)
// (§4.2); shared — длина такого хвоста. segs не изменяется.
func vmSpreadSeq(segs []runtime.Value, vector bool) (_ runtime.Value, shared int, _ error) {
	if len(segs)%2 != 0 {
		return runtime.Unit, 0, fmt.Errorf("internal: spread seq: %d regs", len(segs))
	}
	if vector {
		out, err := spreadElems(nil, segs, true)
		if err != nil {
			return runtime.Unit, 0, err
		}
		return runtime.Vector(out...), 0, nil
	}
	tail := runtime.List()
	if n := len(segs); n > 0 && segs[n-2].Kind == runtime.KindBool &&
		segs[n-2].Bool && segs[n-1].Kind == runtime.KindList {
		tail, segs = segs[n-1], segs[:n-2]
	}
	// Головы копирует ListPrepend: буфер на стеке, пока их немного.
	var buf [4]runtime.Value
	out, err := spreadElems(buf[:0], segs, false)
	if err != nil {
		return runtime.Unit, 0, err
	}
	return runtime.ListPrepend(out, tail), tail.Len(), nil
}

// spreadElems дописывает к out элементы сегментов segs (см. vmSpreadSeq).
func spreadElems(out, segs []runtime.Value, vector bool) ([]runtime.Value, error) {
	for i := 0; i < len(segs); i += 2 {
		tag, val := segs[i], segs[i+1]
		if tag.Kind != runtime.KindBool {
			return nil, fmt.Errorf("internal: spread tag %s", tag.Inspect())
		}
		if !tag.Bool {
			out = append(out, val)
			continue
		}
		switch val.Kind {
		case runtime.KindList:
			for e := range val.Items() {
				out = append(out, e)
			}
		case runtime.KindVector:
			if !vector {
				return nil, typeErr("spread", val)
			}
			out = append(out, val.Elems()...)
		default:
			return nil, typeErr("spread", val)
		}
	}
	return out, nil
}

// vmSpreadMap собирает мапу из сегментов (§5.2, §4.5).
// Сегмент — тройка: Bool, затем либо мапа (true, третье значение игнорируется),
// либо пара ключ/значение (false). Правые ключи перекрывают левые.
func vmSpreadMap(segs []runtime.Value) (runtime.Value, error) {
	if len(segs)%3 != 0 {
		return runtime.Unit, fmt.Errorf("internal: spread map: %d regs", len(segs))
	}
	m := runtime.Map(nil)
	for i := 0; i < len(segs); i += 3 {
		tag := segs[i]
		if tag.Kind != runtime.KindBool {
			return runtime.Unit, fmt.Errorf("internal: spread tag %s", tag.Inspect())
		}
		if tag.Bool {
			src := segs[i+1]
			if src.Kind != runtime.KindMap {
				return runtime.Unit, typeErr("spread", src)
			}
			if i == 0 {
				// Первый спред — основа без копирования (§4.5).
				m = src
				continue
			}
			for _, e := range src.Entries() {
				m = m.MapPut(e.Key, e.Val)
			}
			continue
		}
		m = m.MapPut(segs[i+1], segs[i+2])
	}
	return m, nil
}

// ---- helpers for RECORD / GETFIELD (T-73, §4.7) ----

// vmMakeRecord собирает запись по форме (см. RECORD). Поля номинальной
// записи идут в порядке декларации, анонимной — в порядке первого
// появления; повторное поле (спред, затем явное) перезаписывает значение.
// Спред в номинальную запись поля, которого нет в декларации, —
// raise (:field_error, (:field, "Type")). Литерал без имени типа со
// спредом — record update (§4.7): вид первого спреда; поле не из его
// типа — raise (:no_field, (:field, rec)), rec — первый спред.
func vmMakeRecord(shape runtime.Value, vals []runtime.Value) (runtime.Value, error) {
	if shape.Kind != runtime.KindTuple || len(shape.Tuple) != 3 ||
		shape.Tuple[0].Kind != runtime.KindStr ||
		shape.Tuple[1].Kind != runtime.KindTuple ||
		shape.Tuple[2].Kind != runtime.KindTuple ||
		len(shape.Tuple[2].Tuple) != len(vals) {
		return runtime.Unit, fmt.Errorf("internal: RECORD: bad shape %s", shape.Inspect())
	}
	typ, declared, slots := shape.Tuple[0].Str, shape.Tuple[1].Tuple, shape.Tuple[2].Tuple

	var base runtime.Value
	update := false
	if typ == "" {
		if i := slices.IndexFunc(slots, func(s runtime.Value) bool { return s.Str == ".." }); i >= 0 {
			if src := vals[i]; src.Kind == runtime.KindRecord && src.Record.Type != "" {
				typ, declared, base, update = src.Record.Type, src.Record.Declared, src, true
			}
		}
	}

	var fields []runtime.RecordField
	put := func(name string, v runtime.Value) {
		for i := range fields {
			if fields[i].Name == name {
				fields[i].Val = v
				return
			}
		}
		fields = append(fields, runtime.RecordField{Name: name, Val: v})
	}
	isDeclared := func(name string) bool {
		for _, d := range declared {
			if d.Str == name {
				return true
			}
		}
		return false
	}
	check := func(name string) error {
		switch {
		case typ == "" || isDeclared(name):
			return nil
		case update:
			return &ErrRaise{Val: runtime.Tuple(
				runtime.Atom("no_field"),
				runtime.Tuple(runtime.Atom(name), base))}
		default:
			return &ErrRaise{Val: runtime.Tuple(
				runtime.Atom("field_error"),
				runtime.Tuple(runtime.Atom(name), runtime.Str(typ)))}
		}
	}

	for i, slot := range slots {
		if slot.Str != ".." {
			if update {
				if err := check(slot.Str); err != nil {
					return runtime.Unit, err
				}
			}
			put(slot.Str, vals[i])
			continue
		}
		src := vals[i]
		if src.Kind != runtime.KindRecord {
			return runtime.Unit, typeErr("record_spread", src)
		}
		for _, f := range src.Record.Fields {
			if err := check(f.Name); err != nil {
				return runtime.Unit, err
			}
			put(f.Name, f.Val)
		}
	}

	if typ == "" {
		return runtime.Record("", fields), nil
	}
	ordered := make([]runtime.RecordField, 0, len(fields))
	for _, d := range declared {
		for _, f := range fields {
			if f.Name == d.Str {
				ordered = append(ordered, f)
				break
			}
		}
	}
	r := runtime.Record(typ, ordered)
	r.Record.Declared = declared
	return r, nil
}

// vmGetField — доступ к полю записи. Отсутствующее поле — raise
// (:no_field, (:field, record)).
func vmGetField(obj, name runtime.Value) (runtime.Value, error) {
	if name.Kind != runtime.KindStr {
		return runtime.Unit, fmt.Errorf("internal: GETFIELD: field name %s", name.Inspect())
	}
	if obj.Kind != runtime.KindRecord {
		return runtime.Unit, typeErr("field", runtime.Tuple(name, obj))
	}
	if v, ok := obj.Record.Get(name.Str); ok {
		return v, nil
	}
	return runtime.Unit, &ErrRaise{Val: runtime.Tuple(
		runtime.Atom("no_field"),
		runtime.Tuple(runtime.Atom(name.Str), obj))}
}

func indexToInt(v runtime.Value) (int64, error) {
	if v.Kind != runtime.KindInt {
		return 0, typeErr("index_key", v)
	}
	if v.IsSmall {
		return v.SmallInt, nil
	}
	if !v.AsBig().IsInt64() {
		return 0, typeErr("index_key", v)
	}
	return v.AsBig().Int64(), nil
}

// ---- callSync ----

func (s *Scheduler) callSync(fn runtime.Value, args []runtime.Value) (runtime.Value, error) {
	tmp := &Actor{
		pid:      -1,
		hwm:      defaultHWM,
		watchers: make(map[int]int),
		watching: make(map[int]int),
		status:   actorReady,
	}
	if _, err := tmp.pushCall(fn, args); err != nil {
		return runtime.Unit, err
	}

	prev := s.active
	s.active = tmp
	defer func() { s.active = prev }()

	for {
		if len(tmp.frames) == 0 {
			return tmp.result, nil
		}
		top := tmp.frames[len(tmp.frames)-1]
		outcome := s.stepFrame(tmp, top)
		switch outcome {
		case stepDone:
			tmp.popFrame()
			if len(tmp.frames) == 0 {
				return tmp.result, nil
			}
			caller := tmp.frames[len(tmp.frames)-1]
			caller.regs[caller.callDst] = tmp.result

		case stepFailed:
			if s.tryUnwindRaise(tmp) {
				continue
			}
			return runtime.Unit, tmp.err

		case stepBlock:
			return runtime.Unit,
				fmt.Errorf("internal: recv in synchronous call context")

		case stepExit:
			// Синхронный вызов вне планировщика: ensure не исполняются.
			return runtime.Unit, &ErrExit{Reason: tmp.exit.reason}

		case stepYield, stepContinue:
			// продолжаем
		}
	}
}

// ---- tryUnwindRaise ----

// raiseCatchable сообщает, поймает ли raise какой-либо trap в кадрах
// родителей верхнего кадра (в самом верхнем Frame.catch уже отработал).
// Для не-raise ошибок возвращает true: trace для них не собирается.
func (s *Scheduler) raiseCatchable(a *Actor) bool {
	var rerr *ErrRaise
	if !errors.As(a.err, &rerr) {
		return true
	}
	for i := len(a.frames) - 2; i >= 0; i-- {
		if len(a.frames[i].handlers) > 0 {
			return true
		}
		// Кадр Telemetry ловит raise обработчика: актор не падает (§12.14).
		if _, ok := a.frames[i].cont.(*teleRun); ok {
			return true
		}
		// Записи cleanups и drain ловят raise как trap уровня (§5.1).
		if _, ok := a.frames[i].cont.(*drainRun); ok || len(a.frames[i].cleanups) > 0 {
			return true
		}
	}
	return false
}

// attachTrace кладёт в ErrRaise кадры от места raise к main. Кадры,
// заменённые TAILCALL, в trace не попадают: их уже нет в a.frames.
// У вызывающих кадров ip уже сдвинут за CALL, поэтому берём ip-1.
// Вызывается только на пути непойманного raise. Trace, уже собранный во
// вложенном вызове (CallNested), не перезаписывается: он длиннее.
func attachTrace(a *Actor) {
	var rerr *ErrRaise
	if !errors.As(a.err, &rerr) || rerr.Trace != nil {
		return
	}
	n := len(a.frames)
	trace := make([]TraceFrame, 0, n)
	for i := n - 1; i >= 0; i-- {
		f := a.frames[i]
		if f.cont != nil {
			continue // кадр натива: позиции в исходнике нет
		}
		ip := f.ip
		if i != n-1 {
			ip--
		}
		trace = append(trace, TraceFrame{Func: f.name, File: f.chunk.File, Pos: f.chunk.PosAt(ip)})
	}
	rerr.Trace = trace
}

// tryUnwindRaise пытается поймать невыловленный raise, всплывая вверх
// по стеку кадров текущего актора.
func (s *Scheduler) tryUnwindRaise(a *Actor) bool {
	var rerr *ErrRaise
	if !errors.As(a.err, &rerr) {
		return false
	}
	if len(a.frames) == 0 {
		return false
	}

	a.popFrame()

	for len(a.frames) > 0 {
		parent := a.frames[len(a.frames)-1]
		if len(parent.handlers) > 0 {
			h := parent.handlers[len(parent.handlers)-1]
			parent.handlers = parent.handlers[:len(parent.handlers)-1]
			parent.regs[h.errReg] = rerr.Val
			parent.ip = h.ip
			a.err = nil
			a.result = runtime.Unit
			return true
		}
		if run, ok := parent.cont.(*teleRun); ok {
			s.catchTele(a, run, rerr.Val)
			return true
		}
		if s.catchCleanups(a, parent, rerr.Val) {
			return true
		}
		a.popFrame()
	}
	return false
}

// notBoolErr — raise (:type_error, (:expected_bool, v)) для не-Bool в
// условии if, операнде and/or и guard (строгий Bool, DD #41 вариант A);
// форма payload — как у assert: (:type_error, (:assert_expected_bool, v)).
func notBoolErr(v runtime.Value) error {
	return typeErr("expected_bool", v)
}
