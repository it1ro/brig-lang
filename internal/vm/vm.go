// Package vm — стековая байткод-машина Brig.
//
// ОСОЗНАННОЕ отступление от §15.1: первая машина стековая, не регистровая.
// Трек α: добавлена поддержка замыканий (KindClosure, OpMakeClosure,
// OpGetUpvalue, OpSetUpvalue). Capture-by-value на момент создания.
// v0.4.7: поддержка trap / ensure (§10.2, §10.3).
package vm

import (
	"errors"
	"fmt"
	"math"
	"math/big"

	"github.com/it1ro/brig-lang/internal/runtime"
)

const maxLocals = 64

// ErrRaise — перенос необработанного исключения.
type ErrRaise struct{ Val runtime.Value }

func (e *ErrRaise) Error() string { return "raise: " + e.Val.Inspect() }

// frame — кадр вызова (задел под общий стек кадров).
type frame struct {
	fn   *Function
	ip   int
	base int
}

// trapHandler — активный обработчик исключений одного trap.
//
// ip — адрес перехода при исключении; stackLen — длина операндного
// стека, восстанавливаемая перед переходом (всё, что накопилось
// внутри тела trap, отбрасывается).
type trapHandler struct {
	ip       int
	stackLen int
}

// VM — стековый интерпретатор байткода.
type VM struct {
	globals map[string]runtime.Value
	stack   []runtime.Value
	frames  []frame
}

// New создаёт ВМ с установленной прелюдией.
func New() *VM {
	vm := &VM{globals: make(map[string]runtime.Value)}
	InstallPrelude(vm.globals)
	return vm
}

// DefineGlobal регистрирует глобальное имя.
func (vm *VM) DefineGlobal(name string, v runtime.Value) { vm.globals[name] = v }

// Global возвращает глобальное значение по имени.
func (vm *VM) Global(name string) runtime.Value { return vm.globals[name] }

// FuncValue оборачивает скомпилированную функцию в значение-функцию.
func FuncValue(fn *Function) runtime.Value {
	return runtime.Func(&runtime.FuncValue{
		Name:     fn.Name,
		Arity:    fn.Arity,
		IsNative: false,
		Body:     fn.Chunk, // *Chunk реализует runtime.Code
	})
}

// Call реализует runtime.Caller: прелюдия может вызывать функции.
// Поддерживает KindFunction (обычные) и KindClosure (замыкания).
func (vm *VM) Call(fn runtime.Value, args []runtime.Value) (runtime.Value, error) {
	switch fn.Kind {
	case runtime.KindFunction:
		if fn.Func == nil {
			return runtime.Unit, fmt.Errorf("(:type_error, (:call, nil))")
		}
		f := fn.Func
		if f.IsNative {
			if f.Native == nil {
				return runtime.Unit, fmt.Errorf("internal: nil native %q", f.Name)
			}
			return f.Native(vm, args)
		}
		chunk, ok := f.Body.(*Chunk)
		if !ok || chunk == nil {
			return runtime.Unit, fmt.Errorf("internal: function without chunk")
		}
		return vm.run(Function{Name: f.Name, Arity: f.Arity, Chunk: chunk}, args, nil)

	case runtime.KindClosure:
		cv := fn.ClosureVal
		if cv == nil {
			return runtime.Unit, fmt.Errorf("(:type_error, (:call, nil-closure))")
		}
		chunk, ok := cv.Func.(*Chunk)
		if !ok || chunk == nil {
			return runtime.Unit, fmt.Errorf("internal: closure without chunk")
		}
		return vm.run(
			Function{Name: cv.Name, Arity: cv.Arity, Chunk: chunk},
			args, cv.Captures,
		)

	default:
		return runtime.Unit,
			fmt.Errorf("(:type_error, (:call, %s))", fn.Inspect())
	}
}

// run исполняет один фрейм. captures — захваченные переменные замыкания
// (nil для обычных функций).
func (vm *VM) run(fn Function, args, captures []runtime.Value) (runtime.Value, error) {
	if fn.Arity >= 0 && len(args) != fn.Arity {
		return runtime.Unit, fmt.Errorf("(:function_clause, (%s, %d args))",
			fn.Name, len(args))
	}
	chunk := fn.Chunk
	code := chunk.Code
	consts := chunk.Constants

	locals := make([]runtime.Value, maxLocals)
	copy(locals, args)

	var stack []runtime.Value
	push := func(v runtime.Value) { stack = append(stack, v) }
	pop := func() (runtime.Value, error) {
		if len(stack) == 0 {
			return runtime.Unit, fmt.Errorf("internal: stack underflow in %s", fn.Name)
		}
		v := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		return v, nil
	}
	operand := func(ip int) int {
		return int(code[ip+1])<<8 | int(code[ip+2])
	}

	// Стек активных trap-обработчиков этого кадра.
	var handlers []trapHandler

	ip := 0

	// handleRaise — маршрутизация ErrRaise в ближайший активный handler.
	// Возвращает true, если исключение поглощено (ip и stack обновлены).
	handleRaise := func(err error) bool {
		var rerr *ErrRaise
		if !errors.As(err, &rerr) {
			return false
		}
		if len(handlers) == 0 {
			return false
		}
		h := handlers[len(handlers)-1]
		handlers = handlers[:len(handlers)-1]
		stack = stack[:h.stackLen]
		push(rerr.Val)
		ip = h.ip
		return true
	}

	for ip < len(code) {
		op := OpCode(code[ip])
		switch op {
		case OpConstant:
			push(consts[operand(ip)])
			ip += 3

		case OpPop:
			if _, err := pop(); err != nil {
				return runtime.Unit, err
			}
			ip++

		case OpDup:
			top, err := pop()
			if err != nil {
				return runtime.Unit, err
			}
			push(top)
			push(top)
			ip++

		case OpAdd:
			b, _ := pop()
			a, _ := pop()
			r, err := add(a, b)
			if err != nil {
				return runtime.Unit, err
			}
			push(r)
			ip++

		case OpSub:
			b, _ := pop()
			a, _ := pop()
			r, err := sub(a, b)
			if err != nil {
				return runtime.Unit, err
			}
			push(r)
			ip++

		case OpMul:
			b, _ := pop()
			a, _ := pop()
			r, err := mul(a, b)
			if err != nil {
				return runtime.Unit, err
			}
			push(r)
			ip++

		case OpDiv:
			b, _ := pop()
			a, _ := pop()
			r, err := div(a, b)
			if err != nil {
				if handleRaise(err) {
					continue
				}
				return runtime.Unit, err
			}
			push(r)
			ip++

		case OpIntDiv:
			b, _ := pop()
			a, _ := pop()
			r, err := intDiv(a, b)
			if err != nil {
				if handleRaise(err) {
					continue
				}
				return runtime.Unit, err
			}
			push(r)
			ip++

		case OpRem:
			b, _ := pop()
			a, _ := pop()
			r, err := rem(a, b)
			if err != nil {
				if handleRaise(err) {
					continue
				}
				return runtime.Unit, err
			}
			push(r)
			ip++

		case OpPow:
			b, _ := pop()
			a, _ := pop()
			r, err := pow(a, b)
			if err != nil {
				if handleRaise(err) {
					continue
				}
				return runtime.Unit, err
			}
			push(r)
			ip++

		case OpNeg:
			a, err := pop()
			if err != nil {
				return runtime.Unit, err
			}
			r, err := neg(a)
			if err != nil {
				return runtime.Unit, err
			}
			push(r)
			ip++

		case OpNot:
			a, err := pop()
			if err != nil {
				return runtime.Unit, err
			}
			if a.Kind != runtime.KindBool {
				return runtime.Unit,
					fmt.Errorf("(:type_error, (:not, %s))", a.Inspect())
			}
			push(runtime.Bool(!a.Bool))
			ip++

		case OpEq, OpNeq:
			b, _ := pop()
			a, _ := pop()
			eq := runtime.Equal(a, b)
			if op == OpNeq {
				eq = !eq
			}
			push(runtime.Bool(eq))
			ip++

		case OpLt, OpGt, OpLe, OpGe:
			b, _ := pop()
			a, _ := pop()
			c, err := runtime.Compare(a, b)
			if err != nil {
				return runtime.Unit, err
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
			ip++

		case OpJump:
			ip = operand(ip)

		case OpJumpFalse:
			target := operand(ip)
			v, err := pop()
			if err != nil {
				return runtime.Unit, err
			}
			if v.Kind == runtime.KindBool && !v.Bool {
				ip = target
			} else {
				ip += 3
			}

		case OpJumpTrue:
			target := operand(ip)
			v, err := pop()
			if err != nil {
				return runtime.Unit, err
			}
			if v.Kind == runtime.KindBool && v.Bool {
				ip = target
			} else {
				ip += 3
			}

		case OpGetLocal:
			idx := operand(ip)
			if idx >= len(locals) {
				return runtime.Unit, fmt.Errorf("internal: local %d out of range", idx)
			}
			push(locals[idx])
			ip += 3

		case OpSetLocal:
			idx := operand(ip)
			if idx >= len(locals) {
				return runtime.Unit, fmt.Errorf("internal: local %d out of range", idx)
			}
			v, err := pop()
			if err != nil {
				return runtime.Unit, err
			}
			locals[idx] = v
			ip += 3

		case OpGetGlobal:
			name := consts[operand(ip)].Str
			g, ok := vm.globals[name]
			if !ok {
				return runtime.Unit, fmt.Errorf("undefined: %s", name)
			}
			push(g)
			ip += 3

		case OpSetGlobal:
			name := consts[operand(ip)].Str
			v, err := pop()
			if err != nil {
				return runtime.Unit, err
			}
			vm.globals[name] = v
			ip += 3

		case OpCall:
			argc := operand(ip)
			callArgs := make([]runtime.Value, argc)
			for i := argc - 1; i >= 0; i-- {
				v, err := pop()
				if err != nil {
					return runtime.Unit, err
				}
				callArgs[i] = v
			}
			callee, err := pop()
			if err != nil {
				return runtime.Unit, err
			}
			r, err := vm.Call(callee, callArgs)
			if err != nil {
				if handleRaise(err) {
					continue
				}
				return runtime.Unit, err
			}
			push(r)
			ip += 3

		case OpReturn:
			v, err := pop()
			if err != nil {
				return runtime.Unit, err
			}
			return v, nil

		case OpTuple, OpList, OpVector:
			n := operand(ip)
			vs := make([]runtime.Value, n)
			for i := n - 1; i >= 0; i-- {
				v, err := pop()
				if err != nil {
					return runtime.Unit, err
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
			ip += 3

		case OpMap:
			n := operand(ip)
			entries := make([]runtime.MapEntry, n)
			for i := n - 1; i >= 0; i-- {
				val, err := pop()
				if err != nil {
					return runtime.Unit, err
				}
				key, err := pop()
				if err != nil {
					return runtime.Unit, err
				}
				entries[i] = runtime.MapEntry{Key: key, Val: val}
			}
			push(runtime.Map(entries))
			ip += 3

		case OpRaise:
			v, err := pop()
			if err != nil {
				return runtime.Unit, err
			}
			if handleRaise(&ErrRaise{Val: v}) {
				continue
			}
			return runtime.Unit, &ErrRaise{Val: v}

		// ---- Трек α: замыкания ----

		case OpMakeClosure:
			n := operand(ip) // число захватов
			caps := make([]runtime.Value, n)
			for i := n - 1; i >= 0; i-- {
				v, err := pop()
				if err != nil {
					return runtime.Unit, err
				}
				caps[i] = v
			}
			fnVal, err := pop()
			if err != nil {
				return runtime.Unit, err
			}
			if fnVal.Kind != runtime.KindFunction || fnVal.Func == nil {
				return runtime.Unit,
					fmt.Errorf("internal: MAKECLOSURE expects function on stack")
			}
			fv := fnVal.Func
			push(runtime.MakeClosure(fv.Name, fv.Arity, fv.Body, caps))
			ip += 3

		case OpGetUpvalue:
			idx := operand(ip)
			if idx >= len(captures) {
				return runtime.Unit,
					fmt.Errorf("internal: upvalue %d out of range in %s", idx, fn.Name)
			}
			push(captures[idx])
			ip += 3

		case OpSetUpvalue:
			idx := operand(ip)
			if idx >= len(captures) {
				return runtime.Unit,
					fmt.Errorf("internal: upvalue %d out of range in %s", idx, fn.Name)
			}
			v, err := pop()
			if err != nil {
				return runtime.Unit, err
			}
			captures[idx] = v
			ip += 3

		case OpCloseUpvalue, OpDefineLocalFn:
			return runtime.Unit,
				fmt.Errorf("internal: opcode %s not yet implemented", op)

		// ---- v0.4.7: trap / ensure (§10.2, §10.3) ----

		case OpTrapBegin:
			handlers = append(handlers, trapHandler{
				ip:       operand(ip),
				stackLen: len(stack),
			})
			ip += 3

		case OpTrapEnd:
			if len(handlers) == 0 {
				return runtime.Unit,
					fmt.Errorf("internal: TRAPEND without active handler in %s", fn.Name)
			}
			handlers = handlers[:len(handlers)-1]
			ip++

		case OpMakeOk:
			v, err := pop()
			if err != nil {
				return runtime.Unit, err
			}
			push(runtime.Variant("Ok", v))
			ip++

		case OpMakeError:
			v, err := pop()
			if err != nil {
				return runtime.Unit, err
			}
			push(runtime.Variant("Error", v))
			ip++

		default:
			return runtime.Unit,
				fmt.Errorf("internal: unknown opcode %d at %d in %s", op, ip, fn.Name)
		}
	}

	if len(stack) > 0 {
		return stack[len(stack)-1], nil
	}
	return runtime.Unit, nil
}

// ---- арифметика (§7.3) ----

func add(a, b runtime.Value) (runtime.Value, error) {
	if a.Kind == runtime.KindStr && b.Kind == runtime.KindStr {
		return runtime.Str(a.Str + b.Str), nil
	}
	if a.Kind == runtime.KindList && b.Kind == runtime.KindList {
		joined := make([]runtime.Value, 0, len(a.List)+len(b.List))
		joined = append(joined, a.List...)
		joined = append(joined, b.List...)
		return runtime.List(joined...), nil
	}
	if !bothNum(a, b) {
		return runtime.Unit, arithErr(a, b, ":add")
	}
	if a.Kind == runtime.KindFloat || b.Kind == runtime.KindFloat {
		return runtime.Float(numToFloat(a) + numToFloat(b)), nil
	}
	return runtime.IntBig(new(big.Int).Add(a.Int, b.Int)), nil
}

func sub(a, b runtime.Value) (runtime.Value, error) {
	if !bothNum(a, b) {
		return runtime.Unit, arithErr(a, b, ":sub")
	}
	if a.Kind == runtime.KindFloat || b.Kind == runtime.KindFloat {
		return runtime.Float(numToFloat(a) - numToFloat(b)), nil
	}
	return runtime.IntBig(new(big.Int).Sub(a.Int, b.Int)), nil
}

func mul(a, b runtime.Value) (runtime.Value, error) {
	if !bothNum(a, b) {
		return runtime.Unit, arithErr(a, b, ":mul")
	}
	if a.Kind == runtime.KindFloat || b.Kind == runtime.KindFloat {
		return runtime.Float(numToFloat(a) * numToFloat(b)), nil
	}
	return runtime.IntBig(new(big.Int).Mul(a.Int, b.Int)), nil
}

func div(a, b runtime.Value) (runtime.Value, error) {
	if !bothNum(a, b) {
		return runtime.Unit, arithErr(a, b, ":div")
	}
	if numToFloat(b) == 0 {
		return runtime.Unit, &ErrRaise{Val: runtime.Tuple(
			runtime.Atom("division_by_zero"), runtime.Unit)}
	}
	return runtime.Float(numToFloat(a) / numToFloat(b)), nil
}

func intDiv(a, b runtime.Value) (runtime.Value, error) {
	if a.Kind != runtime.KindInt || b.Kind != runtime.KindInt {
		return runtime.Unit, arithErr(a, b, ":div")
	}
	if b.Int.Sign() == 0 {
		return runtime.Unit, &ErrRaise{Val: runtime.Tuple(
			runtime.Atom("division_by_zero"), runtime.Unit)}
	}
	return runtime.IntBig(new(big.Int).Quo(a.Int, b.Int)), nil
}

func rem(a, b runtime.Value) (runtime.Value, error) {
	if a.Kind != runtime.KindInt || b.Kind != runtime.KindInt {
		return runtime.Unit, arithErr(a, b, ":rem")
	}
	if b.Int.Sign() == 0 {
		return runtime.Unit, &ErrRaise{Val: runtime.Tuple(
			runtime.Atom("division_by_zero"), runtime.Unit)}
	}
	return runtime.IntBig(new(big.Int).Rem(a.Int, b.Int)), nil
}

func pow(a, b runtime.Value) (runtime.Value, error) {
	if !bothNum(a, b) {
		return runtime.Unit, arithErr(a, b, ":pow")
	}
	if a.Kind == runtime.KindInt && b.Kind == runtime.KindInt && b.Int.Sign() >= 0 {
		return runtime.IntBig(new(big.Int).Exp(a.Int, b.Int, nil)), nil
	}
	return runtime.Float(math.Pow(numToFloat(a), numToFloat(b))), nil
}

func neg(a runtime.Value) (runtime.Value, error) {
	switch a.Kind {
	case runtime.KindInt:
		return runtime.IntBig(new(big.Int).Neg(a.Int)), nil
	case runtime.KindFloat:
		return runtime.Float(-a.Float), nil
	}
	return runtime.Unit, fmt.Errorf("(:type_error, (:neg, %s))", a.Inspect())
}

func bothNum(a, b runtime.Value) bool {
	return (a.Kind == runtime.KindInt || a.Kind == runtime.KindFloat) &&
		(b.Kind == runtime.KindInt || b.Kind == runtime.KindFloat)
}

func numToFloat(v runtime.Value) float64 {
	if v.Kind == runtime.KindFloat {
		return v.Float
	}
	f, _ := new(big.Float).SetInt(v.Int).Float64()
	return f
}

func arithErr(a, b runtime.Value, op string) error {
	return fmt.Errorf("(:type_error, (%s, (%s, %s)))", op, a.Inspect(), b.Inspect())
}
