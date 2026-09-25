// Package vm — стековая байткод-машина Brig.
//
// Подэтап 4.8: акторы с явным scheduler loop (§12, §15.2).
// Sprint 6.2: RunMainWithArgs для persistent REPL.
package vm

import (
	"fmt"
	"math"
	"math/big"

	"github.com/it1ro/brig-lang/internal/runtime"
)

// maxLocals — потолок числа локальных слотов на кадр функции.
// Sprint 6.2: увеличен с 64 до 256 + проверка в compiler.declareLocal.
const maxLocals = 256

// MaxLocals — экспортируемая версия для compiler.declareLocal.
const MaxLocals = maxLocals

// ErrRaise — необработанное исключение.
type ErrRaise struct{ Val runtime.Value }

func (e *ErrRaise) Error() string { return "raise: " + e.Val.Inspect() }

// VM — виртуальная машина.
type VM struct {
	globals   map[string]runtime.Value
	scheduler *Scheduler
}

// New создаёт ВМ с установленной прелюдией.
func New() *VM {
	vm := &VM{globals: make(map[string]runtime.Value)}
	vm.scheduler = NewScheduler(vm)
	InstallPrelude(vm)
	return vm
}

// Scheduler возвращает планировщик (нужен прелюдии).
func (vm *VM) Scheduler() *Scheduler { return vm.scheduler }

// DefineGlobal регистрирует глобальное имя.
func (vm *VM) DefineGlobal(name string, v runtime.Value) { vm.globals[name] = v }

// Global возвращает глобальное значение.
func (vm *VM) Global(name string) runtime.Value { return vm.globals[name] }

// FuncValue оборачивает скомпилированную функцию.
func FuncValue(fn *Function) runtime.Value {
	return runtime.Func(&runtime.FuncValue{
		Name:     fn.Name,
		Arity:    fn.Arity,
		IsNative: false,
		Body:     fn.Chunk,
	})
}

// Call — синхронный вызов (для прелюдии и legacy-кода).
func (vm *VM) Call(fn runtime.Value, args []runtime.Value) (runtime.Value, error) {
	switch fn.Kind {
	case runtime.KindFunction:
		if fn.Func == nil {
			return runtime.Unit, fmt.Errorf("(:type_error, (:call, nil))")
		}
		if fn.Func.IsNative {
			if fn.Func.Native == nil {
				return runtime.Unit, fmt.Errorf("internal: nil native %q", fn.Func.Name)
			}
			return fn.Func.Native(vm, args)
		}
		return vm.scheduler.callSync(fn, args)
	case runtime.KindClosure:
		return vm.scheduler.callSync(fn, args)
	default:
		return runtime.Unit, fmt.Errorf("(:type_error, (:call, %s))", fn.Inspect())
	}
}

// RunMain запускает main как актор и крутит планировщик.
func (vm *VM) RunMain(mainFn runtime.Value) (runtime.Value, error) {
	return vm.scheduler.RunMain(mainFn)
}

// RunMainWithArgs — вариант RunMain с аргументами (Sprint 6.2, REPL).
func (vm *VM) RunMainWithArgs(mainFn runtime.Value, args []runtime.Value) (runtime.Value, error) {
	return vm.scheduler.RunMainWithArgs(mainFn, args)
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
	if a.IsSmall && b.IsSmall {
		sum := a.SmallInt + b.SmallInt
		if (b.SmallInt > 0 && sum > a.SmallInt) ||
			(b.SmallInt < 0 && sum < a.SmallInt) ||
			b.SmallInt == 0 {
			return runtime.Int(sum), nil
		}
	}
	return runtime.IntBig(new(big.Int).Add(a.AsBig(), b.AsBig())), nil
}

func sub(a, b runtime.Value) (runtime.Value, error) {
	if !bothNum(a, b) {
		return runtime.Unit, arithErr(a, b, ":sub")
	}
	if a.Kind == runtime.KindFloat || b.Kind == runtime.KindFloat {
		return runtime.Float(numToFloat(a) - numToFloat(b)), nil
	}
	if a.IsSmall && b.IsSmall {
		diff := a.SmallInt - b.SmallInt
		if (b.SmallInt > 0 && diff < a.SmallInt) ||
			(b.SmallInt < 0 && diff > a.SmallInt) ||
			b.SmallInt == 0 {
			return runtime.Int(diff), nil
		}
	}
	return runtime.IntBig(new(big.Int).Sub(a.AsBig(), b.AsBig())), nil
}

func mul(a, b runtime.Value) (runtime.Value, error) {
	if !bothNum(a, b) {
		return runtime.Unit, arithErr(a, b, ":mul")
	}
	if a.Kind == runtime.KindFloat || b.Kind == runtime.KindFloat {
		return runtime.Float(numToFloat(a) * numToFloat(b)), nil
	}
	if a.IsSmall && b.IsSmall {
		r := a.SmallInt * b.SmallInt
		if a.SmallInt == 0 || (r/a.SmallInt == b.SmallInt && !(a.SmallInt == -1 && b.SmallInt == math.MinInt64) && !(b.SmallInt == -1 && a.SmallInt == math.MinInt64)) { //lint:ignore QF1001
			return runtime.Int(r), nil
		}
	}
	return runtime.IntBig(new(big.Int).Mul(a.AsBig(), b.AsBig())), nil
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
	if b.IsSmall {
		if b.SmallInt == 0 {
			return runtime.Unit, &ErrRaise{Val: runtime.Tuple(
				runtime.Atom("division_by_zero"), runtime.Unit)}
		}
		if a.IsSmall && !(a.SmallInt == math.MinInt64 && b.SmallInt == -1) { //lint:ignore QF1001
			return runtime.Int(a.SmallInt / b.SmallInt), nil
		}
	} else if b.AsBig().Sign() == 0 {
		return runtime.Unit, &ErrRaise{Val: runtime.Tuple(
			runtime.Atom("division_by_zero"), runtime.Unit)}
	}
	return runtime.IntBig(new(big.Int).Quo(a.AsBig(), b.AsBig())), nil
}

func neg(a runtime.Value) (runtime.Value, error) {
	switch a.Kind {
	case runtime.KindInt:
		if a.IsSmall && a.SmallInt != math.MinInt64 {
			return runtime.Int(-a.SmallInt), nil
		}
		return runtime.IntBig(new(big.Int).Neg(a.AsBig())), nil
	case runtime.KindFloat:
		return runtime.Float(-a.Float), nil
	}
	return runtime.Unit, fmt.Errorf("(:type_error, (:neg, %s))", a.Inspect())
}

func rem(a, b runtime.Value) (runtime.Value, error) {
	if a.Kind != runtime.KindInt || b.Kind != runtime.KindInt {
		return runtime.Unit, arithErr(a, b, ":rem")
	}
	if b.IsSmall {
		if b.SmallInt == 0 {
			return runtime.Unit, &ErrRaise{Val: runtime.Tuple(
				runtime.Atom("division_by_zero"), runtime.Unit)}
		}
		if a.IsSmall {
			return runtime.Int(a.SmallInt % b.SmallInt), nil
		}
	} else if b.AsBig().Sign() == 0 {
		return runtime.Unit, &ErrRaise{Val: runtime.Tuple(
			runtime.Atom("division_by_zero"), runtime.Unit)}
	}
	return runtime.IntBig(new(big.Int).Rem(a.AsBig(), b.AsBig())), nil
}

func pow(a, b runtime.Value) (runtime.Value, error) {
	if !bothNum(a, b) {
		return runtime.Unit, arithErr(a, b, ":pow")
	}
	if a.Kind == runtime.KindInt && b.Kind == runtime.KindInt && b.AsBig().Sign() >= 0 {
		return runtime.IntBig(new(big.Int).Exp(a.AsBig(), b.AsBig(), nil)), nil
	}
	return runtime.Float(math.Pow(numToFloat(a), numToFloat(b))), nil
}

func bothNum(a, b runtime.Value) bool {
	return (a.Kind == runtime.KindInt || a.Kind == runtime.KindFloat) &&
		(b.Kind == runtime.KindInt || b.Kind == runtime.KindFloat)
}

func numToFloat(v runtime.Value) float64 {
	if v.Kind == runtime.KindFloat {
		return v.Float
	}
	if v.IsSmall {
		return float64(v.SmallInt)
	}
	f, _ := new(big.Float).SetInt(v.AsBig()).Float64()
	return f
}

func arithErr(a, b runtime.Value, op string) error {
	return fmt.Errorf("(:type_error, (%s, (%s, %s)))", op, a.Inspect(), b.Inspect())
}
