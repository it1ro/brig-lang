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

// resolveCallee разбирает Function/Closure с байткод-телом.
func resolveCallee(fn runtime.Value) (callee, error) {
	switch fn.Kind {
	case runtime.KindFunction:
		if fn.Func == nil {
			return callee{}, fmt.Errorf("(:type_error, (:call, nil))")
		}
		ch, ok := fn.Func.Body.(*Chunk)
		if !ok {
			return callee{}, fmt.Errorf("(:type_error, (:call, %s))", fn.Inspect())
		}
		return callee{chunk: ch, name: fn.Func.Name}, nil
	case runtime.KindClosure:
		if fn.ClosureVal == nil {
			return callee{}, fmt.Errorf("(:type_error, (:call, nil-closure))")
		}
		ch, ok := fn.ClosureVal.Func.(*Chunk)
		if !ok {
			return callee{}, fmt.Errorf("(:type_error, (:call, %s))", fn.Inspect())
		}
		return callee{
			chunk:    ch,
			name:     fn.ClosureVal.Name,
			captures: fn.ClosureVal.Captures,
		}, nil
	}
	return callee{}, fmt.Errorf("(:type_error, (:call, %s))", fn.Inspect())
}

// checkArity: variadic — argc >= NumParams-1, иначе argc == NumParams.
func checkArity(c callee, argc int) error {
	if c.chunk.Variadic {
		if argc < c.chunk.NumParams-1 {
			return fmt.Errorf("(:function_clause, (%s, %d args))", c.name, argc)
		}
		return nil
	}
	if argc != c.chunk.NumParams {
		return fmt.Errorf("(:function_clause, (%s, %d args))", c.name, argc)
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

// frameFromFn создаёт кадр вызова.
func frameFromFn(fn runtime.Value, args []runtime.Value) (*Frame, error) {
	c, err := resolveCallee(fn)
	if err != nil {
		return nil, err
	}
	if err := checkArity(c, len(args)); err != nil {
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
				return fail(fmt.Errorf("(:type_error, (:not, %s))", v.Inspect()))
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
			c, err := runtime.Compare(av, bv)
			if err != nil {
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
			if v.Kind == runtime.KindBool && !v.Bool {
				f.ip += 1 + in.SBx()
			} else {
				f.ip++
			}

		case JMPIF:
			v := regs[in.A()]
			if v.Kind == runtime.KindBool && v.Bool {
				f.ip += 1 + in.SBx()
			} else {
				f.ip++
			}

		case CALL:
			base, argc, dst := in.A(), in.B(), in.C()
			cv := regs[base]
			args := regs[base+1 : base+1+argc]

			if cv.Kind == runtime.KindFunction && cv.Func != nil && cv.Func.IsNative {
				if ar := cv.Func.Arity; ar >= 0 && ar != argc {
					return fail(fmt.Errorf(
						"(:function_clause, (%s, %d args))", cv.Func.Name, argc))
				}
				fresh := append([]runtime.Value(nil), args...) // K-5
				r, err := cv.Func.Native(s.vm, fresh)
				if err != nil {
					if f.catch(err) {
						continue
					}
					return fail(err)
				}
				regs[dst] = r
				f.ip++
				continue
			}

			nf, err := frameFromFn(cv, args)
			if err != nil {
				if f.catch(err) {
					continue
				}
				return fail(err)
			}
			f.callDst = dst
			f.ip++
			a.frames = append(a.frames, nf)
			return stepContinue

		case TAILCALL:
			base, argc := in.A(), in.B()
			cv := regs[base]
			args := regs[base+1 : base+1+argc]

			if len(f.handlers) != 0 {
				return fail(errors.New("internal: TAILCALL under active trap"))
			}

			if cv.Kind == runtime.KindFunction && cv.Func != nil && cv.Func.IsNative {
				if ar := cv.Func.Arity; ar >= 0 && ar != argc {
					return fail(fmt.Errorf(
						"(:function_clause, (%s, %d args))", cv.Func.Name, argc))
				}
				r, err := cv.Func.Native(s.vm, append([]runtime.Value(nil), args...))
				if err != nil {
					return fail(err)
				}
				a.result = r
				return stepDone
			}

			c, err := resolveCallee(cv)
			if err == nil {
				err = checkArity(c, argc)
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
				return fail(fmt.Errorf(
					"(:type_error, (:send, %s))", pidVal.Inspect()))
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
				return fail(fmt.Errorf(
					"(:type_error, (:watch, %s))", pidVal.Inspect()))
			}
			ref := s.Watch(a.pid, pidVal.Pid)
			regs[in.A()] = runtime.Value{Kind: runtime.KindRef, Ref: ref}
			f.ip++

		case UNWATCH:
			refVal := regs[in.B()]
			if refVal.Kind != runtime.KindRef {
				return fail(fmt.Errorf(
					"(:type_error, (:unwatch, %s))", refVal.Inspect()))
			}
			s.Unwatch(a.pid, refVal.Ref)
			regs[in.A()] = runtime.Unit
			f.ip++

		case MAILBOXSIZE:
			pidVal := regs[in.B()]
			if pidVal.Kind != runtime.KindPid {
				return fail(fmt.Errorf(
					"(:type_error, (:mailbox_size, %s))", pidVal.Inspect()))
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
				return fail(fmt.Errorf(
					"(:type_error, (:after, %s))", msVal.Inspect()))
			}
			var ms int64
			if msVal.IsSmall {
				ms = msVal.SmallInt
			} else {
				if !msVal.AsBig().IsInt64() {
					return fail(fmt.Errorf(
						"(:type_error, (:after, %s))", msVal.Inspect()))
				}
				ms = msVal.AsBig().Int64()
			}
			d, ok := recvTimerDuration(ms)
			if !ok {
				return fail(fmt.Errorf(
					"(:type_error, (:after, %s))", msVal.Inspect()))
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
		return runtime.Unit, fmt.Errorf("(:type_error, (:range, (%s, %s)))",
			startV.Inspect(), endV.Inspect())
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
	return runtime.Unit, fmt.Errorf("(:type_error, (:index, %s))", obj.Inspect())
}

func indexToInt(v runtime.Value) (int64, error) {
	if v.Kind != runtime.KindInt {
		return 0, fmt.Errorf("(:type_error, (:index_key, %s))", v.Inspect())
	}
	if v.IsSmall {
		return v.SmallInt, nil
	}
	if !v.AsBig().IsInt64() {
		return 0, fmt.Errorf("(:type_error, (:index_key, %s))", v.Inspect())
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
