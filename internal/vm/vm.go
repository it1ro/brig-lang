// Package vm — регистровая байткод-машина Brig.
//
// Подэтап 4.8: акторы с явным scheduler loop (§12, §15.2).
// Sprint 6.2: RunMainWithArgs для persistent REPL.
// Sprint 5.4: Decimal (§3.1) — арифметика + негативные кейсы Decimal×Float.
package vm

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
	"sync/atomic"

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

// TraceFrame — кадр stack trace: функция, файл её модуля (Chunk.File) и
// позиция инструкции в ней.
type TraceFrame struct {
	Func string
	File string
	Pos  SrcPos
}

func (e *ErrRaise) Error() string { return "raise: " + e.Val.Inspect() }

// ErrUndefined — GETGLOBAL по имени, которого нет (`undefined: g`).
// At — функция и место инструкции: по ним REPL печатает строку ввода и
// `^` (§E.1).
type ErrUndefined struct {
	Name string
	At   TraceFrame
}

func (e *ErrUndefined) Error() string { return "undefined: " + e.Name }

// globalCell — ячейка глобального имени. Адрес ячейки стабилен на всё время
// жизни VM, поэтому чанк держит её в кэше и не ищет имя в map на каждом
// GETGLOBAL (T-276): переопределение в REPL (§11.4) пишет в ту же ячейку,
// снятие имени ставит set = false.
type globalCell struct {
	val runtime.Value
	set bool
}

// cellBlock — ячейки выделяются блоками по cellBlock штук: прелюдия
// регистрирует ~100 имён, и отдельная аллокация на каждое выросла бы в
// allocs/op прогона (BenchmarkCall). Блок не переезжает — адрес ячейки
// внутри него стабилен. Размер — компромисс: больший блок режет аллокации,
// но недоиспользованный хвост блока растёт в B/op (ячейка — 280 B).
const cellBlock = 4

// VM — виртуальная машина.
type VM struct {
	globals map[string]*globalCell
	// cellFree — неизданный остаток последнего блока ячеек.
	cellFree     []globalCell
	scheduler    *Scheduler
	tests        []testCase
	currentGroup string
	args         []string
	// resumable — нативы, исполняемые кадром актора (map, filter, …; T-58).
	resumable map[*runtime.FuncValue]resumableFunc
	// teleEmit — Telemetry.emit: enterCall узнаёт его по указателю (§12.14).
	teleEmit *runtime.FuncValue
	// signals — реализация портов Signal (§12.12); nil — без ОС.
	signals SignalHub
	// files — реализация портов File (§12.12); nil — без файловой системы.
	files FileHub
	// http — реализация портов HttpServer (§12.12); nil — без сети.
	http HTTPHub
}

// New создаёт ВМ с установленной прелюдией.
func New() *VM {
	vm := &VM{
		globals:   make(map[string]*globalCell),
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
	for name, c := range vm.globals {
		if c.set && c.val.Kind == runtime.KindFunction && !strings.Contains(name, ".") {
			vm.DefineGlobal("Prelude."+name, c.val)
		}
	}
}

// SetArgs задаёт аргументы программы для Sys.args() (§16).
func (vm *VM) SetArgs(args []string) { vm.args = args }

// Scheduler возвращает планировщик (нужен прелюдии).
func (vm *VM) Scheduler() *Scheduler { return vm.scheduler }

// DefineGlobal регистрирует глобальное имя.
func (vm *VM) DefineGlobal(name string, v runtime.Value) {
	c := vm.cell(name)
	c.val, c.set = v, true
}

// Global возвращает глобальное значение; имя не зарегистрировано — Unit.
func (vm *VM) Global(name string) runtime.Value {
	if c := vm.globals[name]; c != nil && c.set {
		return c.val
	}
	return runtime.Value{}
}

// cell отдаёт ячейку имени, создавая её при первом обращении. Ячейка
// переживает снятие имени (undefineGlobal): кэш чанка держит её адрес, и
// повторное определение того же имени должно попасть в ту же ячейку.
func (vm *VM) cell(name string) *globalCell {
	if c := vm.globals[name]; c != nil {
		return c
	}
	if len(vm.cellFree) == 0 {
		vm.cellFree = make([]globalCell, cellBlock)
	}
	c := &vm.cellFree[0]
	vm.cellFree = vm.cellFree[1:]
	vm.globals[name] = c
	return c
}

// undefineGlobal снимает глобальное имя (REPL, §11.4): ячейка остаётся в
// таблице пустой, и кэши чанков видят снятие без инвалидации.
func (vm *VM) undefineGlobal(name string) {
	if c := vm.globals[name]; c != nil {
		c.val, c.set = runtime.Value{}, false
	}
}

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
			return runtime.Unit, errors.New("internal: call of nil function")
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
		return runtime.Unit, typeErr("call", fn)
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

// StartSession поднимает долгоживущий актор сессии и фоновый планировщик (§11.4).
func (vm *VM) StartSession() error { return vm.scheduler.StartSession() }

// CloseSession останавливает фоновый планировщик сессии.
func (vm *VM) CloseSession() { vm.scheduler.CloseSession() }

// Interrupt снимает текущий ввод сессии (§11.4). Не raise.
func (vm *VM) Interrupt() { vm.scheduler.Interrupt() }

// SessionEval исполняет fn(args) актором сессии. defs попадают в глобалы
// на горутине планировщика до запуска ввода.
func (vm *VM) SessionEval(fn runtime.Value, args []runtime.Value, defs map[string]runtime.Value) (runtime.Value, error) {
	return vm.scheduler.Submit(fn, args, defs)
}

// SessionRedefine снимает глобалы undef и регистрирует defs на горутине
// планировщика, атомарно для всех акторов; ввод не исполняется.
func (vm *VM) SessionRedefine(defs map[string]runtime.Value, undef []string) error {
	return vm.scheduler.Redefine(defs, undef)
}

// SessionRedefineHere — SessionRedefine из натива, который исполняет цикл
// сессии (хелперы консоли): глобалы меняются сразу, без сдачи работы.
func (vm *VM) SessionRedefineHere(defs map[string]runtime.Value, undef []string) {
	vm.scheduler.RedefineHere(defs, undef)
}

// CountSessionReductions включает счёт редукций актора сессии в c.
// Вызывать до StartSession.
func (vm *VM) CountSessionReductions(c *atomic.Uint64) {
	vm.scheduler.CountSessionReductions(c)
}

// ReductionsAfterInterrupt — сколько редукций актор сессии сделал после
// последнего Interrupt, пока ввод ещё не сняли. Не больше одного слайса.
func (vm *VM) ReductionsAfterInterrupt() uint64 {
	return vm.scheduler.afterInterrupt.Load()
}

// ---- быстрый путь арифметики и сравнений (T-276) ----
//
// runtime.Value — крупная структура (272 B, раскладку меняет T-104),
// поэтому передача операндов в add/sub/mul и runtime.Equal/Compare по
// значению копирует её трижды на операцию: в профиле BenchmarkTailCall это
// самая дорогая точка (~26 %). Быстрый путь работает прямо по указателям на
// регистры и покрывает два самых частых сочетания — Int в smallint-форме и
// Float×Float. Всё остальное (Decimal, big.Int, смешанные Int×Float,
// не-числа, переполнение smallint) отдаётся прежнему медленному пути, он же
// остаётся единственным источником :type_error и значений результата —
// семантика §7.3/§7.4 и K-3 не меняется.

// fastAdd, fastSub, fastMul пишут результат в dst (может совпадать с a или
// b: операнды читаются до записи) и возвращают false, если сочетание видов
// или переполнение им не по силам. Условия переполнения smallint — те же,
// что в add/sub/mul.
func fastAdd(dst, a, b *runtime.Value) bool {
	if a.Kind == runtime.KindInt && b.Kind == runtime.KindInt {
		if !a.IsSmall || !b.IsSmall {
			return false
		}
		sum := a.SmallInt + b.SmallInt
		if (b.SmallInt > 0 && sum > a.SmallInt) ||
			(b.SmallInt < 0 && sum < a.SmallInt) ||
			b.SmallInt == 0 {
			*dst = runtime.Int(sum)
			return true
		}
		return false
	}
	if a.Kind == runtime.KindFloat && b.Kind == runtime.KindFloat {
		*dst = runtime.Float(a.Float + b.Float)
		return true
	}
	return false
}

func fastSub(dst, a, b *runtime.Value) bool {
	if a.Kind == runtime.KindInt && b.Kind == runtime.KindInt {
		if !a.IsSmall || !b.IsSmall {
			return false
		}
		diff := a.SmallInt - b.SmallInt
		if (b.SmallInt > 0 && diff < a.SmallInt) ||
			(b.SmallInt < 0 && diff > a.SmallInt) ||
			b.SmallInt == 0 {
			*dst = runtime.Int(diff)
			return true
		}
		return false
	}
	if a.Kind == runtime.KindFloat && b.Kind == runtime.KindFloat {
		*dst = runtime.Float(a.Float - b.Float)
		return true
	}
	return false
}

func fastMul(dst, a, b *runtime.Value) bool {
	if a.Kind == runtime.KindInt && b.Kind == runtime.KindInt {
		if !a.IsSmall || !b.IsSmall {
			return false
		}
		x, y := a.SmallInt, b.SmallInt
		r := x * y
		if x == 0 || (r/x == y && (x != -1 || y != math.MinInt64) && (y != -1 || x != math.MinInt64)) {
			*dst = runtime.Int(r)
			return true
		}
		return false
	}
	if a.Kind == runtime.KindFloat && b.Kind == runtime.KindFloat {
		*dst = runtime.Float(a.Float * b.Float)
		return true
	}
	return false
}

// fastEq — равенство для smallint×smallint и Float×Float. Правила те же, что
// у runtime.Equal: Int равен Int численно, NaN не равен ничему, -0.0 == 0.0.
func fastEq(a, b *runtime.Value) (eq, ok bool) {
	switch {
	case a.Kind == runtime.KindInt && b.Kind == runtime.KindInt:
		if !a.IsSmall || !b.IsSmall {
			return false, false
		}
		return a.SmallInt == b.SmallInt, true
	case a.Kind == runtime.KindFloat && b.Kind == runtime.KindFloat:
		return a.Float == b.Float, true
	}
	return false, false
}

// fastCmp — результат LT/GT/LE/GE для smallint×smallint и Float×Float.
// Сравнение float в Go даёт false для любого операнда NaN — это и есть
// правило §7.4 (runtime.IsNaNOperand на медленном пути).
func fastCmp(op OpCode, a, b *runtime.Value) (res, ok bool) {
	switch {
	case a.Kind == runtime.KindInt && b.Kind == runtime.KindInt:
		if !a.IsSmall || !b.IsSmall {
			return false, false
		}
		x, y := a.SmallInt, b.SmallInt
		switch op {
		case LT:
			return x < y, true
		case GT:
			return x > y, true
		case LE:
			return x <= y, true
		case GE:
			return x >= y, true
		}
	case a.Kind == runtime.KindFloat && b.Kind == runtime.KindFloat:
		x, y := a.Float, b.Float
		switch op {
		case LT:
			return x < y, true
		case GT:
			return x > y, true
		case LE:
			return x <= y, true
		case GE:
			return x >= y, true
		}
	}
	return false, false
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

// modTypeErr — :type_error функции встроенного модуля со структурным
// тегом `((:mod, :f), val)`, mod — имя модуля в snake_case (атом
// пишется только со строчной): без коллизий между модулями и пишется в
// паттерне (T-176; перевод прежних `:Mod.f` — T-236).
func modTypeErr(mod, fn string, val runtime.Value) error {
	return &ErrRaise{Val: runtime.Tuple(
		runtime.Atom("type_error"),
		runtime.Tuple(runtime.Tuple(runtime.Atom(mod), runtime.Atom(fn)), val))}
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
	if !bothNum(a, b) {
		return runtime.Unit, arithErr(a, b, "add")
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
		return runtime.Unit, arithErr(a, b, "sub")
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
		return runtime.Unit, arithErr(a, b, "mul")
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
		return runtime.Unit, arithErr(a, b, "div")
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
		return runtime.Unit, arithErr(a, b, "div")
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
	return runtime.Unit, typeErr("neg", a)
}

func rem(a, b runtime.Value) (runtime.Value, error) {
	if a.Kind == runtime.KindDecimal || b.Kind == runtime.KindDecimal {
		return runtime.Unit, decArithErr(a, b, "rem")
	}
	if a.Kind != runtime.KindInt || b.Kind != runtime.KindInt {
		return runtime.Unit, arithErr(a, b, "rem")
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
		return runtime.Unit, arithErr(a, b, "pow")
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

// arithErr — ловимый raise (:type_error, (op, (a, b))) для операндов
// не того вида (DD #42, вариант A).
func arithErr(a, b runtime.Value, op string) error {
	return typeErr(op, runtime.Tuple(a, b))
}

// checkMixedEq — проверка Decimal×Float для оператора == / != (§7.4,
// решение #43 п.3): ловимый (:type_error, (:eq, (a, b))). Проверяется
// только пара верхнего уровня; вложенные значения сравнивает Equal (false).
//
// Ключи Map/Set (INDEX, set, Map.*) и паттерны ошибку не бросают: разные
// виды — разные значения, см. runtime.KeyEqual и runtime.MatchEqual.
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
// Форма raise — (:type_error, (:compare, (a, b))), как у ошибки
// runtime.Compare (в т.ч. для вложенной пары) в LT/GT/LE/GE.
func checkMixedCmp(a, b runtime.Value) error {
	if (a.Kind == runtime.KindDecimal && b.Kind == runtime.KindFloat) ||
		(a.Kind == runtime.KindFloat && b.Kind == runtime.KindDecimal) {
		return typeErr("compare", runtime.Tuple(a, b))
	}
	return nil
}
