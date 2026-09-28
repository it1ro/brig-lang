package vm

import "github.com/it1ro/brig-lang/internal/runtime"

// ---- exit (§12.7, T-163) ----

// ErrExit — актор завершён сигналом exit (§12.7). Не raise: trap его не
// ловит; наблюдатели получают Reason как есть.
type ErrExit struct{ Reason runtime.Value }

func (e *ErrExit) Error() string { return "exit: " + e.Reason.Inspect() }

// exitSig — полученный актором сигнал exit.
type exitSig struct {
	reason runtime.Value
	// kill — пришёл exit(pid, :kill): ensure не выполняются, начатые
	// прерываются. Причина при этом остаётся первой.
	kill bool
	// unwinding — unwind начат; повторный exit (кроме :kill) игнорируется.
	unwinding bool
	// ensFrame, ensHandlers — ensure-блок, в который вошёл unwind: его
	// ENSEND в том же кадре при той же глубине handlers продолжает unwind.
	ensFrame, ensHandlers int
}

func isKill(v runtime.Value) bool {
	return v.Kind == runtime.KindAtom && v.Atom == "kill"
}

// Exit ставит актору pid сигнал exit. Мёртвый или несуществующий pid —
// ничего. Возвращает Ok(()) всегда (§12.7). Сигнал не проходит через
// ящик; жертва завершается в ближайшей точке редукции или ожидания:
// заблокированную будим, готовую ставим в начало очереди. exit себе —
// не сюда: EXIT обрабатывает его синхронно (stepExit).
func (s *Scheduler) Exit(pid int, reason runtime.Value) runtime.Value {
	if a, ok := s.actors[pid]; ok && a.status != actorDone && a.status != actorFailed && !s.sessionIdle(a) {
		if s.signalExit(a, reason) {
			s.hurry(a)
		}
	}
	return runtime.Variant("Ok", runtime.Unit)
}

// signalExit записывает сигнал. Первая причина выигрывает; :kill
// поверх начатого unwind прерывает оставшиеся ensure. false — сигнал
// ничего не меняет.
func (s *Scheduler) signalExit(a *Actor, reason runtime.Value) bool {
	if a.exit == nil {
		a.exit = &exitSig{reason: reason, kill: isKill(reason)}
		return true
	}
	if isKill(reason) && !a.exit.kill {
		a.exit.kill = true
		a.exit.unwinding = false
		return true
	}
	return false
}

// hurry переводит жертву exit в начало очереди готовых, не дожидаясь её
// кванта; ждущую в recv или await — будит, снимая таймер.
func (s *Scheduler) hurry(a *Actor) {
	switch a.status {
	case actorBlocked:
		s.clearTimer(a)
	case actorReady:
		s.unready(a)
	default:
		return
	}
	a.status = actorReady
	s.ready = append([]*Actor{a}, s.ready...)
}

// sessionIdle — актор сессии REPL между вводами: завершать нечего, exit
// ему игнорируется, как и пробуждение почтой (wakeIfBlocked).
func (s *Scheduler) sessionIdle(a *Actor) bool {
	return a.pid == s.sessionPid && s.current == nil && len(a.frames) == 0
}

// exitPending — сигнал есть, а unwind по нему ещё не начат (или :kill
// перезапустил его).
func (a *Actor) exitPending() bool {
	return a.exit != nil && !a.exit.unwinding
}

// unwindExit снимает кадры актора до ближайшего ensure-блока, пропуская
// кадры trap (§12.7). true — вошли в ensure-блок, исполнение продолжается;
// false — ensure не осталось (или :kill), актора пора завершать.
func (s *Scheduler) unwindExit(a *Actor) bool {
	sig := a.exit
	sig.unwinding = true
	for len(a.frames) > 0 {
		f := a.frames[len(a.frames)-1]
		for !sig.kill && len(f.handlers) > 0 {
			h := f.handlers[len(f.handlers)-1]
			f.handlers = f.handlers[:len(f.handlers)-1]
			if !h.ensure {
				continue
			}
			f.regs[h.errReg] = sig.reason
			f.ip = h.ip
			sig.ensFrame = len(a.frames) - 1
			sig.ensHandlers = len(f.handlers)
			a.err = nil
			return true
		}
		a.popFrame()
	}
	return false
}

// atEnsureEnd — ENSEND закрывает ensure-блок, в который вошёл unwind.
func (a *Actor) atEnsureEnd(f *Frame) bool {
	sig := a.exit
	return sig != nil && sig.unwinding &&
		sig.ensFrame == len(a.frames)-1 && sig.ensHandlers == len(f.handlers)
}

// exitStep продвигает завершение по сигналу: unwind до следующего ensure
// или смерть. true — актор больше не исполняется в этом слайсе.
func (s *Scheduler) exitStep(a *Actor) bool {
	if s.unwindExit(a) {
		return false
	}
	s.exitDone(a)
	return true
}

// exitDone — смерть по сигналу: :down наблюдателям с причиной сигнала
// как есть. Ввод сессии REPL завершается ошибкой ErrExit, актор сессии
// остаётся.
func (s *Scheduler) exitDone(a *Actor) {
	sig := a.exit
	a.exit = nil
	err := &ErrExit{Reason: sig.reason}
	s.dropFrames(a)
	if a.pid == s.sessionPid {
		if !s.finishSessionJob(a, err) {
			a.status = actorBlocked
		}
		return
	}
	a.status = actorFailed
	a.err = err
	s.notifyWatchers(a, sig.reason)
	s.reapActor(a)
}
