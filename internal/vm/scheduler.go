package vm

import (
	"errors"
	"fmt"
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

// Frame — кадр вызова.
type Frame struct {
	fn       *Function
	ip       int
	stack    []runtime.Value
	locals   []runtime.Value
	captures []runtime.Value
	handlers []trapHandler
}

// trapHandler — активный trap (§10.2).
type trapHandler struct {
	ip       int
	stackLen int
}

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
}

// Scheduler — единый run-loop (§15.2).
type Scheduler struct {
	vm      *VM
	actors  map[int]*Actor
	nextPid int
	nextRef int
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

func frameFromFn(fn runtime.Value, args []runtime.Value) (*Frame, error) {
	var chunk *Chunk
	var captures []runtime.Value
	var fnName string
	var arity int

	switch fn.Kind {
	case runtime.KindFunction:
		if fn.Func == nil {
			return nil, fmt.Errorf("(:type_error, (:spawn, nil))")
		}
		c, ok := fn.Func.Body.(*Chunk)
		if !ok {
			return nil, fmt.Errorf("(:type_error, (:spawn, %s))", fn.Inspect())
		}
		chunk = c
		fnName = fn.Func.Name
		arity = fn.Func.Arity
	case runtime.KindClosure:
		if fn.ClosureVal == nil {
			return nil, fmt.Errorf("(:type_error, (:spawn, nil-closure))")
		}
		c, ok := fn.ClosureVal.Func.(*Chunk)
		if !ok {
			return nil, fmt.Errorf("(:type_error, (:spawn, %s))", fn.Inspect())
		}
		chunk = c
		captures = fn.ClosureVal.Captures
		fnName = fn.ClosureVal.Name
		arity = fn.ClosureVal.Arity
	default:
		return nil, fmt.Errorf("(:type_error, (:spawn, %s))", fn.Inspect())
	}

	if arity >= 0 && len(args) != arity {
		return nil, fmt.Errorf("(:function_clause, (spawn %s, %d args))", fnName, len(args))
	}

	f := &Frame{
		fn:       &Function{Name: fnName, Arity: arity, Chunk: chunk},
		locals:   make([]runtime.Value, maxLocals),
		captures: captures,
	}
	copy(f.locals, args)
	return f, nil
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

func (s *Scheduler) wakeExpired() {
	now := time.Now()
	for _, a := range s.actors {
		if a.status != actorBlocked {
			continue
		}
		if a.recvDeadline.IsZero() || a.recvDeadline.After(now) {
			continue
		}
		a.status = actorReady
		s.ready = append(s.ready, a)
	}
}

// ---- run slice ----

func (s *Scheduler) runSlice(a *Actor) {
	reds := s.reds
	for reds > 0 {
		if len(a.frames) == 0 {
			a.status = actorDone
			a.result = runtime.Unit
			s.notifyWatchers(a, runtime.Atom("normal"))
			return
		}

		f := a.frames[len(a.frames)-1]
		outcome := s.stepFrame(a, f)
		switch outcome {
		case stepDone:
			a.frames = a.frames[:len(a.frames)-1]
			if len(a.frames) == 0 {
				a.status = actorDone
				s.notifyWatchers(a, runtime.Atom("normal"))
				return
			}
			caller := a.frames[len(a.frames)-1]
			caller.stack = append(caller.stack, a.result)
			reds--

		case stepFailed:
			if s.tryUnwindRaise(a) {
				reds--
				continue
			}
			a.status = actorFailed
			s.notifyWatchers(a, runtime.Tuple(runtime.Atom("raise"), a.result))
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

type stepOutcome int

const (
	stepContinue stepOutcome = iota
	stepYield
	stepBlock
	stepDone
	stepFailed
)

// ---- stepFrame ----

func (s *Scheduler) stepFrame(a *Actor, f *Frame) stepOutcome {
	chunk := f.fn.Chunk
	code := chunk.Code
	consts := chunk.Constants

	operand := func(ip int) int {
		return int(code[ip+1])<<8 | int(code[ip+2])
	}
	operand2 := func(ip int) int {
		return int(code[ip+3])<<8 | int(code[ip+4])
	}

	push := func(v runtime.Value) { f.stack = append(f.stack, v) }
	pop := func() (runtime.Value, error) {
		if len(f.stack) == 0 {
			return runtime.Unit, fmt.Errorf("internal: stack underflow in %s", f.fn.Name)
		}
		v := f.stack[len(f.stack)-1]
		f.stack = f.stack[:len(f.stack)-1]
		return v, nil
	}

	handleRaise := func(err error) bool {
		var rerr *ErrRaise
		if !errors.As(err, &rerr) {
			return false
		}
		if len(f.handlers) == 0 {
			return false
		}
		h := f.handlers[len(f.handlers)-1]
		f.handlers = f.handlers[:len(f.handlers)-1]
		f.stack = f.stack[:h.stackLen]
		push(rerr.Val)
		f.ip = h.ip
		return true
	}

	fail := func(err error) stepOutcome {
		a.err = err
		a.result = runtime.Unit
		return stepFailed
	}

	for f.ip < len(code) {
		op := OpCode(code[f.ip])
		switch op {
		case OpConstant:
			push(consts[operand(f.ip)])
			f.ip += 3

		case OpPop:
			if _, err := pop(); err != nil {
				return fail(err)
			}
			f.ip++

		case OpDup:
			top, err := pop()
			if err != nil {
				return fail(err)
			}
			push(top)
			push(top)
			f.ip++

		case OpAdd:
			b, _ := pop()
			aa, _ := pop()
			r, err := add(aa, b)
			if err != nil {
				return fail(err)
			}
			push(r)
			f.ip++

		case OpSub:
			b, _ := pop()
			aa, _ := pop()
			r, err := sub(aa, b)
			if err != nil {
				return fail(err)
			}
			push(r)
			f.ip++

		case OpMul:
			b, _ := pop()
			aa, _ := pop()
			r, err := mul(aa, b)
			if err != nil {
				return fail(err)
			}
			push(r)
			f.ip++

		case OpDiv:
			b, _ := pop()
			aa, _ := pop()
			r, err := div(aa, b)
			if err != nil {
				if handleRaise(err) {
					continue
				}
				return fail(err)
			}
			push(r)
			f.ip++

		case OpIntDiv:
			b, _ := pop()
			aa, _ := pop()
			r, err := intDiv(aa, b)
			if err != nil {
				if handleRaise(err) {
					continue
				}
				return fail(err)
			}
			push(r)
			f.ip++

		case OpRem:
			b, _ := pop()
			aa, _ := pop()
			r, err := rem(aa, b)
			if err != nil {
				if handleRaise(err) {
					continue
				}
				return fail(err)
			}
			push(r)
			f.ip++

		case OpPow:
			b, _ := pop()
			aa, _ := pop()
			r, err := pow(aa, b)
			if err != nil {
				return fail(err)
			}
			push(r)
			f.ip++

		case OpNeg:
			aa, err := pop()
			if err != nil {
				return fail(err)
			}
			r, err := neg(aa)
			if err != nil {
				return fail(err)
			}
			push(r)
			f.ip++

		case OpNot:
			aa, err := pop()
			if err != nil {
				return fail(err)
			}
			if aa.Kind != runtime.KindBool {
				return fail(fmt.Errorf("(:type_error, (:not, %s))", aa.Inspect()))
			}
			push(runtime.Bool(!aa.Bool))
			f.ip++

		case OpEq, OpNeq:
			b, _ := pop()
			aa, _ := pop()
			eq := runtime.Equal(aa, b)
			if op == OpNeq {
				eq = !eq
			}
			push(runtime.Bool(eq))
			f.ip++

		case OpLt, OpGt, OpLe, OpGe:
			b, _ := pop()
			aa, _ := pop()
			c, err := runtime.Compare(aa, b)
			if err != nil {
				return fail(err)
			}
			var r bool
			switch op {
			case OpLt:
				r = c < 0
			case OpGt:
				r = c > 0
			case OpLe:
				r = c <= 0
			case OpGe:
				r = c >= 0
			}
			push(runtime.Bool(r))
			f.ip++

		case OpJump:
			f.ip = operand(f.ip)

		case OpJumpFalse:
			target := operand(f.ip)
			v, err := pop()
			if err != nil {
				return fail(err)
			}
			if v.Kind == runtime.KindBool && !v.Bool {
				f.ip = target
			} else {
				f.ip += 3
			}

		case OpJumpTrue:
			target := operand(f.ip)
			v, err := pop()
			if err != nil {
				return fail(err)
			}
			if v.Kind == runtime.KindBool && v.Bool {
				f.ip = target
			} else {
				f.ip += 3
			}

		case OpGetLocal:
			idx := operand(f.ip)
			if idx >= len(f.locals) {
				return fail(fmt.Errorf("internal: local %d out of range", idx))
			}
			push(f.locals[idx])
			f.ip += 3

		case OpSetLocal:
			idx := operand(f.ip)
			if idx >= len(f.locals) {
				return fail(fmt.Errorf("internal: local %d out of range", idx))
			}
			v, err := pop()
			if err != nil {
				return fail(err)
			}
			f.locals[idx] = v
			f.ip += 3

		case OpGetGlobal:
			name := consts[operand(f.ip)].Str
			g, ok := s.vm.globals[name]
			if !ok {
				return fail(fmt.Errorf("undefined: %s", name))
			}
			push(g)
			f.ip += 3

		case OpSetGlobal:
			name := consts[operand(f.ip)].Str
			v, err := pop()
			if err != nil {
				return fail(err)
			}
			s.vm.globals[name] = v
			f.ip += 3

		case OpCall:
			argc := operand(f.ip)
			callArgs := make([]runtime.Value, argc)
			for i := argc - 1; i >= 0; i-- {
				v, err := pop()
				if err != nil {
					return fail(err)
				}
				callArgs[i] = v
			}
			callee, err := pop()
			if err != nil {
				return fail(err)
			}

			if callee.Kind == runtime.KindFunction &&
				callee.Func != nil && callee.Func.IsNative {
				r, err := callee.Func.Native(s.vm, callArgs)
				if err != nil {
					if handleRaise(err) {
						continue
					}
					return fail(err)
				}
				push(r)
				f.ip += 3
				continue
			}

			newFrame, err := frameFromFn(callee, callArgs)
			if err != nil {
				if handleRaise(err) {
					continue
				}
				return fail(err)
			}

			if len(f.handlers) == 0 && isTailCall(code, f.ip) {
				a.frames[len(a.frames)-1] = newFrame
				return stepContinue
			}

			a.frames = append(a.frames, newFrame)
			f.ip += 3
			return stepContinue
		case OpReturn:
			v, err := pop()
			if err != nil {
				return fail(err)
			}
			a.result = v
			return stepDone

		case OpTuple, OpList, OpVector:
			n := operand(f.ip)
			vs := make([]runtime.Value, n)
			for i := n - 1; i >= 0; i-- {
				v, err := pop()
				if err != nil {
					return fail(err)
				}
				vs[i] = v
			}
			switch op {
			case OpTuple:
				push(runtime.Tuple(vs...))
			case OpList:
				push(runtime.List(vs...))
			case OpVector:
				push(runtime.Vector(vs...))
			}
			f.ip += 3

		case OpMap:
			n := operand(f.ip)
			entries := make([]runtime.MapEntry, n)
			for i := n - 1; i >= 0; i-- {
				val, err := pop()
				if err != nil {
					return fail(err)
				}
				key, err := pop()
				if err != nil {
					return fail(err)
				}
				entries[i] = runtime.MapEntry{Key: key, Val: val}
			}
			push(runtime.Map(entries))
			f.ip += 3

		case OpRange:
			endV, err := pop()
			if err != nil {
				return fail(err)
			}
			startV, err := pop()
			if err != nil {
				return fail(err)
			}
			r, err := vmMakeRange(startV, endV)
			if err != nil {
				if handleRaise(err) {
					continue
				}
				return fail(err)
			}
			push(r)
			f.ip++

		case OpIndex:
			idx, err := pop()
			if err != nil {
				return fail(err)
			}
			obj, err := pop()
			if err != nil {
				return fail(err)
			}
			r, err := vmIndex(obj, idx)
			if err != nil {
				if handleRaise(err) {
					continue
				}
				return fail(err)
			}
			push(r)
			f.ip++

		case OpRaise:
			v, err := pop()
			if err != nil {
				return fail(err)
			}
			if handleRaise(&ErrRaise{Val: v}) {
				continue
			}
			return fail(&ErrRaise{Val: v})

		case OpMakeClosure:
			n := operand(f.ip)
			caps := make([]runtime.Value, n)
			for i := n - 1; i >= 0; i-- {
				v, err := pop()
				if err != nil {
					return fail(err)
				}
				caps[i] = v
			}
			fnVal, err := pop()
			if err != nil {
				return fail(err)
			}
			if fnVal.Kind != runtime.KindFunction || fnVal.Func == nil {
				return fail(fmt.Errorf("internal: MAKECLOSURE expects function"))
			}
			fv := fnVal.Func
			push(runtime.MakeClosure(fv.Name, fv.Arity, fv.Body, caps))
			f.ip += 3

		case OpGetUpvalue:
			idx := operand(f.ip)
			if idx >= len(f.captures) {
				return fail(fmt.Errorf("internal: upvalue %d out of range", idx))
			}
			push(f.captures[idx])
			f.ip += 3

		case OpSetUpvalue:
			idx := operand(f.ip)
			if idx >= len(f.captures) {
				return fail(fmt.Errorf("internal: upvalue %d out of range", idx))
			}
			v, err := pop()
			if err != nil {
				return fail(err)
			}
			f.captures[idx] = v
			f.ip += 3

		case OpTrapBegin:
			f.handlers = append(f.handlers, trapHandler{
				ip:       operand(f.ip),
				stackLen: len(f.stack),
			})
			f.ip += 3

		case OpTrapEnd:
			if len(f.handlers) == 0 {
				return fail(fmt.Errorf("internal: TRAPEND without handler"))
			}
			f.handlers = f.handlers[:len(f.handlers)-1]
			f.ip++

		case OpMakeOk:
			v, err := pop()
			if err != nil {
				return fail(err)
			}
			push(runtime.Variant("Ok", v))
			f.ip++

		case OpMakeError:
			v, err := pop()
			if err != nil {
				return fail(err)
			}
			push(runtime.Variant("Error", v))
			f.ip++

		case OpSpawn:
			linked := operand(f.ip) == 1
			fn, err := pop()
			if err != nil {
				return fail(err)
			}
			pid, err := s.Spawn(fn, nil)
			if err != nil {
				return fail(err)
			}
			if linked {
				s.Watch(a.pid, pid)
			}
			push(runtime.Value{Kind: runtime.KindPid, Pid: pid})
			f.ip += 3

		case OpSend:
			msg, err := pop()
			if err != nil {
				return fail(err)
			}
			pidVal, err := pop()
			if err != nil {
				return fail(err)
			}
			if pidVal.Kind != runtime.KindPid {
				return fail(fmt.Errorf("(:type_error, (:send, %s))", pidVal.Inspect()))
			}
			push(s.Send(pidVal.Pid, msg))
			f.ip++

		case OpSelf:
			push(runtime.Value{Kind: runtime.KindPid, Pid: a.pid})
			f.ip++

		case OpMakeRef:
			ref := s.nextRef
			s.nextRef++
			push(runtime.Value{Kind: runtime.KindRef, Ref: ref})
			f.ip++

		case OpWatch:
			pidVal, err := pop()
			if err != nil {
				return fail(err)
			}
			if pidVal.Kind != runtime.KindPid {
				return fail(fmt.Errorf("(:type_error, (:watch, %s))", pidVal.Inspect()))
			}
			ref := s.Watch(a.pid, pidVal.Pid)
			push(runtime.Value{Kind: runtime.KindRef, Ref: ref})
			f.ip++

		case OpUnwatch:
			refVal, err := pop()
			if err != nil {
				return fail(err)
			}
			if refVal.Kind != runtime.KindRef {
				return fail(fmt.Errorf("(:type_error, (:unwatch, %s))", refVal.Inspect()))
			}
			s.Unwatch(a.pid, refVal.Ref)
			push(runtime.Unit)
			f.ip++

		case OpMailboxSize:
			pidVal, err := pop()
			if err != nil {
				return fail(err)
			}
			if pidVal.Kind != runtime.KindPid {
				return fail(fmt.Errorf("(:type_error, (:mailbox_size, %s))", pidVal.Inspect()))
			}
			if t, ok := s.actors[pidVal.Pid]; ok {
				push(runtime.Int(int64(len(t.mailbox))))
			} else {
				push(runtime.Int(0))
			}
			f.ip++

		case OpYield:
			f.ip++
			return stepYield

		case OpRecvTimer:
			msVal, err := pop()
			if err != nil {
				return fail(err)
			}
			if msVal.Kind != runtime.KindInt {
				return fail(fmt.Errorf("(:type_error, (:after, %s))", msVal.Inspect()))
			}
			var ms int64
			if msVal.IsSmall {
				ms = msVal.SmallInt
			} else {
				ms = msVal.AsBig().Int64()
			}
			a.recvDeadline = time.Now().Add(time.Duration(ms) * time.Millisecond)
			f.ip++
		case OpRecvTake:
			slot := operand(f.ip)
			after := operand2(f.ip)
			f.ip += 5

			if len(a.downMsgs) > 0 {
				msg := a.downMsgs[0]
				a.downMsgs = a.downMsgs[1:]
				a.recvDeadline = time.Time{}
				if slot < len(f.locals) {
					f.locals[slot] = msg
				}
				continue
			}
			if len(a.mailbox) > 0 {
				msg := a.mailbox[0]
				a.mailbox = a.mailbox[1:]
				a.recvDeadline = time.Time{}
				if slot < len(f.locals) {
					f.locals[slot] = msg
				}
				continue
			}

			if !a.recvDeadline.IsZero() && !time.Now().Before(a.recvDeadline) {
				a.recvDeadline = time.Time{}
				if after != 0xFFFF {
					f.ip = after
					continue
				}
			}

			f.ip -= 5
			return stepBlock

		case OpMatchLocal:
			slot := operand(f.ip)
			patIdx := operand2(f.ip)
			f.ip += 5

			if slot >= len(f.locals) {
				return fail(fmt.Errorf("internal: match slot %d out of range", slot))
			}
			if patIdx >= len(chunk.Patterns) {
				return fail(fmt.Errorf("internal: pattern %d out of range", patIdx))
			}
			pat := chunk.Patterns[patIdx]
			if !MatchPattern(f.locals[slot], pat, f.locals) {
				f.ip = pat.FailAddr
			}

		default:
			return fail(fmt.Errorf("internal: unknown opcode %d at %d in %s", op, f.ip, f.fn.Name))
		}
	}

	return fail(fmt.Errorf("internal: fell off end of %s", f.fn.Name))
}

// ---- helpers for OpRange / OpIndex ----

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
			tmp.frames = tmp.frames[:len(tmp.frames)-1]
			if len(tmp.frames) == 0 {
				return tmp.result, nil
			}
			caller := tmp.frames[len(tmp.frames)-1]
			caller.stack = append(caller.stack, tmp.result)
		case stepFailed:
			return runtime.Unit, tmp.err
		case stepBlock:
			return runtime.Unit,
				fmt.Errorf("internal: recv in synchronous call context")
		case stepYield, stepContinue:
			// продолжаем
		}
	}
}

// isTailCall reports whether the OpCall at byte offset ip in code is in
// tail position of its enclosing function.
func isTailCall(code []byte, ip int) bool {
	next := ip + 3
	if next >= len(code) {
		return false
	}
	switch OpCode(code[next]) {
	case OpReturn:
		return true
	case OpJump:
		if next+2 >= len(code) {
			return false
		}
		target := int(code[next+1])<<8 | int(code[next+2])
		return target < len(code) && OpCode(code[target]) == OpReturn
	}
	return false
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

	a.frames = a.frames[:len(a.frames)-1]

	for len(a.frames) > 0 {
		parent := a.frames[len(a.frames)-1]
		if len(parent.handlers) > 0 {
			h := parent.handlers[len(parent.handlers)-1]
			parent.handlers = parent.handlers[:len(parent.handlers)-1]
			parent.stack = parent.stack[:h.stackLen]
			parent.stack = append(parent.stack, rerr.Val)
			parent.ip = h.ip
			a.err = nil
			a.result = runtime.Unit
			return true
		}
		a.frames = a.frames[:len(a.frames)-1]
	}
	return false
}
