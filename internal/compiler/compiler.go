// Package compiler — компиляция AST в регистровый байткод ВМ.
//
// Sprint 7, S7.2 + S7.6. Дизайн: docs/02-register-based-virtual-machine.md §7.
//
// Аллокатор — bump-указатель со стековой дисциплиной (nextReg + releaseToMark).
// Соглашение о вызовах (§3): callee в R[A], аргументы в R[A+1..A+B], результат
// в R[C]. Хвостовость — поле dest.tail; TAILCALL эмитится только вне trap,
// хвост тела trap с ensure — TAILCALLENS (dest.ens, doc 02 §5.1).
package compiler

import (
	"errors"
	"fmt"
	"math"

	"github.com/it1ro/brig-lang/internal/ast"
	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/vm"
)

// Verify — если true, компилятор прогоняет vm.Verify по каждому чанку
// после сборки модуля. Включается в тестах (verify_on_test.go) или
// через BRIG_VERIFY=1 в cmd/brig/main.go.
var Verify = false

// ProgramImage — результат компиляции модуля.
type ProgramImage struct {
	Functions map[string]*vm.Function
	Main      *vm.Function
}

// Compiler переводит AST в ProgramImage.
type Compiler struct {
	image *ProgramImage
	// lifted — лифтнутые локальные fn по mangled-имени (T-51, T-39).
	lifted map[string]*liftedFn
	// mods — модули программы по имени (T-137, §11.1); entry — входной,
	// cur — модуль, функции которого компилируются сейчас.
	mods  map[string]*module
	entry *module
	cur   *module
	// gen — поколение кода в сессии REPL: суффикс префикса вложенных fn
	// (`M.f@2$g`). recompile() не подменяет поднятые локальные fn у старых
	// кадров (T-208, #246); у первой компиляции пуст.
	gen string
	// where — место определения новых чанков для печати лямбд
	// (vm.Chunk.Where); REPL ставит `<repl>:N` по номеру ввода.
	where string
	// replHelpers — инструкция REPL: аргумент `h`/`Repl.h`, который есть
	// имя модуля, компилируется как атом для хелпера.
	replHelpers bool
	// preludeQualified — входной модуль исполнения неизвестен при
	// компиляции (stdlib, T-146: её образ ставится в ВМ любой программы).
	// Голое имя функции не своего модуля в не-входном модуле — всегда
	// `Prelude.name`: fn входного модуля программы лежат под голыми
	// именами и затенили бы прелюдию.
	preludeQualified bool
}

// Module — модуль программы для CompileProgram (§11.1): имя, путь файла
// (для stack trace и ошибок) и AST.
type Module struct {
	Name, Path string
	Prog       *ast.Program
}

// module — декларации модуля программы, видимые при компиляции тел
// функций любого модуля.
type module struct {
	name, path string
	// prefix — префикс глобальных имён fn модуля: у входного пустой
	// (`main`, как в программе из одного файла), у остальных `Name.`
	// (`Util.f`). Им же квалифицируются типы записей и вариантов в
	// рантайме: одноимённые типы двух модулей различны.
	prefix string
	// fns — имена fn модуля.
	fns map[string]bool
	// records — поля записей-деклараций `type X {...}` по имени типа
	// в порядке объявления (T-73, §4.7).
	records map[string][]string
	// ctors — конструкторы пользовательских вариантов по имени тега
	// (T-136, §14.2).
	ctors map[string]userCtor
	// locals — локальные имена модулей в файле (§11.1): `import A.B` —
	// `A.B` и `B`, `alias A.B as X` — `X` и `A.B`; значение — полное имя.
	locals map[string]string
}

func newModule(name, path, prefix string) *module {
	return &module{
		name: name, path: path, prefix: prefix,
		fns:     map[string]bool{},
		records: map[string][]string{},
		ctors:   map[string]userCtor{},
		locals:  map[string]string{},
	}
}

// userCtor — конструктор варианта `type Type { Tag(...) }`. val — значение
// при ссылке на имя: сам вариант для конструктора без аргументов, иначе
// native-функция его арности (одна на программу — identity стабильна).
// typ — имя типа в рантайме (с префиксом модуля).
type userCtor struct {
	typ string
	val runtime.Value
}

// liftedFn — локальная fn после лямбда-лифтинга: захваты передаются
// скрытыми параметрами после arity объявленных. caps заданы
// относительно функции-владельца (как upvalueInfo её прямого потомка):
// на месте вызова их достаёт fetchCapture по связыванию, а не по имени.
//
// У variadic-fn захваты идут ПЕРЕД параметрами (хвост занят rest-списком,
// который у спред-вызова обязан быть последним аргументом).
type liftedFn struct {
	arity    int // объявленные параметры; у variadic — fixed (без rest)
	variadic bool
	caps     []upvalueInfo
}

// New создаёт компилятор. До CompileProgram текущий модуль — пустой
// входной (REPL).
func New() *Compiler {
	m := newModule("", "", "")
	return &Compiler{entry: m, cur: m}
}

// SetGeneration задаёт поколение кода n (recompile() в REPL, T-208):
// глобальные имена поднятых локальных fn получают суффикс `@n`, и старый
// кадр функции модуля вызывает свою версию локальной fn, а не новую.
// n == 0 — без суффикса.
func (c *Compiler) SetGeneration(n int) {
	c.gen = ""
	if n > 0 {
		c.gen = fmt.Sprintf("@%d", n)
	}
}

// SetWhere задаёт место определения для печати лямбд, которые компилируются
// дальше (`<repl>:3`); "" — файл и строка исходника.
func (c *Compiler) SetWhere(where string) { c.where = where }

// Image возвращает собранный ProgramImage (для REPL).
func (c *Compiler) Image() *ProgramImage { return c.image }

// compileError — внутренняя ошибка компиляции, поднимаемая через panic
// и перехватываемая в Compile/CompileReplLine.
type compileError struct {
	msg string
	pos vm.SrcPos
}

func (e compileError) Error() string { return e.msg }

// Error — ошибка компиляции с позицией узла AST (§E.1: line и col с 1,
// col в code points). Line == 0 — позиция неизвестна. File — путь
// модуля, в котором ошибка (CompileProgram); в Error() не входит.
type Error struct {
	File      string
	Line, Col int
	Msg       string
}

func (e *Error) Error() string {
	if e.Line == 0 {
		return e.Msg
	}
	return fmt.Sprintf("%d:%d: %s", e.Line, e.Col, e.Msg)
}

// wrapCtx добавляет к ошибке префикс контекста, сохраняя позицию. Ошибке
// без позиции даётся позиция at (позиция объемлющего объявления).
func wrapCtx(at vm.SrcPos, err error, format string, args ...any) error {
	ctx := fmt.Sprintf(format, args...)
	var ce *Error
	if errors.As(err, &ce) {
		return &Error{Line: ce.Line, Col: ce.Col, Msg: ctx + ": " + ce.Msg}
	}
	return &Error{Line: int(at.Line), Col: int(at.Col), Msg: ctx + ": " + err.Error()}
}

// locate приписывает ошибке позицию текущего узла, если у неё ещё нет
// позиции: самая глубокая конструкция, в которой ошибка возникла, побеждает.
func (fc *funcCompiler) locate(err error) error {
	var ce *Error
	if err == nil || errors.As(err, &ce) || fc.pos.Line == 0 {
		return err
	}
	return &Error{Line: int(fc.pos.Line), Col: int(fc.pos.Col), Msg: err.Error()}
}

// ---- funcCompiler ----

// constKey — ключ дедупликации скалярных констант (§8).
type constKey struct {
	kind runtime.Kind
	i    int64
	f    uint64
	s    string
}

// upvalueInfo — захваченная переменная. Пустое name — захват,
// добавленный по связыванию (fetchCapture), а не по имени.
type upvalueInfo struct {
	name    string
	isLocal bool
	index   int
}

// scope — лексическая область видимости.
type scope struct {
	names map[string]int
	mark  int
}

// funcCompiler — состояние компиляции одной функции.
type funcCompiler struct {
	compiler  *Compiler
	parent    *funcCompiler
	prefix    string
	chunk     *vm.Chunk
	nextReg   int
	maxReg    int
	scopes    []scope
	bound     [vm.MaxRegs]bool
	localFns  map[string]string
	upvalues  []upvalueInfo
	caps      []upvalueInfo // скрытые параметры лифтнутой fn (compileClauses)
	capBase   int           // регистр первого скрытого параметра
	consts    map[constKey]int
	trapDepth int
	lambdaSeq int // уникальный суффикс для лямбд этой функции (A-F6)
	pos       vm.SrcPos
	// patSlots — предаллоцированные регистры имён паттерна (let-связывания
	// в trap с ensure); compilePattern берёт слот отсюда вместо allocReg.
	patSlots map[string]int
}

// dest — назначение результата выражения (§7).
type dest struct {
	reg  int  // регистр результата; в tail-контексте — scratch
	tail bool // результат — значение функции, управление не возвращается
	// ens — хвост тела trap с ensure (doc 02 §5.1): tail == false,
	// результат пишется в reg, но вызов здесь эмитится TAILCALLENS.
	// Наследуется только хвостовыми позициями, как tail.
	ens *ensTail
}

// ensTail — контекст хвоста тела trap с ensure: ensure этого trap и
// глубина областей тела (замыкания ensure видят только их).
type ensTail struct {
	ensures []ast.Expr
	scopes  int
}

// discard — dest, отбрасывающий результат.
var discard = dest{reg: -1}

// val — dest с целевым регистром.
func val(r int) dest { return dest{reg: r} }

// posOf извлекает (line, col) из узла AST (см. sema.posOf).
func posOf(n ast.Node) vm.SrcPos {
	return vm.SrcPos{Line: int32(n.Pos()), Col: int32(n.End())}
}

func (c *Compiler) newFuncCompiler(parent *funcCompiler) *funcCompiler {
	chunk := vm.NewChunk()
	chunk.File = c.cur.path
	chunk.Where = c.where
	return &funcCompiler{
		compiler: c,
		parent:   parent,
		chunk:    chunk,
		localFns: make(map[string]string),
		consts:   make(map[constKey]int),
		scopes:   []scope{{names: map[string]int{}, mark: 0}},
	}
}

// ---- allocator (§7) ----

func (fc *funcCompiler) fail(format string, args ...any) {
	panic(compileError{msg: fmt.Sprintf(format, args...), pos: fc.pos})
}

func (fc *funcCompiler) allocReg() int {
	if fc.nextReg >= vm.MaxRegs {
		fc.fail("function %q needs more than %d registers", fc.prefix, vm.MaxRegs)
	}
	r := fc.nextReg
	fc.nextReg++
	if fc.nextReg > fc.maxReg {
		fc.maxReg = fc.nextReg
	}
	return r
}

func (fc *funcCompiler) releaseToMark(mark int) {
	for r := mark; r < fc.nextReg; r++ {
		fc.bound[r] = false
	}
	fc.nextReg = mark
}

func (fc *funcCompiler) pushScope() {
	fc.scopes = append(fc.scopes, scope{
		names: map[string]int{},
		mark:  fc.nextReg,
	})
}

func (fc *funcCompiler) popScope() {
	if len(fc.scopes) <= 1 {
		return
	}
	s := fc.scopes[len(fc.scopes)-1]
	fc.scopes = fc.scopes[:len(fc.scopes)-1]
	fc.releaseToMark(s.mark)
}

func (fc *funcCompiler) bindLocal(name string, r int) {
	if r < 0 || r >= fc.nextReg {
		fc.fail("bindLocal: reg %d out of range [0,%d)", r, fc.nextReg)
	}
	fc.scopes[len(fc.scopes)-1].names[name] = r
	fc.bound[r] = true
}

func (fc *funcCompiler) resolveLocal(name string) (int, bool) {
	for i := len(fc.scopes) - 1; i >= 0; i-- {
		if r, ok := fc.scopes[i].names[name]; ok {
			return r, true
		}
	}
	return 0, false
}

// resolveUpvalue ищет name в цепочке parents, добавляя upvalue на каждом
// промежуточном уровне (§7, D-4). Возвращает индекс upvalue в текущем
// компиляторе.
func (fc *funcCompiler) resolveUpvalue(name string) (int, bool) {
	for i, uv := range fc.upvalues {
		if uv.name == name {
			return i, true
		}
	}
	p := fc.parent
	if p == nil {
		return 0, false
	}
	if r, ok := p.resolveLocal(name); ok {
		return fc.addUpvalue(upvalueInfo{name: name, isLocal: true, index: r}), true
	}
	if i, ok := p.resolveUpvalue(name); ok {
		return fc.addUpvalue(upvalueInfo{name: name, isLocal: false, index: i}), true
	}
	return 0, false
}

// addUpvalue — индекс upvalue со связыванием uv; одно связывание
// захватывается один раз, даже если сначала пришло без имени.
func (fc *funcCompiler) addUpvalue(uv upvalueInfo) int {
	if i, ok := findCapture(fc.upvalues, uv); ok {
		if fc.upvalues[i].name == "" {
			fc.upvalues[i].name = uv.name
		}
		return i
	}
	fc.upvalues = append(fc.upvalues, uv)
	return len(fc.upvalues) - 1
}

// findCapture ищет связывание uv (без учёта имени).
func findCapture(caps []upvalueInfo, uv upvalueInfo) (int, bool) {
	for i, c := range caps {
		if c.isLocal == uv.isLocal && c.index == uv.index {
			return i, true
		}
	}
	return 0, false
}

// fetchCapture — где в fc лежит связывание uv функции owner (uv задан
// относительно owner). Промежуточные уровни получают его по
// связыванию, а не по имени: имя в точке вызова может быть затенено
// (T-39). Лифтнутая fn держит его скрытым параметром, прочие — upvalue;
// недостающий захват лифтнутой fn добирает фикспойнт declareLocalFns.
func (fc *funcCompiler) fetchCapture(owner *funcCompiler, uv upvalueInfo) (isLocal bool, index int) {
	if fc == owner {
		return uv.isLocal, uv.index
	}
	if fc.parent == nil {
		fc.fail("internal: capture owner is not an ancestor of %q", fc.prefix)
	}
	pl, pi := fc.parent.fetchCapture(owner, uv)
	in := upvalueInfo{isLocal: pl, index: pi}
	if i, ok := findCapture(fc.caps, in); ok {
		return true, fc.capBase + i
	}
	return false, fc.addUpvalue(in)
}

// loadCapture кладёт связывание uv функции owner в регистр r.
func (fc *funcCompiler) loadCapture(owner *funcCompiler, uv upvalueInfo, r int) {
	isLocal, idx := fc.fetchCapture(owner, uv)
	switch {
	case !isLocal:
		fc.emit(vm.ABC(vm.GETUPVAL, r, idx, 0))
	case idx != r:
		fc.emit(vm.ABC(vm.MOVE, r, idx, 0))
	}
}

// ---- constants (§8) ----

func (fc *funcCompiler) konst(v runtime.Value) int {
	var k constKey
	switch v.Kind {
	case runtime.KindBool:
		b := int64(0)
		if v.Bool {
			b = 1
		}
		k = constKey{kind: v.Kind, i: b}
	case runtime.KindInt:
		if v.IsSmall {
			k = constKey{kind: v.Kind, i: v.SmallInt}
		} else {
			return fc.chunk.AddConstant(v)
		}
	case runtime.KindFloat:
		k = constKey{kind: v.Kind, f: math.Float64bits(v.Float)}
	case runtime.KindStr:
		k = constKey{kind: v.Kind, s: v.Str}
	case runtime.KindAtom:
		k = constKey{kind: v.Kind, s: v.Atom}
	case runtime.KindUnit:
		k = constKey{kind: v.Kind}
	default:
		return fc.chunk.AddConstant(v)
	}
	if idx, ok := fc.consts[k]; ok {
		return idx
	}
	idx := fc.chunk.AddConstant(v)
	fc.consts[k] = idx
	return idx
}

// ---- emit (§7, I-3, I-4) ----

func (fc *funcCompiler) emit(i vm.Instr) int {
	reads, writes, err := vm.RegUse(i)
	if err != nil {
		fc.fail("emit: %v", err)
	}
	for _, r := range reads {
		if r < 0 || r >= fc.nextReg {
			fc.fail("emit: %s reads r%d >= nextReg %d (I-4)", i.Op(), r, fc.nextReg)
		}
	}
	for _, r := range writes {
		if r < 0 || r >= fc.nextReg {
			fc.fail("emit: %s writes r%d >= nextReg %d (I-4)", i.Op(), r, fc.nextReg)
		}
	}
	// I-3: no write via operand A into a bound register.
	// MATCHLOCAL reads A and writes pattern slots implicitly (not in RegUse.writes).
	if i.Op() != vm.MATCHLOCAL {
		a := i.A()
		for _, r := range writes {
			if r == a && fc.bound[a] {
				fc.fail("emit: %s writes bound r%d (I-3)", i.Op(), a)
			}
		}
	}
	return fc.chunk.Emit(i, fc.pos)
}

func (fc *funcCompiler) emitJump(op vm.OpCode, a int) int {
	return fc.emit(vm.AsBx(op, a, 0))
}

func (fc *funcCompiler) patchHere(at int) {
	if err := fc.chunk.PatchJump(at, len(fc.chunk.Code)); err != nil {
		fc.fail("patch: %v", err)
	}
}

// ---- helpers ----

func (fc *funcCompiler) destReg(d dest) int {
	if d.reg != -1 {
		return d.reg
	}
	return fc.allocReg()
}

func (fc *funcCompiler) finish(d dest, from int) {
	switch {
	case d.tail:
		fc.emit(vm.ABC(vm.RETURN, from, 0, 0))
	case d.reg == -1:
		// discard
	case from != d.reg:
		fc.emit(vm.ABC(vm.MOVE, d.reg, from, 0))
	}
}

func (fc *funcCompiler) loadConst(v runtime.Value, d dest) error {
	if d.tail {
		r := fc.allocReg()
		idx := fc.konst(v)
		fc.emit(vm.ABx(vm.LOADK, r, idx))
		fc.emit(vm.ABC(vm.RETURN, r, 0, 0))
		fc.releaseToMark(r)
		return nil
	}
	if d.reg == -1 {
		return nil
	}
	idx := fc.konst(v)
	fc.emit(vm.ABx(vm.LOADK, d.reg, idx))
	return nil
}

func (fc *funcCompiler) loadUnit(d dest) error {
	return fc.loadConst(runtime.Unit, d)
}

// operand: регистр со значением e. Локальная переменная — её собственный
// регистр, код не эмитится.
func (fc *funcCompiler) operand(e ast.Expr) (int, error) {
	if v, ok := e.(ast.VariableExpr); ok {
		if r, ok := fc.resolveLocal(v.Name()); ok {
			return r, nil
		}
	}
	r := fc.allocReg()
	if err := fc.compileExpr(e, val(r)); err != nil {
		return 0, err
	}
	return r, nil
}

// operandInto: то же, но «нелокальное» вычисляется прямо в into.
func (fc *funcCompiler) operandInto(e ast.Expr, into int) (int, error) {
	if v, ok := e.(ast.VariableExpr); ok {
		if r, ok := fc.resolveLocal(v.Name()); ok {
			return r, nil
		}
	}
	if err := fc.compileExpr(e, val(into)); err != nil {
		return 0, err
	}
	return into, nil
}

// ---- entry points ----
