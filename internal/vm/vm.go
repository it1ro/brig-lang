// Package vm — регистровая байткод-машина Brig.
//
// Подэтап 4.8: акторы с явным scheduler loop (§12, §15.2).
// Sprint 6.2: RunMainWithArgs для persistent REPL.
// Sprint 5.4: Decimal (§3.1) — арифметика + негативные кейсы Decimal×Float.
package vm

import (
	"fmt"
	"math"
	"math/big"
	"strings"

	"github.com/it1ro/brig-lang/internal/runtime"
)

// maxLocals — потолок числа локальных слотов на кадр функции.
const maxLocals = 256

// MaxLocals — экспортируемая версия для compiler.declareLocal.
const MaxLocals = maxLocals

// ErrRaise — необработанное исключение.
//
// Trace — отдельный debug-канал (§17 Q1): его заполняет только планировщик
// при непойманном raise; в Val, trap и :down он не попадает.
type ErrRaise struct {
	Val   runtime.Value
	Trace []TraceFrame
}

// TraceFrame — кадр stack trace: функция и позиция инструкции в ней.
type TraceFrame struct {
	Func string
	Pos  SrcPos
}

func (e *ErrRaise) Error() string { return "raise: " + e.Val.Inspect() }

// VM — виртуальная машина.
type VM struct {
	globals      map[string]runtime.Value
	scheduler    *Scheduler
	tests        []testCase
	currentGroup string
	args         []string
	// resumable — нативы, исполняемые кадром актора (map, filter, …; T-58).
	resumable map[*runtime.FuncValue]resumableFunc
}

// New создаёт ВМ с установленной прелюдией.
func New() *VM {
	vm := &VM{
		globals:   make(map[string]runtime.Value),
		resumable: make(map[*runtime.FuncValue]resumableFunc),
	}
	vm.scheduler = NewScheduler(vm)
	InstallPrelude(vm)
	InstallJSONPrelude(vm)
	InstallTestPrelude(vm)
	aliasPrelude(vm)
	return vm
}

// aliasPrelude кладёт функции прелюдии под именами Prelude.<name>: они
// остаются доступны, когда пользовательская fn затеняет глобал (§11.5).
func aliasPrelude(vm *VM) {
	for name, v := range vm.globals {
		if v.Kind == runtime.KindFunction && !strings.Contains(name, ".") {
			vm.globals["Prelude."+name] = v
		}
	}
}

// SetArgs задаёт аргументы программы для Sys.args() (§16).
func (vm *VM) SetArgs(args []string) { vm.args = args }

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

// ---- арифметика (§7.3, §7.4) ----

// numToRat возвращает big.Rat для Int/Decimal. Float и прочее — false.
func numToRat(v runtime.Value) (*big.Rat, bool) {
	switch v.Kind {
	case runtime.KindDecimal:
		return v.Dec, true
	case runtime.KindInt:
		return new(big.Rat).SetInt(v.AsBig()), true
	}
	return nil, false
}

// typeErr — ловимый raise (:type_error, (op, val)) (§10.4).
//
// `op` передаётся без ведущего двоеточия (`"add"`, а не `":add"`):
// runtime.Atom сам добавляет ":" при печати, и `Atom(":add")`
// давал бы `::add` в Inspect().
func typeErr(op string, val runtime.Value) error {
	return &ErrRaise{Val: runtime.Tuple(
		runtime.Atom("type_error"),
		runtime.Tuple(runtime.Atom(op), val))}
}

// decArithErr — :type_error как catchable raise (для trap).
func decArithErr(a, b runtime.Value, op string) error {
	return typeErr(op, runtime.Tuple(a, b))
}

func add(a, b runtime.Value) (runtime.Value, error) {
	if a.Kind == runtime.KindDecimal || b.Kind == runtime.KindDecimal {
		ar, ok1 := numToRat(a)
		br, ok2 := numToRat(b)
		if !ok1 || !ok2 {
			return runtime.Unit, decArithErr(a, b, "add")
		}
		return runtime.Decimal(new(big.Rat).Add(ar, br)), nil
	}
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
	if a.Kind == runtime.KindDecimal || b.Kind == runtime.KindDecimal {
		ar, ok1 := numToRat(a)
		br, ok2 := numToRat(b)
		if !ok1 || !ok2 {
			return runtime.Unit, decArithErr(a, b, "sub")
		}
		return runtime.Decimal(new(big.Rat).Sub(ar, br)), nil
	}
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
	if a.Kind == runtime.KindDecimal || b.Kind == runtime.KindDecimal {
		ar, ok1 := numToRat(a)
		br, ok2 := numToRat(b)
		if !ok1 || !ok2 {
			return runtime.Unit, decArithErr(a, b, "mul")
		}
		return runtime.Decimal(new(big.Rat).Mul(ar, br)), nil
	}
	if !bothNum(a, b) {
		return runtime.Unit, arithErr(a, b, ":mul")
	}
	if a.Kind == runtime.KindFloat || b.Kind == runtime.KindFloat {
		return runtime.Float(numToFloat(a) * numToFloat(b)), nil
	}
	if a.IsSmall && b.IsSmall {
		r := a.SmallInt * b.SmallInt
		if a.SmallInt == 0 || (r/a.SmallInt == b.SmallInt && (a.SmallInt != -1 || b.SmallInt != math.MinInt64) && (b.SmallInt != -1 || a.SmallInt != math.MinInt64)) {
			return runtime.Int(r), nil
		}
	}
	return runtime.IntBig(new(big.Int).Mul(a.AsBig(), b.AsBig())), nil
}

// div — оператор `/`.
//
// §7.3 буквально: «`/` всегда возвращает `Float`». Осознанное
// расширение для Decimal: если хотя бы один операнд — Decimal и ни
// один — Float, возвращаем Decimal, чтобы сохранить точность
// (иначе `dec"1" / dec"2"` теряет смысл «точной десятичной
// арифметики»). Decimal×Float — :type_error (§7.4).
func div(a, b runtime.Value) (runtime.Value, error) {
	if a.Kind == runtime.KindDecimal || b.Kind == runtime.KindDecimal {
		if a.Kind == runtime.KindFloat || b.Kind == runtime.KindFloat {
			return runtime.Unit, decArithErr(a, b, "div")
		}
		ar, ok1 := numToRat(a)
		br, ok2 := numToRat(b)
		if !ok1 || !ok2 {
			return runtime.Unit, decArithErr(a, b, "div")
		}
		if br.Sign() == 0 {
			return runtime.Unit, &ErrRaise{Val: runtime.Tuple(
				runtime.Atom("division_by_zero"), runtime.Unit)}
		}
		return runtime.Decimal(new(big.Rat).Quo(ar, br)), nil
	}
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
	if a.Kind == runtime.KindDecimal || b.Kind == runtime.KindDecimal {
		return runtime.Unit, decArithErr(a, b, "div")
	}
	if a.Kind != runtime.KindInt || b.Kind != runtime.KindInt {
		return runtime.Unit, arithErr(a, b, ":div")
	}
	if b.IsSmall {
		if b.SmallInt == 0 {
			return runtime.Unit, &ErrRaise{Val: runtime.Tuple(
				runtime.Atom("division_by_zero"), runtime.Unit)}
		}
		if a.IsSmall && (a.SmallInt != math.MinInt64 || b.SmallInt != -1) {
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
	case runtime.KindDecimal:
		return runtime.Decimal(new(big.Rat).Neg(a.Dec)), nil
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
	if a.Kind == runtime.KindDecimal || b.Kind == runtime.KindDecimal {
		return runtime.Unit, decArithErr(a, b, "rem")
	}
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

// pow — `**`. Для Decimal базы и целочисленного (Int или Decimal-целого)
// показателя — точное возведение: (p/q)^n = p^n / q^n. Для остальных
// сочетаний — Float. Decimal×Float → :type_error.
func pow(a, b runtime.Value) (runtime.Value, error) {
	if a.Kind == runtime.KindDecimal || b.Kind == runtime.KindDecimal {
		if a.Kind == runtime.KindFloat || b.Kind == runtime.KindFloat {
			return runtime.Unit, decArithErr(a, b, "pow")
		}
		if a.Kind == runtime.KindDecimal && b.Kind == runtime.KindInt {
			exp := b.AsBig()
			if !exp.IsInt64() {
				return runtime.Unit, decArithErr(a, b, "pow")
			}
			n := exp.Int64()
			if n < 0 {
				// (p/q)^-n = (q/p)^n
				inv := new(big.Rat).Inv(a.Dec)
				if inv == nil {
					return runtime.Unit, &ErrRaise{Val: runtime.Tuple(
						runtime.Atom("division_by_zero"), runtime.Unit)}
				}
				return runtime.Decimal(ratIntPow(inv, -n)), nil
			}
			return runtime.Decimal(ratIntPow(a.Dec, n)), nil
		}
		return runtime.Unit, decArithErr(a, b, "pow")
	}
	if !bothNum(a, b) {
		return runtime.Unit, arithErr(a, b, ":pow")
	}
	if a.Kind == runtime.KindInt && b.Kind == runtime.KindInt && b.AsBig().Sign() >= 0 {
		return runtime.IntBig(new(big.Int).Exp(a.AsBig(), b.AsBig(), nil)), nil
	}
	return runtime.Float(math.Pow(numToFloat(a), numToFloat(b))), nil
}

// ratIntPow — r^n для n >= 0.
func ratIntPow(r *big.Rat, n int64) *big.Rat {
	out := new(big.Rat)
	num := new(big.Int).Exp(r.Num(), big.NewInt(n), nil)
	den := new(big.Int).Exp(r.Denom(), big.NewInt(n), nil)
	return out.SetFrac(num, den)
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

// checkMixedEq — жёсткая проверка Decimal×Float для оператора == / !=
// (§7.4: ошибка). Возвращает catchable *ErrRaise.
//
// В остальных контекстах (Equal из pattern-match, коллекции) та же
// пара даёт false — так безопаснее для нестроковых использований.
func checkMixedEq(a, b runtime.Value) error {
	if (a.Kind == runtime.KindDecimal && b.Kind == runtime.KindFloat) ||
		(a.Kind == runtime.KindFloat && b.Kind == runtime.KindDecimal) {
		return typeErr("eq", runtime.Tuple(a, b))
	}
	return nil
}

// checkMixedCmp — жёсткая проверка Decimal×Float для операторов
// сравнения < > <= >= (§7.4: ошибка). Возвращает catchable *ErrRaise.
//
// Нужна до вызова runtime.Compare, потому что последний возвращает
// обычную error (без обёртки ErrRaise), и handleRaise её не ловит.
func checkMixedCmp(a, b runtime.Value) error {
	if (a.Kind == runtime.KindDecimal && b.Kind == runtime.KindFloat) ||
		(a.Kind == runtime.KindFloat && b.Kind == runtime.KindDecimal) {
		return typeErr("compare", runtime.Tuple(a, b))
	}
	return nil
}
