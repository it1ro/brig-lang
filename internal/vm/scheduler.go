package vm

import (
	"errors"
	"fmt"
	"math"
	"sort"
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
func bindArgs(regs []runtime.Value, ch *Chunk, args []runtime.Value) {
	if !ch.Variadic {
		copy(regs, args) // copy == memmove; перекрытие допустимо
		return
	}
	fixed := ch.NumParams - 1
	rest := make([]runtime.Value, len(args)-fixed)
	copy(rest, args[fixed:])
	copy(regs, args[:fixed])
	regs[fixed] = runtime.List(rest...)
}

// spreadArgs разворачивает последний аргумент-List в отдельные аргументы;
// результат — свежий срез, не пересекающийся с regs.
func spreadArgs(args []runtime.Value) ([]runtime.Value, error) {
	last := args[len(args)-1]
	if last.Kind != runtime.KindList {
		return nil, typeErr("spread", last)
	}
	out := make([]runtime.Value, 0, len(args)-1+len(last.List))
	out = append(out, args[:len(args)-1]...)
	return append(out, last.List...), nil
}

// frameFromFn создаёт кадр вызова.
func frameFromFn(fn runtime.Value, args []runtime.Value) (*Frame, error) {
	c, err := resolveCallee(fn)
	if err != nil {
		return nil, err
	}
	if err := checkArity(c, args); err != nil {
		return nil, err
	}
	regs := make([]runtime.Value, c.chunk.NumRegs)
	bindArgs(regs, c.chunk, args)
	return &Frame{
		chunk:    c.chunk,
		name:     c.name,
		regs:     regs,
		captures: c.captures,
	}, nil
}

// nativeFrame — кадр возобновляемого натива name с состоянием k.
func nativeFrame(name string, k nativeCont) *Frame {
	return &Frame{name: name, regs: make([]runtime.Value, 1), cont: k}
}

// enterCall готовит вызов fn(args) из кадра актора: кадр для байткод-
// функции или возобновляемого натива (G3, T-58) либо сразу результат
// обычного натива, который редукций не тратит (K-4). args может быть окном
// регистров вызывающего: нативу уходит свежая копия (K-5).
func (s *Scheduler) enterCall(fn runtime.Value, args []runtime.Value) (*Frame, runtime.Value, error) {
	if fn.Kind == runtime.KindFunction && fn.Func != nil && fn.Func.IsNative {
		if ar := fn.Func.Arity; ar >= 0 && ar != len(args) {
			return nil, runtime.Unit, functionClause(args)
		}
		fresh := append([]runtime.Value(nil), args...)
		if start, ok := s.vm.resumable[fn.Func]; ok {
			k, err := start(fresh)
			if err != nil {
				return nil, runtime.Unit, err
			}
			return nativeFrame(fn.Func.Name, k), runtime.Unit, nil
		}
		r, err := fn.Func.Native(s.vm, fresh)
		return nil, r, err
	}
	nf, err := frameFromFn(fn, args)
	return nf, runtime.Unit, err
}

// ---- Actor / Scheduler ----

// Actor — процесс в планировщике.
type Actor struct {
	pid    int
	frames []*Frame

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
}

// Scheduler — единый run-loop (§15.2).
type Scheduler struct {
	vm      *VM
	actors  map[int]*Actor
	nextPid int
	nextRef int
	nextSeq uint64
	ready   []*Actor
	reds    int
	mainPid int
	active  *Actor
}

// NewScheduler создаёт планировщик.
func NewScheduler(vm *VM) *Scheduler {
	return &Scheduler{
		vm:     vm,
		actors: make(map[int]*Actor),
		reds:   defaultReductions,
	}
}

// Spawn регистрирует новый актор с fn и args.
func (s *Scheduler) Spawn(fn runtime.Value, args []runtime.Value) (int, error) {
	f, err := frameFromFn(fn, args)
	if err != nil {
		return 0, err
	}
	pid := s.nextPid
	s.nextPid++

	a := &Actor{
		pid:      pid,
		frames:   []*Frame{f},
		hwm:      defaultHWM,
		watchers: make(map[int]int),
		watching: make(map[int]int),
		status:   actorReady,
	}
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
	if a.status == actorBlocked {
		a.status = actorReady
		s.ready = append(s.ready, a)
	}
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
	for ref, watcherPid := range a.watchers {
		s.sendDown(watcherPid, runtime.Tuple(
			runtime.Atom("down"),
			runtime.Value{Kind: runtime.KindRef, Ref: ref},
			reason,
		))
	}
}

// reapActor удаляет завершённый актор из таблицы (I-F9). mainPid
// оставляем: runMain читает его result/err после выхода из цикла.
func (s *Scheduler) reapActor(a *Actor) {
	if a.pid != s.mainPid {
		delete(s.actors, a.pid)
	}
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
	pid, err := s.Spawn(mainFn, args)
	if err != nil {
		return runtime.Unit, err
	}
	s.mainPid = pid

	for {
		if m, ok := s.actors[s.mainPid]; ok {
			if m.status == actorDone {
				return m.result, nil
			}
			if m.status == actorFailed {
				return runtime.Unit, m.err
			}
		}

		if len(s.ready) == 0 {
			next := s.nextDeadline()
			if !next.IsZero() {
				if d := time.Until(next); d > 0 {
					time.Sleep(d)
				}
				s.wakeExpired()
				continue
			}
			return runtime.Unit, fmt.Errorf("deadlock: all actors blocked")
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

func (s *Scheduler) nextDeadline() time.Time {
	var best time.Time
	for _, a := range s.actors {
		if a.status != actorBlocked || a.recvDeadline.IsZero() {
			continue
		}
		if best.IsZero() || a.recvDeadline.Before(best) {
			best = a.recvDeadline
		}
	}
	return best
}

// wakeExpired будит актёров с истёкшим таймером в порядке (deadline, seq):
// обход map недетерминирован, а порядок пробуждений наблюдаем (§15.4).
func (s *Scheduler) wakeExpired() {
	now := time.Now()
	var expired []*Actor
	for _, a := range s.actors {
		if a.status != actorBlocked {
			continue
		}
		if a.recvDeadline.IsZero() || a.recvDeadline.After(now) {
			continue
		}
		expired = append(expired, a)
	}
	sort.Slice(expired, func(i, j int) bool {
		a, b := expired[i], expired[j]
		if !a.recvDeadline.Equal(b.recvDeadline) {
			return a.recvDeadline.Before(b.recvDeadline)
		}
		return a.timerSeq < b.timerSeq
	})
	for _, a := range expired {
		a.status = actorReady
		s.ready = append(s.ready, a)
	}
}

// ---- runSlice ----

func (s *Scheduler) runSlice(a *Actor) {
	reds := s.reds
	for reds > 0 {
		if len(a.frames) == 0 {
			a.status = actorDone
			a.result = runtime.Unit
			s.notifyWatchers(a, runtime.Atom("normal"))
			s.reapActor(a)
			return
		}

		f := a.frames[len(a.frames)-1]
		outcome := s.stepFrame(a, f)
		switch outcome {
		case stepDone:
			// nil слот перед усечением, чтобы кадр не висел в backing-массиве.
			a.frames[len(a.frames)-1] = nil
			a.frames = a.frames[:len(a.frames)-1]
			reds--
			if len(a.frames) == 0 {
				a.status = actorDone
				s.notifyWatchers(a, runtime.Atom("normal"))
				s.reapActor(a)
				return
			}
			caller := a.frames[len(a.frames)-1]
			caller.regs[caller.callDst] = a.result

		case stepFailed:
			if !s.raiseCatchable(a) {
				attachTrace(a)
			}
			if s.tryUnwindRaise(a) {
				reds--
				continue
			}
			a.status = actorFailed
			s.notifyWatchers(a, downRaiseReason(a))
			s.reapActor(a)
			return

		case stepBlock:
			a.status = actorBlocked
			return

		case stepYield, stepContinue:
			reds--
		}
	}

	a.status = actorReady
	s.ready = append(s.ready, a)
}

// ---- stepFrame ----

func (s *Scheduler) stepFrame(a *Actor, f *Frame) stepOutcome {
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

			nf, r, err := s.enterCall(cv, args)
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
			a.frames = append(a.frames, nf)
			return stepContinue

		case TAILCALL, TAILCALLSPREAD:
			base, argc := in.A(), in.B()
			cv := regs[base]
			args := regs[base+1 : base+1+argc]
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
				if start, ok := s.vm.resumable[cv.Func]; ok {
					// Кадр становится кадром натива: колбэки пойдут поверх
					// него, результат — туда же, куда ушёл бы у f.
					k, err := start(fresh)
					if err != nil {
						return fail(err)
					}
					f.regs, f.chunk, f.name, f.captures, f.ip, f.cont =
						make([]runtime.Value, 1), nil, cv.Func.Name, nil, 0, k
					return stepContinue
				}
				r, err := cv.Func.Native(s.vm, fresh)
				if err != nil {
					return fail(err)
				}
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
				regs = make([]runtime.Value, nregs)
				bindArgs(regs, c.chunk, args)
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
			f.ip++

		case MAP:
			n := in.C()
			base := in.B()
			entries := make([]runtime.MapEntry, n)
			for i := 0; i < n; i++ {
				entries[i] = runtime.MapEntry{
					Key: regs[base+2*i],
					Val: regs[base+2*i+1],
				}
			}
			regs[in.A()] = runtime.Map(entries)
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
			f.ip++

		case TRAPBEGIN:
			f.handlers = append(f.handlers, trapHandler{
				ip:     f.ip + 1 + in.SBx(),
				errReg: in.A(),
			})
			f.ip++

		case TRAPEND:
			if len(f.handlers) == 0 {
				return fail(fmt.Errorf("internal: TRAPEND without handler"))
			}
			f.handlers = f.handlers[:len(f.handlers)-1]
			f.ip++

		case MAKEOK:
			regs[in.A()] = runtime.Variant("Ok", regs[in.B()])
			f.ip++

		case MAKEERROR:
			regs[in.A()] = runtime.Variant("Error", regs[in.B()])
			f.ip++

		// ---- Акторные опкоды (§6) ----

		case SPAWN:
			base, linked := in.A(), in.C() == 1
			fn := regs[in.B()]
			pid, err := s.Spawn(fn, nil)
			if err != nil {
				if f.catch(err) {
					continue
				}
				return fail(err)
			}
			if linked {
				s.Watch(a.pid, pid)
			}
			regs[base] = runtime.Value{Kind: runtime.KindPid, Pid: pid}
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

		case SELF:
			regs[in.A()] = runtime.Value{Kind: runtime.KindPid, Pid: a.pid}
			f.ip++

		case MAKEREF:
			ref := s.nextRef
			s.nextRef++
			regs[in.A()] = runtime.Value{Kind: runtime.KindRef, Ref: ref}
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
			a.recvDeadline = time.Now().Add(d)
			a.timerSeq = s.nextSeq
			s.nextSeq++
			f.ip++

		case RECVTAKE:
			slot := in.A()
			sbx := in.SBx()

			if len(a.downMsgs) > 0 {
				msg := a.downMsgs[0]
				a.downMsgs = a.downMsgs[1:]
				a.recvDeadline = time.Time{}
				regs[slot] = msg
				f.ip++
				continue
			}
			if len(a.mailbox) > 0 {
				msg := a.mailbox[0]
				a.mailbox = a.mailbox[1:]
				a.recvDeadline = time.Time{}
				regs[slot] = msg
				f.ip++
				continue
			}

			if !a.recvDeadline.IsZero() && !time.Now().Before(a.recvDeadline) {
				a.recvDeadline = time.Time{}
				if sbx != 0 {
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
		if st.done {
			a.result = st.res
			return stepDone
		}
		nf, r, err := s.enterCall(st.fn, st.args)
		if err != nil {
			return fail(err)
		}
		if nf == nil {
			f.regs[0] = r
			continue
		}
		f.callDst = 0
		a.frames = append(a.frames, nf)
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
		if i < 0 || i >= int64(len(obj.List)) {
			return runtime.Unit, &ErrRaise{Val: runtime.Tuple(
				runtime.Atom("index_out_of_bounds"),
				runtime.Tuple(idx, runtime.Int(int64(len(obj.List)))))}
		}
		return obj.List[i], nil

	case runtime.KindVector:
		i, err := indexToInt(idx)
		if err != nil {
			return runtime.Unit, err
		}
		if i < 0 || i >= int64(len(obj.Vector)) {
			return runtime.Unit, &ErrRaise{Val: runtime.Tuple(
				runtime.Atom("index_out_of_bounds"),
				runtime.Tuple(idx, runtime.Int(int64(len(obj.Vector)))))}
		}
		return obj.Vector[i], nil

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
		for _, e := range obj.Map {
			if runtime.Equal(e.Key, idx) {
				return runtime.Variant("Some", e.Val), nil
			}
		}
		return runtime.Variant("None"), nil
	}
	return runtime.Unit, typeErr("index", obj)
}

// ---- helpers for RECORD / GETFIELD (T-73, §4.7) ----

// vmMakeRecord собирает запись по форме (см. RECORD). Поля номинальной
// записи идут в порядке декларации, анонимной — в порядке первого
// появления; повторное поле (спред, затем явное) перезаписывает значение.
// Спред в номинальную запись поля, которого нет в декларации, —
// raise (:field_error, (:field, "Type")).
func vmMakeRecord(shape runtime.Value, vals []runtime.Value) (runtime.Value, error) {
	if shape.Kind != runtime.KindTuple || len(shape.Tuple) != 3 ||
		shape.Tuple[0].Kind != runtime.KindStr ||
		shape.Tuple[1].Kind != runtime.KindTuple ||
		shape.Tuple[2].Kind != runtime.KindTuple ||
		len(shape.Tuple[2].Tuple) != len(vals) {
		return runtime.Unit, fmt.Errorf("internal: RECORD: bad shape %s", shape.Inspect())
	}
	typ, declared, slots := shape.Tuple[0].Str, shape.Tuple[1].Tuple, shape.Tuple[2].Tuple

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

	for i, slot := range slots {
		if slot.Str != ".." {
			put(slot.Str, vals[i])
			continue
		}
		src := vals[i]
		if src.Kind != runtime.KindRecord {
			return runtime.Unit, typeErr("record_spread", src)
		}
		for _, f := range src.Record.Fields {
			if typ != "" && !isDeclared(f.Name) {
				return runtime.Unit, &ErrRaise{Val: runtime.Tuple(
					runtime.Atom("field_error"),
					runtime.Tuple(runtime.Atom(f.Name), runtime.Str(typ)))}
			}
			put(f.Name, f.Val)
		}
	}

	if typ != "" {
		ordered := make([]runtime.RecordField, 0, len(fields))
		for _, d := range declared {
			for _, f := range fields {
				if f.Name == d.Str {
					ordered = append(ordered, f)
					break
				}
			}
		}
		fields = ordered
	}
	return runtime.Record(typ, fields), nil
}

// vmGetField — доступ к полю записи. Отсутствующее поле — raise
// (:field_error, (:field, record)).
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
		runtime.Atom("field_error"),
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
	f, err := frameFromFn(fn, args)
	if err != nil {
		return runtime.Unit, err
	}
	tmp := &Actor{
		pid:      -1,
		frames:   []*Frame{f},
		hwm:      defaultHWM,
		watchers: make(map[int]int),
		watching: make(map[int]int),
		status:   actorReady,
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
			tmp.frames[len(tmp.frames)-1] = nil
			tmp.frames = tmp.frames[:len(tmp.frames)-1]
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
	}
	return false
}

// attachTrace кладёт в ErrRaise кадры от места raise к main. Кадры,
// заменённые TAILCALL, в trace не попадают: их уже нет в a.frames.
// У вызывающих кадров ip уже сдвинут за CALL, поэтому берём ip-1.
// Вызывается только на пути непойманного raise.
func attachTrace(a *Actor) {
	var rerr *ErrRaise
	if !errors.As(a.err, &rerr) {
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
		trace = append(trace, TraceFrame{Func: f.name, Pos: f.chunk.PosAt(ip)})
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

	a.frames[len(a.frames)-1] = nil
	a.frames = a.frames[:len(a.frames)-1]

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
		a.frames[len(a.frames)-1] = nil
		a.frames = a.frames[:len(a.frames)-1]
	}
	return false
}

// notBoolErr — raise (:type_error, (:expected_bool, v)) для не-Bool в
// условии if, операнде and/or и guard (строгий Bool, DD #41 вариант A);
// форма payload — как у assert: (:type_error, (:assert_expected_bool, v)).
func notBoolErr(v runtime.Value) error {
	return typeErr("expected_bool", v)
}
