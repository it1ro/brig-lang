// Package vm — стековая байткод-машина Brig.
//
// ОСОЗНАННОЕ отступление от §15.1: первая машина стековая, не регистровая —
// ради быстрого получения исполняемого пайплайна и отладки наблюдаемой
// семантики. Миграция на регистровую модель (плюс дизассемблер
// --dump-bytecode и редукционные yield-точки §15.2) — отдельный подэтап.
//
// Модель исполнения в срезе: каждый вызов функции рекурсивно вызывает
// vm.Call → run, где фрейм получает собственный локальный стек. Это
// использует Go-стек вместо стека кадра и упрощает срез; полноценные
// кадры на общем стеке — в миграции на регистровую ВМ.
package vm

import (
	"fmt"
	"math"
	"math/big"

	"github.com/it1ro/brig-lang/internal/runtime"
)

// maxLocals — резерв локалей на кадр в срезе.
// Компонент назначает индексы локалей последовательно; 64 покрывает
// типовые функции. Динамический расчёт числа локалей придёт с
// регистровой ВМ.
const maxLocals = 64

// ErrRaise — перенос необработанного исключения (значение ошибки).
//
// Срез: без полноценного стекового трейса (открытый вопрос §17.1).
// При выходе из run ошибка всплывает в cmd/brig → exit code 2.
type ErrRaise struct{ Val runtime.Value }

func (e *ErrRaise) Error() string { return "raise: " + e.Val.Inspect() }

// frame — кадр вызова (задел под общий стек кадров).
//
// В срезе поля не используются полноценно: исполнение идёт через
// рекурсию с локальными стеками. Оставлено как структура для миграции.
type frame struct {
	fn   *Function
	ip   int
	base int // индекс начала локалей кадра в общем стеке значений
}

// VM — стековый интерпретатор байткода.
//
// globals: функции модуля + прелюдия (§11.5). Стек значений общий;
// каждый кадр в срезе работает со своим срезом через рекурсию.
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

// DefineGlobal регистрирует глобальное имя (функцию модуля/константу).
func (vm *VM) DefineGlobal(name string, v runtime.Value) { vm.globals[name] = v }

// Global возвращает глобальное значение по имени (нулевое, если нет).
func (vm *VM) Global(name string) runtime.Value { return vm.globals[name] }

// FuncValue оборачивает скомпилированную функцию в значение-функцию.
//
// Тело кладём в Body как *Chunk через any, чтобы пакет runtime не
// импортировал vm и не возникало циклической зависимости.
func FuncValue(fn *Function) runtime.Value {
	return runtime.Func(&runtime.FuncValue{
		Name:     fn.Name,
		Arity:    fn.Arity,
		IsNative: false,
		Body:     fn.Chunk,
	})
}

// Call реализует runtime.Caller: прелюдия может вызывать функции.
//
// Принимает значение-функцию и аргументы, возвращает результат.
// Нативные функции исполняются сразу; байткод-функции — через run.
func (vm *VM) Call(fn runtime.Value, args []runtime.Value) (runtime.Value, error) {
	if fn.Kind != runtime.KindFunction || fn.Func == nil {
		return runtime.Unit,
			fmt.Errorf("(:type_error, (:call, %s))", fn.Inspect())
	}
	f := fn.Func
	if f.IsNative {
		if f.Native == nil {
			return runtime.Unit, fmt.Errorf("internal: nil native function %q", f.Name)
		}
		return f.Native(vm, args)
	}
	chunk, ok := f.Body.(*Chunk)
	if !ok || chunk == nil {
		return runtime.Unit, fmt.Errorf("internal: compiled function without chunk")
	}
	return vm.run(Function{Name: f.Name, Arity: f.Arity, Chunk: chunk}, args)
}

// run исполняет один фрейм с аргументами и возвращает значение.
//
// Стек кадра локальный; локалы — в фиксированном слайсе (см. maxLocals).
// Опкоды с операндом читают 16-битное значение прямо из Code.
func (vm *VM) run(fn Function, args []runtime.Value) (runtime.Value, error) {
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

	ip := 0
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
				return runtime.Unit, err
			}
			push(r)
			ip++

		case OpIntDiv:
			b, _ := pop()
			a, _ := pop()
			r, err := intDiv(a, b)
			if err != nil {
				return runtime.Unit, err
			}
			push(r)
			ip++

		case OpRem:
			b, _ := pop()
			a, _ := pop()
			r, err := rem(a, b)
			if err != nil {
				return runtime.Unit, err
			}
			push(r)
			ip++

		case OpPow:
			b, _ := pop()
			a, _ := pop()
			r, err := pow(a, b)
			if err != nil {
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

		case OpGetLocal:
			idx := operand(ip)
			if idx >= len(locals) {
				return runtime.Unit, fmt.Errorf("internal: local index %d out of range", idx)
			}
			push(locals[idx])
			ip += 3

		case OpSetLocal:
			idx := operand(ip)
			if idx >= len(locals) {
				return runtime.Unit, fmt.Errorf("internal: local index %d out of range", idx)
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
			args := make([]runtime.Value, argc)
			for i := argc - 1; i >= 0; i-- {
				v, err := pop()
				if err != nil {
					return runtime.Unit, err
				}
				args[i] = v
			}
			callee, err := pop()
			if err != nil {
				return runtime.Unit, err
			}
			r, err := vm.Call(callee, args)
			if err != nil {
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
			n := operand(ip) // число пар
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
			return runtime.Unit, &ErrRaise{Val: v}

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
//
// Правила (Must для VM):
//   - `/` всегда возвращает Float;
//   - деление на ноль → raise((:division_by_zero, ()));
//   - `div` — целочисленное деление к нулю;
//   - `rem` — знак результата следует за делимым;
//   - переполнение Int невозможно (произвольная точность через big.Int).

func add(a, b runtime.Value) (runtime.Value, error) {
	// конкатенация строк и списков
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

// div всегда возвращает Float (§7.3).
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

// intDiv — целочисленное деление к нулю (big.Int.Quo).
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

// rem — остаток; знак следует за делимым (big.Int.Rem).
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

// pow — возведение в степень. Целая неотрицательная степень над Int —
// точно через big.Int.Exp; иначе — через math.Pow (Float).
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

// ---- вспомогательные ----

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
