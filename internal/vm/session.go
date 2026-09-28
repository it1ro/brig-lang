package vm

import (
	"errors"
	goruntime "runtime"
	"sync/atomic"
	"time"

	"github.com/it1ro/brig-lang/internal/runtime"
)

// DefaultSliceReductions — редукций в одном слайсе планировщика.
// Прерывание ввода сессии снимает его не позже чем через один такой слайс.
const DefaultSliceReductions = defaultReductions

// ErrInterrupted — текущий ввод сессии снят прерыванием (§11.4).
// Это не raise: trap его не ловит, ensure прерванного ввода не исполняется.
var ErrInterrupted = errors.New("interrupted")

// errSessionClosed — сессию закрыли, пока ввод ещё исполнялся.
var errSessionClosed = errors.New("internal: repl session closed")

// sessionJob — одна порция работы актора сессии: функция ввода и её аргументы.
type sessionJob struct {
	fn   runtime.Value
	args []runtime.Value
	defs map[string]runtime.Value
	// undef — глобалы, которые снимаются до defs (Redefine).
	undef []string
	// sync — работа на горутине цикла без ввода (регистрация хелперов).
	sync func() error
	done chan sessionResult
}

type sessionResult struct {
	val runtime.Value
	err error
}

// StartSession поднимает актор сессии и фоновый цикл планировщика (§11.4).
// Цикл один на сессию и живёт, пока сессию не закроют: между вводами он
// крутит заспавненные акторы и таймеры. Акторы по-прежнему кооперативные,
// на одной горутине цикла — не N:M.
func (s *Scheduler) StartSession() error {
	if s.session {
		return errors.New("internal: repl session already started")
	}
	a := &Actor{
		hwm:      defaultHWM,
		watchers: make(map[int]int),
		watching: make(map[int]int),
		status:   actorBlocked,
	}
	pid := s.nextPid
	s.nextPid++
	a.pid = pid
	s.actors[pid] = a
	s.sessionPid = pid
	s.session = true
	s.jobs = make(chan *sessionJob)
	s.wake = make(chan struct{}, 1)
	s.stop = make(chan struct{})
	s.loopDone = make(chan struct{})
	go s.sessionLoop()
	return nil
}

// CloseSession останавливает фоновый цикл. Повторный вызов ничего не делает.
// Текущий ввод завершается errSessionClosed.
func (s *Scheduler) CloseSession() {
	if !s.session {
		return
	}
	s.stopOnce.Do(func() { close(s.stop) })
	<-s.loopDone
}

// Interrupt просит снять текущий ввод. Флаг смотрит цикл на границе слайса
// и в ожидании: бесконечный ввод останавливается не позже чем через
// defaultReductions редукций, recv без отправителя — сразу, как только
// цикл выйдет из слайса или из select. Нет текущего ввода — флаг гасится
// и следующий ввод не трогается. Фоновые акторы не затрагиваются.
func (s *Scheduler) Interrupt() {
	if !s.session {
		return
	}
	// Ноль до флага: редукции, которые burn увидит уже при взведённом флаге,
	// и есть хвост текущего слайса. Следующий слайс не начинается.
	s.afterInterrupt.Store(0)
	s.interrupt.Store(true)
	s.poke()
}

// Submit исполняет fn(args) актором сессии и ждёт итог. defs регистрируются
// в глобалах на горутине цикла до запуска — вызывающий их не пишет.
func (s *Scheduler) Submit(fn runtime.Value, args []runtime.Value, defs map[string]runtime.Value) (runtime.Value, error) {
	if !s.session {
		return runtime.Unit, errors.New("internal: repl session is not started")
	}
	return s.submit(&sessionJob{
		fn:   fn,
		args: args,
		defs: defs,
		done: make(chan sessionResult, 1),
	})
}

// Redefine снимает глобалы undef и регистрирует defs одной порцией работы
// цикла: между редукциями, атомарно для всех акторов. Ввод при этом не
// исполняется. Так recompile() подменяет функции модулей (T-208, #246).
func (s *Scheduler) Redefine(defs map[string]runtime.Value, undef []string) error {
	if !s.session {
		return errors.New("internal: repl session is not started")
	}
	if s.onLoop() {
		s.applyGlobals(undef, defs)
		return nil
	}
	_, err := s.submit(&sessionJob{
		fn:    runtime.Unit,
		defs:  defs,
		undef: undef,
		done:  make(chan sessionResult, 1),
	})
	return err
}

// Sync выполняет fn на горутине цикла, между вводами. С самой горутины
// цикла (натив хелпера) fn зовётся сразу: сдача в jobs оттуда deadlock.
func (s *Scheduler) Sync(fn func() error) error {
	if !s.session {
		return errors.New("internal: repl session is not started")
	}
	if s.onLoop() {
		return fn()
	}
	_, err := s.submit(&sessionJob{
		fn:   runtime.Unit,
		sync: fn,
		done: make(chan sessionResult, 1),
	})
	return err
}

func (s *Scheduler) onLoop() bool {
	id := s.loopGID.Load()
	return id != 0 && id == goroutineID()
}

func goroutineID() uint64 {
	var buf [64]byte
	n := goruntime.Stack(buf[:], false)
	const p = "goroutine "
	s := buf[:n]
	if len(s) < len(p) {
		return 0
	}
	var id uint64
	for i := len(p); i < len(s) && s[i] >= '0' && s[i] <= '9'; i++ {
		id = id*10 + uint64(s[i]-'0')
	}
	return id
}

func (s *Scheduler) submit(job *sessionJob) (runtime.Value, error) {
	select {
	case s.jobs <- job:
	case <-s.stop:
		return runtime.Unit, errSessionClosed
	}
	select {
	case res := <-job.done:
		return res.val, res.err
	case <-s.stop:
		return runtime.Unit, errSessionClosed
	}
}

// CountSessionReductions включает счёт редукций в c. Вызывать до StartSession:
// указатель после старта цикла не меняется.
func (s *Scheduler) CountSessionReductions(c *atomic.Uint64) { s.redCount = c }

func (s *Scheduler) poke() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Scheduler) stopping() bool {
	select {
	case <-s.stop:
		return true
	default:
		return false
	}
}

// sessionLoop — run-loop режима сессии. Готовые акторы идут тем же FIFO,
// что и в runMain; пустая очередь не deadlock, а ожидание таймера, ввода
// или прерывания.
func (s *Scheduler) sessionLoop() {
	s.loopGID.Store(goroutineID())
	defer close(s.loopDone)
	defer s.loopGID.Store(0)
	for {
		if s.stopping() {
			s.shutdownSession()
			return
		}
		if s.consumeInterrupt() {
			continue
		}
		s.pollJob()
		if len(s.ready) == 0 {
			if !s.waitSession() {
				s.shutdownSession()
				return
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

// waitSession блокируется, пока нечего крутить. false — сессию закрыли.
func (s *Scheduler) waitSession() bool {
	next := s.nextDeadline()
	var timer *time.Timer
	var timerC <-chan time.Time
	var jobs <-chan *sessionJob
	if s.pending == nil {
		jobs = s.jobs
	}
	if !next.IsZero() {
		d := time.Until(next)
		if d <= 0 {
			s.wakeExpired()
			return true
		}
		timer = time.NewTimer(d)
		defer timer.Stop()
		timerC = timer.C
	}
	select {
	case job := <-jobs:
		s.takeJob(job)
		return true
	case <-timerC:
		s.wakeExpired()
		return true
	case <-s.wake:
		return true
	case <-s.stop:
		return false
	}
}

func (s *Scheduler) pollJob() {
	if s.pending != nil {
		return
	}
	select {
	case job := <-s.jobs:
		s.takeJob(job)
	default:
	}
}

func (s *Scheduler) takeJob(job *sessionJob) {
	if s.current != nil {
		s.pending = job
		return
	}
	s.beginJob(job)
}

func (s *Scheduler) applyGlobals(undef []string, defs map[string]runtime.Value) {
	for _, name := range undef {
		delete(s.vm.globals, name)
	}
	for name, v := range defs {
		s.vm.globals[name] = v
	}
}

func (s *Scheduler) beginJob(job *sessionJob) {
	a := s.actors[s.sessionPid]
	s.applyGlobals(job.undef, job.defs)
	if job.sync != nil {
		job.done <- sessionResult{val: runtime.Unit, err: job.sync()}
		return
	}
	if job.fn.Kind == runtime.KindUnit {
		job.done <- sessionResult{val: runtime.Unit}
		return
	}
	if _, err := a.pushCall(job.fn, job.args); err != nil {
		job.done <- sessionResult{val: runtime.Unit, err: err}
		return
	}
	s.current = job
	a.status = actorReady
	s.ready = append(s.ready, a)
}

func (s *Scheduler) completeJob(val runtime.Value, err error) {
	job := s.current
	s.current = nil
	s.interrupt.Store(false)
	if job != nil {
		job.done <- sessionResult{val: val, err: err}
	}
	if s.pending != nil {
		j := s.pending
		s.pending = nil
		s.beginJob(j)
	}
}

func (s *Scheduler) shutdownSession() {
	pending := s.pending
	s.pending = nil
	if a := s.actors[s.sessionPid]; a != nil && s.current != nil {
		s.dropFrames(a)
		a.status = actorBlocked
		s.clearTimer(a)
		s.completeJob(runtime.Unit, errSessionClosed)
	}
	if pending != nil {
		pending.done <- sessionResult{val: runtime.Unit, err: errSessionClosed}
	}
}

// consumeInterrupt снимает текущий ввод, если прерывание уже запрошено.
// Нет ввода — флаг сбрасывается, чтобы он не задел следующий.
func (s *Scheduler) consumeInterrupt() bool {
	if !s.interrupt.Load() {
		return false
	}
	a := s.actors[s.sessionPid]
	if a == nil || s.current == nil {
		s.interrupt.Store(false)
		return false
	}
	s.unready(a)
	s.abortSession(a)
	return true
}

func (s *Scheduler) sessionInterrupted(a *Actor) bool {
	return a.pid == s.sessionPid && s.current != nil && s.interrupt.Load()
}

// abortSession снимает кадры ввода, не исполняя ensure (§11.4: прерывание
// не гарантирует ensure и не является raise). Ящик и прочие акторы остаются.
func (s *Scheduler) abortSession(a *Actor) {
	if s.current == nil {
		s.interrupt.Store(false)
		return
	}
	s.dropFrames(a)
	a.status = actorBlocked
	s.clearTimer(a)
	s.completeJob(runtime.Unit, ErrInterrupted)
}

// parkSession — у актора сессии кончились кадры без завершённого ввода
// (его разбудил send, а работы нет). Не reaping: pid сессии стабилен.
func (s *Scheduler) parkSession(a *Actor) bool {
	if a.pid != s.sessionPid {
		return false
	}
	if s.current != nil {
		return s.finishSessionJob(a, nil)
	}
	a.status = actorBlocked
	return true
}

// finishSessionJob завершает ввод успехом (err == nil, значение — a.result)
// или ошибкой. Актор сессии остаётся в таблице: ни :down, ни reap.
func (s *Scheduler) finishSessionJob(a *Actor, err error) bool {
	if a.pid != s.sessionPid || s.current == nil {
		return false
	}
	val := a.result
	if err != nil {
		val = runtime.Unit
	}
	s.dropFrames(a)
	a.err = nil
	a.result = runtime.Unit
	a.status = actorBlocked
	s.clearTimer(a)
	s.completeJob(val, err)
	return true
}

func (s *Scheduler) dropFrames(a *Actor) {
	for len(a.frames) > 0 {
		a.popFrame()
	}
	a.err = nil
	a.result = runtime.Unit
}

func (s *Scheduler) unready(a *Actor) {
	n := 0
	for _, x := range s.ready {
		if x != a {
			s.ready[n] = x
			n++
		}
	}
	clear(s.ready[n:])
	s.ready = s.ready[:n]
}

func (s *Scheduler) burn(a *Actor, reds *int) {
	*reds--
	if a.pid != s.sessionPid {
		return
	}
	if c := s.redCount; c != nil {
		c.Add(1)
	}
	// Хвост слайса после сигнала. Следующий слайс сюда не доходит:
	// цикл снимает ввод на границе.
	if s.interrupt.Load() {
		s.afterInterrupt.Add(1)
	}
}

// CallNested исполняет fn на акторе сессии, не трогая кадры, которые уже
// на стеке. Звать с горутины цикла из натива хелпера: сдача ввода в jobs
// оттуда deadlock, а отдельный актор callSync подменил бы self().
func (s *Scheduler) CallNested(fn runtime.Value, args []runtime.Value) (runtime.Value, error) {
	a := s.actors[s.sessionPid]
	if a == nil {
		return runtime.Unit, errors.New("internal: no session actor")
	}
	base := len(a.frames)
	if _, err := a.pushCall(fn, args); err != nil {
		return runtime.Unit, err
	}
	for len(a.frames) > base {
		if s.interrupt.Load() {
			s.dropAbove(a, base)
			return runtime.Unit, ErrInterrupted
		}
		top := a.frames[len(a.frames)-1]
		switch s.stepFrame(a, top) {
		case stepDone:
			a.popFrame()
			if len(a.frames) > base {
				caller := a.frames[len(a.frames)-1]
				caller.regs[caller.callDst] = a.result
			}
		case stepFailed:
			err := a.err
			if s.unwindAbove(a, base) {
				continue
			}
			s.dropAbove(a, base)
			return runtime.Unit, err
		case stepBlock:
			s.dropAbove(a, base)
			return runtime.Unit, errors.New("internal: blocked in nested call")
		}
	}
	return a.result, nil
}

func (s *Scheduler) dropAbove(a *Actor, base int) {
	for len(a.frames) > base {
		a.popFrame()
	}
	a.err = nil
}

// unwindAbove — tryUnwindRaise, который не снимает кадры ниже base.
func (s *Scheduler) unwindAbove(a *Actor, base int) bool {
	var rerr *ErrRaise
	if !errors.As(a.err, &rerr) {
		return false
	}
	if len(a.frames) <= base {
		return false
	}
	a.popFrame()
	for len(a.frames) > base {
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
		a.popFrame()
	}
	return false
}

// TakeSessionMail снимает пользовательские сообщения ящика сессии.
// Звать с горутины цикла.
func (s *Scheduler) TakeSessionMail() []runtime.Value {
	a := s.actors[s.sessionPid]
	if a == nil || len(a.mailbox) == 0 {
		return nil
	}
	out := append([]runtime.Value(nil), a.mailbox...)
	a.mailbox = a.mailbox[:0]
	return out
}

// ActorInfo — жив ли актор pid и сколько в его ящике пользовательских
// сообщений. Снятый актор — не жив, ящик 0. Звать с горутины цикла.
func (s *Scheduler) ActorInfo(pid int) (alive bool, mailbox int) {
	a := s.actors[pid]
	if a == nil || a.status == actorDone || a.status == actorFailed {
		return false, 0
	}
	return true, len(a.mailbox)
}
