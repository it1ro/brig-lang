package vm

import (
	"errors"
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

// ErrSessionClosed — то же, для вызывающего вне пакета.
var ErrSessionClosed = errSessionClosed

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
		hwm:       defaultHWM,
		watchers:  make(map[int]int),
		watching:  make(map[int]int),
		status:    actorBlocked,
		initialFn: sessionInitialFn,
	}
	pid := s.nextPid
	s.nextPid++
	a.pid = pid
	s.actors[pid] = a
	s.sessionPid = pid
	s.session = true
	s.jobs = make(chan *sessionJob)
	s.snaps = make(chan chan []ActorSnapshot)
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
// Натив на горутине цикла зовёт RedefineHere: сдача в jobs оттуда deadlock.
func (s *Scheduler) Redefine(defs map[string]runtime.Value, undef []string) error {
	if !s.session {
		return errors.New("internal: repl session is not started")
	}
	_, err := s.submit(&sessionJob{
		fn:    runtime.Unit,
		defs:  defs,
		undef: undef,
		done:  make(chan sessionResult, 1),
	})
	return err
}

// RedefineHere — Redefine из натива, который исполняет цикл сессии
// (хелперы load и recompile): цикл уже стоит между редукциями.
func (s *Scheduler) RedefineHere(defs map[string]runtime.Value, undef []string) {
	s.applyGlobals(undef, defs)
}

// Sync выполняет fn на горутине цикла, между вводами. Натив на горутине
// цикла зовёт fn сам: сдача в jobs оттуда deadlock.
func (s *Scheduler) Sync(fn func() error) error {
	if !s.session {
		return errors.New("internal: repl session is not started")
	}
	_, err := s.submit(&sessionJob{
		fn:   runtime.Unit,
		sync: fn,
		done: make(chan sessionResult, 1),
	})
	return err
}

func (s *Scheduler) submit(job *sessionJob) (runtime.Value, error) {
	select {
	case s.jobs <- job:
	case <-s.stop:
		return runtime.Unit, errSessionClosed
	case <-s.loopDone:
		return runtime.Unit, s.loopErr()
	}
	select {
	case res := <-job.done:
		return res.val, res.err
	case <-s.stop:
		return runtime.Unit, errSessionClosed
	case <-s.loopDone:
		return runtime.Unit, s.loopErr()
	}
}

// loopErr — почему цикл сессии закончился: Sys.halt или закрытие. Звать
// после <-s.loopDone: halt записан до его закрытия.
func (s *Scheduler) loopErr() error {
	if s.halt != nil {
		return s.halt
	}
	return errSessionClosed
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

// Wake будит цикл сессии, если он спит в select. Можно звать не с горутины цикла.
func (s *Scheduler) Wake() { s.poke() }

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
	defer close(s.loopDone)
	for {
		if s.stopping() {
			s.shutdownSession()
			return
		}
		if s.halt != nil {
			s.haltSession()
			return
		}
		if s.consumeInterrupt() {
			continue
		}
		s.drainInject()
		s.serveSnapshots()
		s.pollJob()
		if len(s.ready) == 0 {
			if !s.waitSession(true) {
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
// takeJobs — принять новый ввод; вложенный вызов (awaitNested) не берёт:
// ввод исполнится после текущего.
func (s *Scheduler) waitSession(takeJobs bool) bool {
	next := s.nextDeadline()
	var timer *time.Timer
	var timerC <-chan time.Time
	var jobs <-chan *sessionJob
	if takeJobs && s.pending == nil {
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
	case reply := <-s.snaps:
		reply <- s.snapshot()
		return true
	case <-timerC:
		s.wakeExpired()
		return true
	case <-s.inject.ready:
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

func (s *Scheduler) shutdownSession() { s.endSession(errSessionClosed) }

// haltSession — Sys.halt в сессии (§12.12): текущий и отложенный ввод
// получают ErrHalt, новые — тоже (loopErr); ensure не исполняются.
func (s *Scheduler) haltSession() { s.endSession(s.halt) }

func (s *Scheduler) endSession(err error) {
	pending := s.pending
	s.pending = nil
	if a := s.actors[s.sessionPid]; a != nil && s.current != nil {
		s.dropFrames(a)
		a.status = actorBlocked
		s.clearTimer(a)
		s.completeJob(runtime.Unit, err)
	}
	if pending != nil {
		pending.done <- sessionResult{val: runtime.Unit, err: err}
	}
	s.closeAllPorts()
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
	a.exit = nil
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

// burn тратит редукцию слайса и считает её в бюджете хода актора (§12.10).
func (s *Scheduler) burn(a *Actor, reds *int) {
	*reds--
	s.reduce(a)
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
// recv без сообщения не ошибка: пока актор сессии ждёт, цикл крутит
// остальные акторы и таймеры (awaitNested). Непойманный raise несёт trace,
// как в runSlice.
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
		if a.exitPending() {
			// exit актору сессии во вложенном вызове: вложенные кадры
			// снимаются без ensure, unwind внешних продолжит runSlice.
			s.dropAbove(a, base)
			return runtime.Unit, &ErrExit{Reason: a.exit.reason}
		}
		top := a.frames[len(a.frames)-1]
		switch s.stepFrame(a, top) {
		case stepDone:
			drop := top.dropResult
			a.popFrame()
			if !drop && len(a.frames) > base {
				caller := a.frames[len(a.frames)-1]
				caller.regs[caller.callDst] = a.result
			}
		case stepFailed:
			if !s.raiseCatchable(a) {
				attachTrace(a)
			}
			err := a.err
			if s.unwindAbove(a, base) {
				continue
			}
			s.dropAbove(a, base)
			return runtime.Unit, err
		case stepExit:
			// обработка — в начале цикла
		case stepBlock:
			if err := s.awaitNested(a); err != nil {
				s.dropAbove(a, base)
				s.clearTimer(a)
				return runtime.Unit, err
			}
		}
	}
	return a.result, nil
}

// YieldUntil крутит остальных акторов и обслуживает Snapshot, пока актор
// сессии занят долгим нативом. done закрывает другая горутина, когда натив
// может вернуться. onStop зовётся один раз при Interrupt или закрытии
// сессии — чтобы та горутина завершилась и закрыла done; до этого цикл не
// бросает ждущий Snapshot. serve — неблокирующая работа на горутине цикла
// (например, ответ на запрос снимка). Звать только с горутины цикла.
func (s *Scheduler) YieldUntil(done <-chan struct{}, onStop func(), serve func()) error {
	if done == nil {
		return errors.New("internal: YieldUntil without done")
	}
	if serve == nil {
		serve = func() {}
	}
	var stopErr error
	stopped := false
	requestStop := func(err error) {
		if stopped {
			return
		}
		stopped = true
		stopErr = err
		if onStop != nil {
			onStop()
		}
	}
	for {
		select {
		case <-done:
			if stopErr != nil {
				return stopErr
			}
			if s.interrupt.Load() {
				return ErrInterrupted
			}
			return nil
		default:
		}
		if s.stopping() || s.halt != nil {
			err := errSessionClosed
			if !s.stopping() {
				err = s.halt
			}
			requestStop(err)
			if onStop == nil {
				return err
			}
			<-done
			return stopErr
		} else if !stopped && s.interrupt.Load() {
			requestStop(ErrInterrupted)
			if onStop == nil {
				return ErrInterrupted
			}
		}
		serve()
		s.drainInject()
		s.serveSnapshots()
		if len(s.ready) == 0 {
			if !s.waitYield(done) {
				requestStop(errSessionClosed)
				if onStop == nil {
					return errSessionClosed
				}
			}
			continue
		}
		b := s.ready[0]
		s.ready = s.ready[1:]
		if b == nil || b.pid == s.sessionPid || b.status == actorDone || b.status == actorFailed {
			continue
		}
		b.status = actorReady
		s.runSlice(b)
	}
}

// waitYield — как waitSession, но ещё просыпается, когда done закрыт.
// false — сессию закрыли.
func (s *Scheduler) waitYield(done <-chan struct{}) bool {
	next := s.nextDeadline()
	var timer *time.Timer
	var timerC <-chan time.Time
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
	case <-done:
		return true
	case reply := <-s.snaps:
		reply <- s.snapshot()
		return true
	case <-timerC:
		s.wakeExpired()
		return true
	case <-s.inject.ready:
		return true
	case <-s.wake:
		return true
	case <-s.stop:
		return false
	}
}

// awaitNested — актор сессии a встал в recv вложенного вызова. Цикл
// крутит остальные акторы и таймеры, пока a не разбудят (send, :down,
// таймер after), прерывание или закрытие сессии.
func (s *Scheduler) awaitNested(a *Actor) error {
	a.status = actorBlocked
	for a.status == actorBlocked {
		if s.stopping() {
			return errSessionClosed
		}
		if s.interrupt.Load() {
			return ErrInterrupted
		}
		if s.halt != nil {
			return s.halt
		}
		s.drainInject()
		s.serveSnapshots()
		if len(s.ready) == 0 {
			if !s.waitSession(false) {
				return errSessionClosed
			}
			continue
		}
		b := s.ready[0]
		s.ready = s.ready[1:]
		if b == a || b.status == actorDone || b.status == actorFailed {
			continue
		}
		b.status = actorReady
		s.runSlice(b)
	}
	s.unready(a)
	a.status = actorReady
	return nil
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
		if run, ok := parent.cont.(*teleRun); ok {
			s.catchTele(a, run, rerr.Val)
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
