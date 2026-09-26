// Package compiler — компиляция AST в регистровый байткод ВМ.
//
// Sprint 7, S7.2 + S7.6. Дизайн: docs/02-register-based-virtual-machine.md §7.
//
// Аллокатор — bump-указатель со стековой дисциплиной (nextReg + releaseToMark).
// Соглашение о вызовах (§3): callee в R[A], аргументы в R[A+1..A+B], результат
// в R[C]. Хвостовость — поле dest.tail; TAILCALL эмитится только вне trap.
package compiler

import (
	"fmt"
	"math"
	"math/big"
	"slices"
	"sort"
	"strconv"
	"strings"

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
	// records — поля записей-деклараций `type X {...}` по имени типа
	// в порядке объявления (T-73, §4.7).
	records map[string][]string
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

// New создаёт компилятор.
func New() *Compiler { return &Compiler{} }

// Image возвращает собранный ProgramImage (для REPL).
func (c *Compiler) Image() *ProgramImage { return c.image }

// compileError — внутренняя ошибка компиляции, поднимаемая через panic
// и перехватываемая в Compile/CompileReplLine.
type compileError struct{ msg string }

func (e compileError) Error() string { return e.msg }

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
}

// dest — назначение результата выражения (§7).
type dest struct {
	reg  int  // регистр результата; в tail-контексте — scratch
	tail bool // результат — значение функции, управление не возвращается
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
	return &funcCompiler{
		compiler: c,
		parent:   parent,
		chunk:    vm.NewChunk(),
		localFns: make(map[string]string),
		consts:   make(map[constKey]int),
		scopes:   []scope{{names: map[string]int{}, mark: 0}},
	}
}

// ---- allocator (§7) ----

func (fc *funcCompiler) fail(format string, args ...any) {
	panic(compileError{msg: fmt.Sprintf(format, args...)})
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

// Compile — входная точка модуля.
func (c *Compiler) Compile(prog *ast.Program) (image *ProgramImage, err error) {
	c.image = &ProgramImage{Functions: make(map[string]*vm.Function)}
	c.lifted = make(map[string]*liftedFn)
	c.records = make(map[string][]string)
	defer func() {
		if r := recover(); r != nil {
			if ce, ok := r.(compileError); ok {
				image = nil
				err = fmt.Errorf("compile: %s", ce.msg)
				return
			}
			panic(r)
		}
	}()

	// Записи-декларации — до функций: порядок деклараций свободный (§11.2).
	for _, d := range prog.Decls {
		if td, ok := d.(ast.TypeDecl); ok {
			if fields, ok := td.RecordFields(); ok {
				c.records[td.TypeName()] = fields
			}
		}
	}

	for _, d := range prog.Decls {
		fd, ok := d.(ast.FuncDecl)
		if !ok {
			continue
		}
		clauses := fd.FuncClauses()
		if len(clauses) == 0 {
			continue
		}
		fn, cerr := c.compileNamedFn(fd.FnName(), clauses)
		if cerr != nil {
			return nil, fmt.Errorf("fn %s: %w", fd.FnName(), cerr)
		}
		c.image.Functions[fd.FnName()] = fn
		if fd.FnName() == "main" {
			c.image.Main = fn
		}
	}

	if len(prog.Stmts) > 0 {
		fn, cerr := c.compileBlock("__repl__", nil, prog.Stmts)
		if cerr != nil {
			return nil, cerr
		}
		c.image.Functions["__repl__"] = fn
		c.image.Main = fn
	}

	if Verify {
		if verr := verifyImage(c.image); verr != nil {
			return nil, verr
		}
	}
	return c.image, nil
}

// verifyImage прогоняет vm.Verify по всем функциям модуля в
// детерминированном порядке имён.
func verifyImage(img *ProgramImage) error {
	names := make([]string, 0, len(img.Functions))
	for name := range img.Functions {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := vm.Verify(img.Functions[name].Chunk); err != nil {
			return fmt.Errorf("verify %s: %w", name, err)
		}
	}
	return nil
}

func (c *Compiler) compileNamedFn(name string, clauses []ast.FnClauseArg) (*vm.Function, error) {
	fc := c.newFuncCompiler(nil)
	fc.prefix = name + "$"
	if err := fc.compileClauses(clauses, nil); err != nil {
		return nil, err
	}
	return fc.function(name), nil
}

// function собирает vm.Function из скомпилированного чанка fn.
func (fc *funcCompiler) function(name string) *vm.Function {
	fc.chunk.NumRegs = fc.maxReg
	arity := fc.chunk.NumParams
	if fc.chunk.Variadic {
		arity = -1
	}
	return &vm.Function{Name: name, Arity: arity, Chunk: fc.chunk}
}

func (c *Compiler) compileBlock(name string, params []string, stmts []ast.Stmt) (*vm.Function, error) {
	fc := c.newFuncCompiler(nil)
	fc.prefix = name + "$"

	fc.chunk.NumParams = len(params)
	for i, p := range params {
		r := fc.allocReg()
		if r != i {
			return nil, fmt.Errorf("internal: block param %d in r%d", i, r)
		}
		fc.bindLocal(p, i)
	}

	if len(stmts) == 0 {
		scratch := fc.allocReg()
		if err := fc.loadUnit(dest{reg: scratch, tail: true}); err != nil {
			return nil, err
		}
	} else {
		scratch := fc.allocReg()
		if err := fc.compileStmts(stmts, dest{reg: scratch, tail: true}); err != nil {
			return nil, err
		}
	}
	fc.chunk.NumRegs = fc.maxReg

	return &vm.Function{Name: name, Arity: len(params), Chunk: fc.chunk}, nil
}

// clauseVariadic — оканчивается ли клоз `..name`; спред не в конце — ошибка.
func clauseVariadic(params []ast.Pattern) (bool, error) {
	variadic := false
	for i, p := range params {
		if _, ok := p.(ast.SpreadPattern); ok {
			if i != len(params)-1 {
				return false, fmt.Errorf("variadic parameter %q must be last", p)
			}
			variadic = true
		}
	}
	return variadic, nil
}

// checkLambdaParams — fail-fast для полной лямбды `fn (…) ->` (T-44).
// `..name` допустим последним параметром (§6.3); параметр-паттерн — ошибка,
// а не молчаливое имя.
func checkLambdaParams(params []string) error {
	for i, p := range params {
		if strings.HasPrefix(p, "..") {
			if i != len(params)-1 {
				return fmt.Errorf("variadic parameter %q must be last", p)
			}
			p = strings.TrimPrefix(p, "..")
		}
		if !isIdentParam(p) {
			return fmt.Errorf("срез: параметр-паттерн %q в лямбде не реализован", p)
		}
	}
	return nil
}

// isIdentParam: LOWER_IDENT (с опциональным trailing `?`), не true/false.
func isIdentParam(p string) bool {
	if p == "" || p[0] < 'a' || p[0] > 'z' || p == "true" || p == "false" {
		return false
	}
	for i := 1; i < len(p); i++ {
		c := p[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_':
		case c == '?' && i == len(p)-1:
		default:
			return false
		}
	}
	return true
}

// clausesShape — форма чанка мультиклозной fn: fixed — минимум фиксированных
// параметров по клозам, variadic — есть ли клоз с `..name`. Разные арности
// допустимы только при наличии variadic-клоза; тогда все аргументы сверх
// fixed приходят в rest-списке, а клозы разбирают его list-паттерном.
func clausesShape(clauses []ast.FnClauseArg) (fixed int, variadic bool, err error) {
	fixed = -1
	arity := -1
	for _, cl := range clauses {
		v, verr := clauseVariadic(cl.Params)
		if verr != nil {
			return 0, false, verr
		}
		k := len(cl.Params)
		if v {
			k--
			variadic = true
		}
		if fixed < 0 || k < fixed {
			fixed = k
		}
		if arity >= 0 && len(cl.Params) != arity {
			arity = -2
		} else if arity == -1 {
			arity = len(cl.Params)
		}
	}
	if !variadic && arity == -2 {
		return 0, false, fmt.Errorf("клозы разной арности без variadic")
	}
	return fixed, variadic, nil
}

// compileClauses компилирует клозы fn (§6.1). Параметры лежат в
// r0..n-1, за ними — скрытые параметры захвата caps (лямбда-лифтинг
// локальной fn). Клозы проверяются по порядку: ident, `..name` и `_`
// связываются без проверки, прочие паттерны — MATCHLOCAL, затем guard
// (JMPIFNOT). Если ни один не подошёл — raise (:function_clause, [args]);
// после неопровержимого последнего клоза raise не эмитится.
func (fc *funcCompiler) compileClauses(clauses []ast.FnClauseArg, caps []upvalueInfo) error {
	if len(clauses) == 0 {
		return fmt.Errorf("нет клозов")
	}
	fixed, variadic, err := clausesShape(clauses)
	if err != nil {
		return err
	}
	// Регистры: [caps (у variadic)] fixed… [rest] [caps (иначе)].
	base, n := 0, fixed
	if variadic {
		base, n = len(caps), fixed+1
		fc.capBase = 0
	} else {
		fc.capBase = n
	}
	fc.caps = caps

	fc.chunk.Variadic = variadic
	fc.chunk.NumParams = n + len(caps)
	for i := 0; i < n+len(caps); i++ {
		if r := fc.allocReg(); r != i {
			return fmt.Errorf("internal: param %d in r%d", i, r)
		}
	}
	for i, c := range caps {
		if c.name != "" {
			fc.bindLocal(c.name, fc.capBase+i)
		}
	}

	var fails []int
	for _, cl := range clauses {
		if fails, err = fc.compileOneClause(cl, base, fixed, variadic); err != nil {
			return err
		}
		here := len(fc.chunk.Code)
		for _, j := range fails {
			if perr := fc.chunk.PatchJump(j, here); perr != nil {
				fc.fail("patch: %v", perr)
			}
		}
	}
	if len(fails) > 0 {
		fc.raiseFunctionClause(clauses[0], base, n)
	}
	return nil
}

// compileOneClause компилирует один клоз и возвращает его fail-переходы
// (к следующему клозу). Пустой список — клоз неопровержим.
func (fc *funcCompiler) compileOneClause(cl ast.FnClauseArg, base, fixed int, variadic bool) ([]int, error) {
	fc.pushScope()
	defer fc.popScope()

	var fails []int
	for i, p := range cl.Params[:fixed] {
		switch x := p.(type) {
		case ast.IdentPattern:
			if isIdentParam(x.IdentName()) {
				fc.bindLocal(x.IdentName(), base+i)
				continue
			}
		case ast.PatternWildcard:
			if p.String() == "_" {
				continue
			}
		}
		cp, err := fc.compilePattern(p)
		if err != nil {
			return nil, err
		}
		patIdx := fc.chunk.AddPattern(cp)
		fc.pos = posOf(p)
		fc.emit(vm.ABx(vm.MATCHLOCAL, base+i, patIdx))
		fails = append(fails, fc.emitJump(vm.JMP, 0))
	}
	if variadic {
		jump, err := fc.bindRestTail(cl, base, fixed)
		if err != nil {
			return nil, err
		}
		if jump >= 0 {
			fails = append(fails, jump)
		}
	}
	if cl.Guard != nil {
		mark := fc.nextReg
		g := fc.allocReg()
		if err := fc.compileExpr(cl.Guard, val(g)); err != nil {
			return nil, err
		}
		fc.pos = posOf(cl.Guard)
		fails = append(fails, fc.emitJump(vm.JMPIFNOT, g))
		fc.releaseToMark(mark)
	}

	scratch := fc.allocReg()
	if cl.Body == nil || len(cl.Body.Body()) == 0 {
		return fails, fc.loadUnit(dest{reg: scratch, tail: true})
	}
	return fails, fc.compileStmts(cl.Body.Body(), dest{reg: scratch, tail: true})
}

// bindRestTail связывает параметры клоза сверх fixed с rest-списком в
// r[base+fixed]: голый `..name` — просто имя, иначе list-паттерн
// (точная длина без `..`). Возвращает fail-переход или -1.
func (fc *funcCompiler) bindRestTail(cl ast.FnClauseArg, base, fixed int) (int, error) {
	tail := cl.Params[fixed:]
	elems, restName, hasRest := tail, "", false
	if n := len(tail); n > 0 {
		if sp, ok := tail[n-1].(ast.SpreadPattern); ok {
			elems, restName, hasRest = tail[:n-1], sp.SpreadName(), true
		}
	}
	if len(elems) == 0 && hasRest {
		fc.bindLocal(restName, base+fixed)
		return -1, nil
	}
	cp, err := fc.compilePattern(ast.NewListPattern(elems, hasRest, restName, 0, 0))
	if err != nil {
		return -1, err
	}
	switch {
	case len(tail) > 0:
		fc.pos = posOf(tail[0])
	case len(cl.Params) > 0:
		fc.pos = posOf(cl.Params[0])
	}
	fc.emit(vm.ABx(vm.MATCHLOCAL, base+fixed, fc.chunk.AddPattern(cp)))
	return fc.emitJump(vm.JMP, 0), nil
}

// raiseFunctionClause — (:function_clause, [arg0, …]) (§6.1, §5.3).
// Аргументы уже лежат подряд в r[base]..r[base+arity-1]. У variadic последний элемент
// списка — сам rest-список: конкатенации в байткоде нет.
func (fc *funcCompiler) raiseFunctionClause(first ast.FnClauseArg, base, arity int) {
	switch {
	case len(first.Params) > 0:
		fc.pos = posOf(first.Params[0])
	case first.Guard != nil:
		fc.pos = posOf(first.Guard)
	}
	w := fc.allocReg()
	fc.emit(vm.ABx(vm.LOADK, w, fc.konst(runtime.Atom("function_clause"))))
	l := fc.allocReg()
	fc.emit(vm.ABC(vm.LIST, l, base, arity))
	fc.emit(vm.ABC(vm.TUPLE, w, w, 2))
	fc.emit(vm.ABC(vm.RAISE, w, 0, 0))
}

func localClauses(cs []ast.LocalFnClauseArg) []ast.FnClauseArg {
	out := make([]ast.FnClauseArg, len(cs))
	for i, c := range cs {
		out[i] = ast.FnClauseArg(c)
	}
	return out
}

// ---- лямбда-лифтинг локальных fn (T-51) ----

// declareLocalFns регистрирует имена локальных fn блока до компиляции
// тел (взаимная рекурсия, §6.5) и вычисляет их захваты — скрытые
// параметры после объявленных. sema держит объявления в начале блока,
// так что видимые снаружи имена к этому моменту уже связаны.
//
// Захваты считаются фикспойнтом: вызов другой локальной fn блока
// (или её значение) передаёт и её захваты, поэтому они транзитивны.
// Итерация компилирует тела во временных funcCompiler и отбрасывает
// результат; новые upvalue дописываются в захваты (по связыванию, см.
// fetchCapture), наборы только растут.
func (fc *funcCompiler) declareLocalFns(stmts []ast.Stmt) error {
	var decls []ast.LocalFnDecl
	for _, s := range stmts {
		if lfd, ok := s.(ast.LocalFnDecl); ok {
			mangled := fc.prefix + lfd.FnName()
			fc.localFns[lfd.FnName()] = mangled
			arity, variadic, err := clausesShape(localClauses(lfd.Clauses()))
			if err != nil {
				return fmt.Errorf("local fn %s: %w", lfd.FnName(), err)
			}
			fc.compiler.lifted[mangled] = &liftedFn{arity: arity, variadic: variadic}
			decls = append(decls, lfd)
		}
	}
	for changed := len(decls) > 0; changed; {
		changed = false
		for _, d := range decls {
			mangled := fc.localFns[d.FnName()]
			lf := fc.compiler.lifted[mangled]
			probe := fc.compiler.newFuncCompiler(fc)
			probe.prefix = mangled + "$"
			if err := probe.compileClauses(localClauses(d.Clauses()), lf.caps); err != nil {
				return fmt.Errorf("local fn %s: %w", d.FnName(), err)
			}
			for _, uv := range probe.upvalues {
				i, ok := findCapture(lf.caps, uv)
				switch {
				case !ok:
					lf.caps = append(lf.caps, uv)
				case lf.caps[i].name == "" && uv.name != "":
					lf.caps[i].name = uv.name
				default:
					continue
				}
				changed = true
			}
		}
	}
	return nil
}

// resolveLocalFn — mangled-имя локальной fn и её владелец (fn, в
// которой она объявлена) по правилам compileVar: по уровням parent,
// на каждом локаль уровня затеняет localFns этого уровня и предков
// (T-56).
func (fc *funcCompiler) resolveLocalFn(name string) (string, *funcCompiler, bool) {
	for p := fc; p != nil; p = p.parent {
		if _, ok := p.resolveLocal(name); ok {
			return "", nil, false
		}
		if mangled, ok := p.localFns[name]; ok {
			return mangled, p, true
		}
	}
	return "", nil, false
}

// CompileReplLine компилирует одну REPL-строку как функцию от видимых имён.
func (c *Compiler) CompileReplLine(names []string, s ast.Stmt) (fn *vm.Function, newName string, err error) {
	defer func() {
		if r := recover(); r != nil {
			if ce, ok := r.(compileError); ok {
				fn = nil
				newName = ""
				err = ce
				return
			}
			panic(r)
		}
	}()

	c.image = &ProgramImage{Functions: make(map[string]*vm.Function)}
	c.lifted = make(map[string]*liftedFn)

	fc := c.newFuncCompiler(nil)
	fc.prefix = "__repl__$"
	fc.chunk.NumParams = len(names)

	for i, n := range names {
		r := fc.allocReg()
		if r != i {
			return nil, "", fmt.Errorf("internal: repl param %d in r%d", i, r)
		}
		fc.bindLocal(n, i)
	}

	switch st := s.(type) {
	case ast.LetBind:
		ip, ok := st.Pat().(ast.IdentPattern)
		if !ok {
			return nil, "", fmt.Errorf("repl: only simple `name = expr` bindings are supported")
		}
		newName = ip.IdentName()
		r := fc.allocReg()
		if err := fc.compileExpr(st.Val(), val(r)); err != nil {
			return nil, "", err
		}
		fc.emit(vm.ABC(vm.RETURN, r, 0, 0))
	case ast.ExprStmt:
		scratch := fc.allocReg()
		if err := fc.compileExpr(st.ExprValue(), dest{reg: scratch, tail: true}); err != nil {
			return nil, "", err
		}
	default:
		return nil, "", fmt.Errorf("repl: unsupported statement %T", s)
	}

	fc.chunk.NumRegs = fc.maxReg
	fn = &vm.Function{Name: "__repl__", Arity: len(names), Chunk: fc.chunk}
	if Verify {
		if verr := vm.Verify(fn.Chunk); verr != nil {
			return nil, "", fmt.Errorf("verify __repl__: %w", verr)
		}
	}
	return fn, newName, nil
}

// ---- statements ----

func (fc *funcCompiler) compileStmts(stmts []ast.Stmt, d dest) error {
	if len(stmts) == 0 {
		return fc.loadUnit(d)
	}

	// Local fn names first — для взаимной рекурсии (§6.5).
	if err := fc.declareLocalFns(stmts); err != nil {
		return err
	}

	for i, s := range stmts {
		var sd dest
		if i == len(stmts)-1 {
			sd = d
		} else {
			sd = discard
		}
		if err := fc.compileStmt(s, sd); err != nil {
			return err
		}
	}
	return nil
}

func (fc *funcCompiler) compileStmt(s ast.Stmt, d dest) error {
	if p := posOf(s); p.Line > 0 {
		saved := fc.pos
		fc.pos = p
		defer func() { fc.pos = saved }()
	}
	switch st := s.(type) {
	case ast.LetBind:
		return fc.compileLetBind(st, d)
	case ast.ExprStmt:
		return fc.compileExpr(st.ExprValue(), d)
	case ast.LocalFnDecl:
		return fc.compileLocalFn(st, d)
	}
	return fmt.Errorf("срез: неподдерживаемый стейтмент %T", s)
}

func (fc *funcCompiler) compileLetBind(st ast.LetBind, d dest) error {
	ip, ok := st.Pat().(ast.IdentPattern)
	if !ok {
		return fmt.Errorf("срез: только простые связывания `name = expr`")
	}
	name := ip.IdentName()
	r := fc.allocReg()
	if err := fc.compileExpr(st.Val(), val(r)); err != nil {
		return err
	}
	fc.bindLocal(name, r)
	// Значение let-стейтмента — () (совместимо с регистровой VM).
	return fc.loadUnit(d)
}

func (fc *funcCompiler) compileLocalFn(decl ast.LocalFnDecl, d dest) error {
	name := decl.FnName()
	mangled, ok := fc.localFns[name]
	if !ok {
		mangled = fc.prefix + name
		fc.localFns[name] = mangled
	}

	// Захваты — скрытые параметры после объявленных (лямбда-лифтинг).
	var caps []upvalueInfo
	if lf := fc.compiler.lifted[mangled]; lf != nil {
		caps = lf.caps
	}
	child := fc.compiler.newFuncCompiler(fc)
	child.prefix = mangled + "$"
	if err := child.compileClauses(localClauses(decl.Clauses()), caps); err != nil {
		return fmt.Errorf("local fn %s: %w", name, err)
	}
	// fn кладётся как глобальная Function: upvalue здесь — захват мимо
	// фикспойнта declareLocalFns, в рантайме это `internal: upvalue`.
	if len(child.upvalues) > 0 {
		fc.fail("internal: local fn %s: capture %q outside lifted params", name, child.upvalues[0].name)
	}
	fc.compiler.image.Functions[mangled] = child.function(mangled)

	// Значение local fn — () (совместимо с регистровой VM).
	return fc.loadUnit(d)
}

// ---- expressions ----

// i1TestLeak — white-box hook for TestCompileExprStackNeutral (T-32).
// When set, compileExpr calls it instead of the real dispatch so the test
// can simulate a callee that forgets releaseToMark.
var i1TestLeak func(*funcCompiler)

func (fc *funcCompiler) compileExpr(e ast.Expr, d dest) (err error) {
	// I-1: compileExpr стек-нейтрален. Регистр dest выделяет вызывающий
	// до входа; временные внутри обязаны быть сняты releaseToMark.
	entry := fc.nextReg
	defer func() {
		if err != nil || fc.nextReg == entry {
			return
		}
		// Уже паникуем (fc.fail в callee) — не подменять исходную ошибку I-1.
		if p := recover(); p != nil {
			panic(p)
		}
		fc.fail("compileExpr: nextReg %d != %d (I-1)", fc.nextReg, entry)
	}()
	if i1TestLeak != nil {
		i1TestLeak(fc)
		return nil
	}
	// Позиция узла действует, пока он компилируется; инструкции
	// родителя после него снова получают позицию родителя (O-F4, T-41).
	if p := posOf(e); p.Line > 0 {
		saved := fc.pos
		fc.pos = p
		defer func() { fc.pos = saved }()
	}
	switch ex := e.(type) {
	case ast.LiteralExpr:
		return fc.compileLiteralExpr(ex.ValueStr(), d)
	case ast.InterpExpr:
		return fc.compileInterp(ex, d)
	case ast.AtomExpr:
		return fc.loadConst(runtime.Atom(ex.AtomName()), d)
	case ast.DecimalExpr:
		return fc.compileDecimal(ex.ValueStr(), d)
	case ast.BytesExpr:
		return fc.compileBytes(ex.ValueStr(), d)
	case ast.RegexExpr:
		return fmt.Errorf("срез: regex не реализован")
	case ast.VariableExpr:
		return fc.compileVar(ex.Name(), d)
	case ast.GroupingExpr:
		return fc.compileExpr(ex.Inner(), d)
	case ast.UnaryExpr:
		return fc.compileUnary(ex, d)
	case ast.BinaryExpr:
		return fc.compileBinary(ex, d)
	case ast.CallExpr:
		return fc.compileCall(ex, d)
	case ast.IfExpr:
		return fc.compileIf(ex, d)
	case ast.TrapExpr:
		return fc.compileTrap(ex, d)
	case ast.MatchExpr:
		return fc.compileMatch(ex, d)
	case ast.WithExpr:
		return fc.compileWith(ex, d)
	case ast.RecvExpr:
		return fc.compileRecv(ex, d)
	case ast.RangeExpr:
		return fc.compileRange(ex, d)
	case ast.IndexExpr:
		return fc.compileIndex(ex, d)
	case ast.MemberExpr:
		return fc.compileMember(ex, d)
	case ast.LambdaShort:
		return fc.compileLambda("", []string{ex.ParamName()}, ex.Body(), d)
	case ast.LambdaEmpty:
		return fc.compileLambda("", nil, ex.Body(), d)
	case ast.LambdaFull:
		return fc.compileLambda("", ex.ParamNames(), ex.BlockBody(), d)
	case ast.PipeExpr:
		return fmt.Errorf("срез: pipe не реализован")
	}
	return fmt.Errorf("срез: неподдерживаемое выражение %T", e)
}

func (fc *funcCompiler) compileLiteralExpr(lit string, d dest) error {
	v, err := parseLiteralValue(lit)
	if err != nil {
		return err
	}
	return fc.loadConst(v, d)
}

// compileInterp lowers "a \(e) b" to string concat of decoded parts and
// to_str(e) for each interpolated expression (S-F1 / T-54).
// parts has len(exprs)+1; empty parts are skipped except as the initial
// accumulator when the string starts with \(...).
func (fc *funcCompiler) compileInterp(ie ast.InterpExpr, d dest) error {
	mark := fc.nextReg
	dst := fc.destReg(d)
	parts := ie.InterpParts()
	exprs := ie.InterpExprs()

	if err := fc.loadConst(runtime.Str(decodeStrBody(parts[0])), val(dst)); err != nil {
		return err
	}

	for i, expr := range exprs {
		iterMark := fc.nextReg

		strReg := fc.allocReg()
		if err := fc.compileGlobalCall("to_str", []ast.Expr{expr}, val(strReg), ie); err != nil {
			return err
		}
		fc.pos = posOf(ie)
		fc.emit(vm.ABC(vm.ADD, dst, dst, strReg))

		if parts[i+1] != "" {
			partReg := fc.allocReg()
			if err := fc.loadConst(runtime.Str(decodeStrBody(parts[i+1])), val(partReg)); err != nil {
				return err
			}
			fc.pos = posOf(ie)
			fc.emit(vm.ABC(vm.ADD, dst, dst, partReg))
		}

		fc.releaseToMark(iterMark)
	}

	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

func (fc *funcCompiler) compileDecimal(body string, d dest) error {
	r, err := runtime.ParseDecimal(body)
	if err != nil {
		return err
	}
	return fc.loadConst(runtime.Decimal(r), d)
}

func (fc *funcCompiler) compileBytes(body string, d dest) error {
	b, err := decodeBytesBody(body)
	if err != nil {
		return fmt.Errorf("bytes literal: %w", err)
	}
	return fc.loadConst(runtime.Bytes(b), d)
}

// compileVar — порядок разрешения (§7):
// локаль → локальная fn (своя и предков) → upvalue → глобал.
func (fc *funcCompiler) compileVar(name string, d dest) error {
	if r, ok := fc.resolveLocal(name); ok {
		return fc.loadVal(d, r)
	}
	if mangled, owner, ok := fc.resolveLocalFn(name); ok {
		return fc.loadLocalFn(d, mangled, owner)
	}
	if idx, ok := fc.resolveUpvalue(name); ok {
		return fc.loadUpval(d, idx)
	}
	return fc.loadGlobal(d, name)
}

// loadLocalFn — локальная fn как значение. Без захватов это глобальная
// Function. С захватами — замыкание над обёрткой: обёртка арности
// объявленных параметров хвостом вызывает лифтнутую fn, дописывая
// захваты из своих upvalue (T-39).
func (fc *funcCompiler) loadLocalFn(d dest, mangled string, owner *funcCompiler) error {
	lf := fc.compiler.lifted[mangled]
	if lf == nil || len(lf.caps) == 0 {
		return fc.loadGlobal(d, mangled)
	}
	if d.reg == -1 && !d.tail {
		return nil
	}

	w := fc.compiler.newFuncCompiler(nil)
	w.prefix = mangled + "$"
	w.pos = fc.pos // у обёртки нет своего исходника — позиция ссылки
	w.chunk.NumParams = lf.arity
	if lf.variadic {
		w.chunk.NumParams++ // rest-список
		w.chunk.Variadic = true
	}
	for i := 0; i < w.chunk.NumParams; i++ {
		w.allocReg()
	}
	wbase := w.allocReg()
	w.emit(vm.ABx(vm.GETGLOBAL, wbase, w.konst(runtime.Str(mangled))))
	movesCaps := func() {
		for j := range lf.caps {
			w.emit(vm.ABC(vm.GETUPVAL, w.allocReg(), j, 0))
		}
	}
	if lf.variadic {
		movesCaps()
	}
	for i := 0; i < w.chunk.NumParams; i++ {
		w.emit(vm.ABC(vm.MOVE, w.allocReg(), i, 0))
	}
	if lf.variadic {
		w.emit(vm.ABC(vm.TAILCALLSPREAD, wbase, w.chunk.NumParams+len(lf.caps), 0))
	} else {
		movesCaps()
		w.emit(vm.ABC(vm.TAILCALL, wbase, lf.arity+len(lf.caps), 0))
	}
	fnIdx := fc.chunk.AddConstant(vm.FuncValue(w.function(mangled)))

	mark := fc.nextReg
	dst := fc.destReg(d)
	base := fc.allocReg()
	fc.emit(vm.ABx(vm.LOADK, base, fnIdx))
	for j, uv := range lf.caps {
		r := fc.allocReg()
		if r != base+1+j {
			fc.fail("closure: capture %d in r%d, want r%d", j, r, base+1+j)
		}
		fc.loadCapture(owner, uv, r)
	}
	fc.emit(vm.ABC(vm.MAKECLOSURE, dst, base, len(lf.caps)))
	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

func (fc *funcCompiler) loadVal(d dest, src int) error {
	if d.tail {
		fc.emit(vm.ABC(vm.RETURN, src, 0, 0))
		return nil
	}
	if d.reg == -1 {
		return nil
	}
	if src != d.reg {
		fc.emit(vm.ABC(vm.MOVE, d.reg, src, 0))
	}
	return nil
}

func (fc *funcCompiler) loadGlobal(d dest, name string) error {
	var r int
	switch {
	case d.tail:
		r = fc.allocReg()
	case d.reg == -1:
		return nil
	default:
		r = d.reg
	}
	gidx := fc.konst(runtime.Str(name))
	fc.emit(vm.ABx(vm.GETGLOBAL, r, gidx))
	if d.tail {
		fc.emit(vm.ABC(vm.RETURN, r, 0, 0))
		fc.releaseToMark(r)
	}
	return nil
}

func (fc *funcCompiler) loadUpval(d dest, idx int) error {
	var r int
	switch {
	case d.tail:
		r = fc.allocReg()
	case d.reg == -1:
		return nil
	default:
		r = d.reg
	}
	fc.emit(vm.ABC(vm.GETUPVAL, r, idx, 0))
	if d.tail {
		fc.emit(vm.ABC(vm.RETURN, r, 0, 0))
		fc.releaseToMark(r)
	}
	return nil
}

func (fc *funcCompiler) compileUnary(u ast.UnaryExpr, d dest) error {
	mark := fc.nextReg
	dst := fc.destReg(d)

	opReg, err := fc.operandInto(u.Operand(), dst)
	if err != nil {
		return err
	}

	fc.pos = posOf(u)
	switch u.OpStr() {
	case "-":
		fc.emit(vm.ABC(vm.NEG, dst, opReg, 0))
	case "not":
		fc.emit(vm.ABC(vm.NOT, dst, opReg, 0))
	default:
		return fmt.Errorf("срез: унарный оператор %q", u.OpStr())
	}
	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

func (fc *funcCompiler) compileBinary(b ast.BinaryExpr, d dest) error {
	switch b.OpStr() {
	case "and":
		return fc.compileAndOr(b, true, d)
	case "or":
		return fc.compileAndOr(b, false, d)
	}

	mark := fc.nextReg
	dst := fc.destReg(d)

	leftReg, err := fc.operandInto(b.Left(), dst)
	if err != nil {
		return err
	}
	rightReg, err := fc.operand(b.Right())
	if err != nil {
		return err
	}

	fc.pos = posOf(b)
	op, ok := binOp(b.OpStr())
	if !ok {
		return fmt.Errorf("срез: оператор %q", b.OpStr())
	}
	fc.emit(vm.ABC(op, dst, leftReg, rightReg))
	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

func binOp(s string) (vm.OpCode, bool) {
	switch s {
	case "+":
		return vm.ADD, true
	case "-":
		return vm.SUB, true
	case "*":
		return vm.MUL, true
	case "/":
		return vm.DIV, true
	case "div":
		return vm.INTDIV, true
	case "rem":
		return vm.REM, true
	case "**":
		return vm.POW, true
	case "==":
		return vm.EQ, true
	case "!=":
		return vm.NEQ, true
	case "<":
		return vm.LT, true
	case ">":
		return vm.GT, true
	case "<=":
		return vm.LE, true
	case ">=":
		return vm.GE, true
	}
	return 0, false
}

// compileAndOr реализует K-2: JMPIF/JMPIFNOT прыгают только на Bool;
// значение операнда возвращается как есть (не приведение к Bool).
func (fc *funcCompiler) compileAndOr(b ast.BinaryExpr, isAnd bool, d dest) error {
	mark := fc.nextReg
	acc := fc.destReg(d)

	if err := fc.compileExpr(b.Left(), val(acc)); err != nil {
		return err
	}

	var jumpOp vm.OpCode
	if isAnd {
		jumpOp = vm.JMPIFNOT
	} else {
		jumpOp = vm.JMPIF
	}
	jEnd := fc.emitJump(jumpOp, acc)

	if err := fc.compileExpr(b.Right(), val(acc)); err != nil {
		return err
	}

	fc.patchHere(jEnd)
	fc.finish(d, acc)
	fc.releaseToMark(mark)
	return nil
}

func (fc *funcCompiler) compileIf(ie ast.IfExpr, d dest) error {
	pos := posOf(ie)
	mark := fc.nextReg

	cReg, err := fc.operand(ie.Cond())
	if err != nil {
		return err
	}

	fc.pos = pos
	jElse := fc.emitJump(vm.JMPIFNOT, cReg)
	fc.releaseToMark(mark)

	thenMark := fc.nextReg
	if err := fc.compileBranch(ie.ThenBody(), d); err != nil {
		return err
	}
	fc.releaseToMark(thenMark)

	jEnd := -1
	if !d.tail {
		jEnd = fc.emitJump(vm.JMP, 0)
	}

	fc.patchHere(jElse)

	elseMark := fc.nextReg
	if eb := ie.ElseBody(); eb != nil {
		if err := fc.compileBranch(eb, d); err != nil {
			return err
		}
	} else {
		if err := fc.loadUnit(d); err != nil {
			return err
		}
	}
	fc.releaseToMark(elseMark)

	if jEnd >= 0 {
		fc.patchHere(jEnd)
	}
	return nil
}

func (fc *funcCompiler) compileBranch(body ast.Expr, d dest) error {
	if blk, ok := body.(*ast.BlockStmt); ok {
		fc.pushScope()
		defer fc.popScope()
		return fc.compileStmts(blk.Body(), d)
	}
	return fc.compileExpr(body, d)
}

// ---- calls ----

func (fc *funcCompiler) compileCall(call ast.CallExpr, d dest) error {
	callee := call.Callee()

	// Модульный dispatch для Vec.*, Map.*, Str.*, Bytes.*, Json.*, Test.*.
	if me, ok := callee.(ast.MemberExpr); ok {
		if obj, ok := me.Obj().(ast.VariableExpr); ok {
			mod := obj.Name()
			if isPreludeModule(mod) {
				fullName := mod + "." + me.MemberName()
				return fc.compileGlobalCall(fullName, call.Args(), d, call)
			}
		}
	}

	// Специальные формы по имени.
	if v, ok := callee.(ast.VariableExpr); ok {
		name := v.Name()
		switch name {
		case "()":
			return fc.compileSeq(call.Args(), vm.TUPLE, d)
		case "[]":
			return fc.compileSeq(call.Args(), vm.LIST, d)
		case "%[]":
			return fc.compileSeq(call.Args(), vm.VECTOR, d)
		case "%{}":
			return fc.compileMap(call.Args(), d)
		case "spawn":
			return fc.compileSpawn(call.Args(), false, d)
		case "spawn_linked":
			return fc.compileSpawn(call.Args(), true, d)
		case "send":
			return fc.compileSend(call.Args(), d)
		case "self":
			return fc.compileSelf(d)
		case "make_ref":
			return fc.compileMakeRef(d)
		case "watch":
			return fc.compileWatch(call.Args(), d)
		case "unwatch":
			return fc.compileUnwatch(call.Args(), d)
		case "mailbox_size":
			return fc.compileMailboxSize(call.Args(), d)
		}
		if strings.HasSuffix(name, "{}") {
			return fc.compileRecord(strings.TrimSuffix(name, "{}"), call, d)
		}
	}

	return fc.compileGenericCall(call, d)
}

func (fc *funcCompiler) compileGenericCall(call ast.CallExpr, d dest) error {
	mark := fc.nextReg
	base := fc.allocReg()

	// Вызов локальной fn с захватом: захваты — хвост аргументов (T-51),
	// достаются по связыванию в точке объявления (T-39).
	var caps []upvalueInfo
	var owner *funcCompiler
	callee, isLocalFn := "", false
	if v, ok := call.Callee().(ast.VariableExpr); ok {
		callee, owner, isLocalFn = fc.resolveLocalFn(v.Name())
	}
	if isLocalFn {
		if lf := fc.compiler.lifted[callee]; lf != nil {
			caps = lf.caps
		}
		if err := fc.loadGlobal(val(base), callee); err != nil {
			return err
		}
	} else if err := fc.compileExpr(call.Callee(), val(base)); err != nil {
		return err
	}

	args := call.Args()
	spread := false
	if n := len(args); n > 0 {
		if u, ok := args[n-1].(ast.UnaryExpr); ok && u.OpStr() == ".." {
			spread = true
		}
	}
	for _, a := range args[:max(len(args)-1, 0)] {
		if u, ok := a.(ast.UnaryExpr); ok && u.OpStr() == ".." {
			return fmt.Errorf("срез: спред-аргумент не последний")
		}
	}
	// У variadic-fn захваты идут перед аргументами (см. liftedFn), у прочих — после.
	leadCaps := len(caps) > 0 && fc.compiler.lifted[callee].variadic
	if spread && len(caps) > 0 && !leadCaps {
		return fmt.Errorf("срез: спред-вызов локальной fn с захватом")
	}

	argc := len(args) + len(caps)
	next := 0
	loadCaps := func() {
		for _, uv := range caps {
			r := fc.allocReg()
			if r != base+1+next {
				fc.fail("call: arg %d in r%d, want r%d", next, r, base+1+next)
			}
			fc.loadCapture(owner, uv, r)
			next++
		}
	}
	if leadCaps {
		loadCaps()
	}
	for _, a := range args {
		r := fc.allocReg()
		if r != base+1+next {
			fc.fail("call: arg %d in r%d, want r%d", next, r, base+1+next)
		}
		if u, ok := a.(ast.UnaryExpr); ok && u.OpStr() == ".." {
			a = u.Operand()
		}
		if err := fc.compileExpr(a, val(r)); err != nil {
			return err
		}
		next++
	}
	if !leadCaps {
		loadCaps()
	}

	fc.pos = posOf(call)
	if d.tail && fc.trapDepth > 0 {
		fc.fail("TAILCALL under active trap (trapDepth=%d)", fc.trapDepth)
	}
	tailOp, callOp := vm.TAILCALL, vm.CALL
	if spread {
		tailOp, callOp = vm.TAILCALLSPREAD, vm.CALLSPREAD
	}
	if d.tail {
		fc.emit(vm.ABC(tailOp, base, argc, 0))
	} else {
		dst := d.reg
		if dst == -1 {
			dst = fc.allocReg()
		}
		fc.emit(vm.ABC(callOp, base, argc, dst))
	}
	fc.releaseToMark(mark)
	return nil
}

func (fc *funcCompiler) compileGlobalCall(name string, args []ast.Expr, d dest, pos ast.Node) error {
	mark := fc.nextReg
	base := fc.allocReg()
	gidx := fc.konst(runtime.Str(name))
	fc.emit(vm.ABx(vm.GETGLOBAL, base, gidx))

	argc := len(args)
	for i, a := range args {
		r := fc.allocReg()
		if r != base+1+i {
			fc.fail("call: arg %d in r%d, want r%d", i, r, base+1+i)
		}
		if err := fc.compileExpr(a, val(r)); err != nil {
			return err
		}
	}

	fc.pos = posOf(pos)
	if d.tail && fc.trapDepth > 0 {
		fc.fail("TAILCALL under active trap (trapDepth=%d)", fc.trapDepth)
	}
	if d.tail {
		fc.emit(vm.ABC(vm.TAILCALL, base, argc, 0))
	} else {
		dst := d.reg
		if dst == -1 {
			dst = fc.allocReg()
		}
		fc.emit(vm.ABC(vm.CALL, base, argc, dst))
	}
	fc.releaseToMark(mark)
	return nil
}

func isPreludeModule(name string) bool {
	switch name {
	case "Vec", "Map", "Str", "Bytes", "Json", "Test", "Sys", "Prelude":
		return true
	}
	return false
}

// ---- collections ----

// compileSeq: элементы в последовательные регистры от base (первый
// элемент задаёт base), затем ctor. Окно чтения ctor — base..base+n-1.
func (fc *funcCompiler) compileSeq(elems []ast.Expr, op vm.OpCode, d dest) error {
	mark := fc.nextReg
	dst := fc.destReg(d)

	base := 0
	for i, e := range elems {
		r := fc.allocReg()
		if i == 0 {
			base = r
		}
		if r != base+i {
			fc.fail("seq: elem %d in r%d, want r%d", i, r, base+i)
		}
		if err := fc.compileExpr(e, val(r)); err != nil {
			return err
		}
	}
	// Пустая коллекция: base не читается (C=0), значение не важно.
	fc.emit(vm.ABC(op, dst, base, len(elems)))
	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

// compileMap: пары k,v в последовательных регистрах от base (первая
// пара задаёт base), затем MAP. Окно чтения — base..base+2n-1.
func (fc *funcCompiler) compileMap(elems []ast.Expr, d dest) error {
	mark := fc.nextReg
	dst := fc.destReg(d)

	base := 0
	n := 0
	for _, e := range elems {
		pair, ok := e.(ast.BinaryExpr)
		if !ok || pair.OpStr() != "=>" {
			return fmt.Errorf("срез: элемент мапы должен быть парой =>")
		}
		if n == 0 {
			base = fc.nextReg
		}
		kReg := fc.allocReg()
		if err := fc.compileExpr(pair.Left(), val(kReg)); err != nil {
			return err
		}
		vReg := fc.allocReg()
		if err := fc.compileExpr(pair.Right(), val(vReg)); err != nil {
			return err
		}
		n++
	}
	fc.emit(vm.ABC(vm.MAP, dst, base, n))
	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

// ---- records (T-73, §4.7) ----

// recordSlot — элемент литерала записи: поле `name: val` или спред
// `..val` (name == "..").
type recordSlot struct {
	name string
	val  ast.Expr
}

// compileRecord — литерал записи `Type{ f: e, ..r }` (typ != "") или
// анонимный `{ ... }`. Неизвестный тип, поле вне декларации и повторное
// явное поле — ошибка компиляции с позицией. Форма записи (тип,
// объявленные поля, слоты) — константа в R[base], значения слотов — в
// R[base+1..]; RECORD собирает запись.
func (fc *funcCompiler) compileRecord(typ string, call ast.CallExpr, d dest) error {
	var declared []string
	if typ != "" {
		fields, ok := fc.compiler.records[typ]
		if !ok {
			p := posOf(call.Callee())
			return fmt.Errorf("%d:%d: неизвестный тип записи %s", p.Line, p.Col, typ)
		}
		declared = fields
	}

	slots := make([]recordSlot, 0, len(call.Args()))
	seen := make(map[string]bool)
	for _, a := range call.Args() {
		if u, ok := a.(ast.UnaryExpr); ok && u.OpStr() == ".." {
			slots = append(slots, recordSlot{"..", u.Operand()})
			continue
		}
		b, ok := a.(ast.BinaryExpr)
		if !ok || b.OpStr() != ":" {
			return fmt.Errorf("internal: элемент литерала записи %T", a)
		}
		fv, ok := b.Left().(ast.VariableExpr)
		if !ok {
			return fmt.Errorf("internal: имя поля записи %T", b.Left())
		}
		name, p := fv.Name(), posOf(fv)
		if typ != "" && !slices.Contains(declared, name) {
			return fmt.Errorf("%d:%d: у типа %s нет поля %s", p.Line, p.Col, typ, name)
		}
		if seen[name] {
			return fmt.Errorf("%d:%d: повторное поле %s в литерале записи", p.Line, p.Col, name)
		}
		seen[name] = true
		slots = append(slots, recordSlot{name, b.Right()})
	}

	decl := make([]runtime.Value, len(declared))
	for i, n := range declared {
		decl[i] = runtime.Str(n)
	}
	names := make([]runtime.Value, len(slots))
	for i, s := range slots {
		names[i] = runtime.Str(s.name)
	}
	shape := runtime.Tuple(runtime.Str(typ), runtime.Tuple(decl...), runtime.Tuple(names...))

	mark := fc.nextReg
	dst := fc.destReg(d)
	base := fc.allocReg()
	fc.emit(vm.ABx(vm.LOADK, base, fc.konst(shape)))
	for i, s := range slots {
		r := fc.allocReg()
		if r != base+1+i {
			fc.fail("record: slot %d in r%d, want r%d", i, r, base+1+i)
		}
		if err := fc.compileExpr(s.val, val(r)); err != nil {
			return err
		}
	}
	fc.emit(vm.ABC(vm.RECORD, dst, base, len(slots)))
	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

// compileMember — доступ к полю записи `obj.field` (§4.7). `Mod.name`
// модуля прелюдии вне позиции вызова не поддерживается.
func (fc *funcCompiler) compileMember(me ast.MemberExpr, d dest) error {
	if obj, ok := me.Obj().(ast.VariableExpr); ok && isPreludeModule(obj.Name()) {
		return fmt.Errorf("срез: неподдерживаемое выражение %s.%s", obj.Name(), me.MemberName())
	}
	mark := fc.nextReg
	dst := fc.destReg(d)
	objReg, err := fc.operandInto(me.Obj(), fc.allocReg())
	if err != nil {
		return err
	}
	nameReg := fc.allocReg()
	fc.emit(vm.ABx(vm.LOADK, nameReg, fc.konst(runtime.Str(me.MemberName()))))
	fc.emit(vm.ABC(vm.GETFIELD, dst, objReg, nameReg))
	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

// ---- actor ops ----

func (fc *funcCompiler) compileSpawn(args []ast.Expr, linked bool, d dest) error {
	if len(args) != 1 {
		return fmt.Errorf("spawn требует 1 аргумент (fn)")
	}
	mark := fc.nextReg
	dst := fc.destReg(d)

	fnReg := fc.allocReg()
	if err := fc.compileExpr(args[0], val(fnReg)); err != nil {
		return err
	}

	c := 0
	if linked {
		c = 1
	}
	fc.emit(vm.ABC(vm.SPAWN, dst, fnReg, c))
	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

func (fc *funcCompiler) compileSend(args []ast.Expr, d dest) error {
	if len(args) != 2 {
		return fmt.Errorf("send требует 2 аргумента (pid, msg)")
	}
	mark := fc.nextReg
	dst := fc.destReg(d)

	pidReg := fc.allocReg()
	if err := fc.compileExpr(args[0], val(pidReg)); err != nil {
		return err
	}
	msgReg := fc.allocReg()
	if err := fc.compileExpr(args[1], val(msgReg)); err != nil {
		return err
	}
	fc.emit(vm.ABC(vm.SEND, dst, pidReg, msgReg))
	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

func (fc *funcCompiler) compileSelf(d dest) error {
	mark := fc.nextReg
	dst := fc.destReg(d)
	fc.emit(vm.ABC(vm.SELF, dst, 0, 0))
	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

func (fc *funcCompiler) compileMakeRef(d dest) error {
	mark := fc.nextReg
	dst := fc.destReg(d)
	fc.emit(vm.ABC(vm.MAKEREF, dst, 0, 0))
	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

func (fc *funcCompiler) compileWatch(args []ast.Expr, d dest) error {
	if len(args) != 1 {
		return fmt.Errorf("watch требует 1 аргумент (pid)")
	}
	mark := fc.nextReg
	dst := fc.destReg(d)
	pidReg := fc.allocReg()
	if err := fc.compileExpr(args[0], val(pidReg)); err != nil {
		return err
	}
	fc.emit(vm.ABC(vm.WATCH, dst, pidReg, 0))
	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

func (fc *funcCompiler) compileUnwatch(args []ast.Expr, d dest) error {
	if len(args) != 1 {
		return fmt.Errorf("unwatch требует 1 аргумент (ref)")
	}
	mark := fc.nextReg
	dst := fc.destReg(d)
	refReg := fc.allocReg()
	if err := fc.compileExpr(args[0], val(refReg)); err != nil {
		return err
	}
	fc.emit(vm.ABC(vm.UNWATCH, dst, refReg, 0))
	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

func (fc *funcCompiler) compileMailboxSize(args []ast.Expr, d dest) error {
	if len(args) != 1 {
		return fmt.Errorf("mailbox_size требует 1 аргумент (pid)")
	}
	mark := fc.nextReg
	dst := fc.destReg(d)
	pidReg := fc.allocReg()
	if err := fc.compileExpr(args[0], val(pidReg)); err != nil {
		return err
	}
	fc.emit(vm.ABC(vm.MAILBOXSIZE, dst, pidReg, 0))
	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

// ---- trap / ensure (§5) ----

func (fc *funcCompiler) compileTrap(te ast.TrapExpr, d dest) error {
	if inline := te.TrapInline(); inline != nil {
		return fc.compileInlineTrap(inline, d)
	}
	return fc.compileBlockTrap(te, d)
}

func (fc *funcCompiler) compileInlineTrap(inner ast.Expr, d dest) error {
	mark := fc.nextReg
	dst := fc.destReg(d)

	fc.trapDepth++
	begin := fc.emitJump(vm.TRAPBEGIN, dst)

	if err := fc.compileExpr(inner, val(dst)); err != nil {
		return err
	}

	fc.emit(vm.ABC(vm.TRAPEND, 0, 0, 0))
	fc.trapDepth--
	fc.emit(vm.ABC(vm.MAKEOK, dst, dst, 0))
	jEnd := fc.emitJump(vm.JMP, 0)

	fc.patchHere(begin)
	fc.emit(vm.ABC(vm.MAKEERROR, dst, dst, 0))
	fc.patchHere(jEnd)

	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

func (fc *funcCompiler) compileBlockTrap(te ast.TrapExpr, d dest) error {
	var stmts []ast.Stmt
	switch body := te.TrapBody().(type) {
	case *ast.BlockStmt:
		stmts = body.Body()
	case nil:
	default:
		stmts = []ast.Stmt{body}
	}
	ensures := te.TrapEnsures()

	if len(ensures) == 0 {
		return fc.compileTrapNoEnsure(stmts, d)
	}
	return fc.compileTrapWithEnsure(stmts, ensures, d)
}

func (fc *funcCompiler) compileTrapNoEnsure(stmts []ast.Stmt, d dest) error {
	mark := fc.nextReg
	dst := fc.destReg(d)

	fc.trapDepth++
	begin := fc.emitJump(vm.TRAPBEGIN, dst)

	fc.pushScope()
	if err := fc.compileStmts(stmts, val(dst)); err != nil {
		fc.popScope()
		return err
	}
	fc.popScope()

	fc.emit(vm.ABC(vm.TRAPEND, 0, 0, 0))
	fc.trapDepth--
	fc.emit(vm.ABC(vm.MAKEOK, dst, dst, 0))
	jEnd := fc.emitJump(vm.JMP, 0)

	fc.patchHere(begin)
	fc.emit(vm.ABC(vm.MAKEERROR, dst, dst, 0))
	fc.patchHere(jEnd)

	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

// compileTrapWithEnsure — схема §5 с флаг-регистром (D-5) и
// per-ensure флагами регистрации (I-F5 / T-37).
//
// Ensure компилируются в области тела (до popScope) и исполняются
// только если до их текстовой точки дошли (LOADK true → JMPIFNOT).
func (fc *funcCompiler) compileTrapWithEnsure(stmts []ast.Stmt, ensures []ast.Expr, d dest) error {
	mark := fc.nextReg
	dst := fc.destReg(d)
	eReg := fc.allocReg()
	fReg := fc.allocReg()

	falseIdx := fc.konst(runtime.Bool(false))
	trueIdx := fc.konst(runtime.Bool(true))

	// Преинициализация dst: тело пишет dst на body-пути, но handler-путь
	// (BH) заходит в ENS без записи dst. Линейный def-assignment в
	// vm.Verify видит пересечение состояний → dst "undefined" в
	// MAKEOK. Значение на error-пути всё равно перезаписывается
	// MAKEERROR, так что семантика не меняется.
	unitIdx := fc.konst(runtime.Unit)
	fc.emit(vm.ABx(vm.LOADK, dst, unitIdx))

	fc.emit(vm.ABx(vm.LOADK, eReg, unitIdx))

	fc.emit(vm.ABx(vm.LOADK, fReg, falseIdx))

	// Флаги регистрации ensure: false до тела (Verify видит def на
	// каждом пути), true — в текстовой точке внутри тела.
	regFlags := make([]int, len(ensures))
	for i := range ensures {
		regFlags[i] = fc.allocReg()
		fc.emit(vm.ABx(vm.LOADK, regFlags[i], falseIdx))
	}

	// Локали тела, которые ensure может прочитать после join с BH:
	// Verify считает живыми на handler-пути только регистры до
	// TRAPBEGIN (как dst выше). Предаллоцируем слоты let-связей.
	letRegs := make(map[string]int)
	for _, s := range stmts {
		lb, ok := s.(ast.LetBind)
		if !ok {
			continue
		}
		ip, ok := lb.Pat().(ast.IdentPattern)
		if !ok {
			continue
		}
		r := fc.allocReg()
		fc.emit(vm.ABx(vm.LOADK, r, unitIdx))
		letRegs[ip.IdentName()] = r
	}

	fc.trapDepth++
	bodyBegin := fc.emitJump(vm.TRAPBEGIN, eReg)

	fc.pushScope()
	if err := fc.compileTrapBodyWithEnsures(stmts, ensures, regFlags, letRegs, trueIdx, val(dst)); err != nil {
		fc.popScope()
		return err
	}

	fc.emit(vm.ABC(vm.TRAPEND, 0, 0, 0))
	fc.trapDepth--
	jEns := fc.emitJump(vm.JMP, 0)

	fc.patchHere(bodyBegin)
	fc.emit(vm.ABx(vm.LOADK, fReg, trueIdx))

	fc.patchHere(jEns)

	// ENS: LIFO; каждый ensure — только если зарегистрирован.
	for i := len(ensures) - 1; i >= 0; i-- {
		ens := ensures[i]

		jSkip := fc.emitJump(vm.JMPIFNOT, regFlags[i])

		ensMark := fc.nextReg
		fc.trapDepth++
		ehBegin := fc.emitJump(vm.TRAPBEGIN, eReg)

		sReg := fc.allocReg()
		if err := fc.compileExpr(ens, val(sReg)); err != nil {
			fc.popScope()
			return err
		}
		fc.emit(vm.ABC(vm.TRAPEND, 0, 0, 0))
		fc.trapDepth--
		fc.releaseToMark(ensMark)

		jNext := fc.emitJump(vm.JMP, 0)

		fc.patchHere(ehBegin)
		fc.emit(vm.ABx(vm.LOADK, fReg, trueIdx))

		fc.patchHere(jNext)
		fc.patchHere(jSkip)
	}

	fc.popScope()

	jErr := fc.emitJump(vm.JMPIF, fReg)

	fc.emit(vm.ABC(vm.MAKEOK, dst, dst, 0))
	jEnd := fc.emitJump(vm.JMP, 0)

	fc.patchHere(jErr)
	fc.emit(vm.ABC(vm.MAKEERROR, dst, eReg, 0))

	fc.patchHere(jEnd)
	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

// compileTrapBodyWithEnsures обходит стейтменты тела и точки регистрации
// ensure в текстовом порядке (Line, Col). Ensure-выражения здесь не
// компилируются — только LOADK true в их флаг-регистр.
func (fc *funcCompiler) compileTrapBodyWithEnsures(
	stmts []ast.Stmt, ensures []ast.Expr, regFlags []int, letRegs map[string]int, trueIdx int, d dest,
) error {
	if len(stmts) == 0 {
		for i := range ensures {
			fc.emit(vm.ABx(vm.LOADK, regFlags[i], trueIdx))
		}
		return fc.loadUnit(d)
	}

	if err := fc.declareLocalFns(stmts); err != nil {
		return err
	}

	type item struct {
		isEnsure bool
		idx      int
	}
	merged := make([]item, 0, len(stmts)+len(ensures))
	si, ei := 0, 0
	for si < len(stmts) || ei < len(ensures) {
		switch {
		case ei >= len(ensures):
			merged = append(merged, item{false, si})
			si++
		case si >= len(stmts):
			merged = append(merged, item{true, ei})
			ei++
		default:
			sLine, sCol := stmts[si].Pos(), stmts[si].End()
			eLine, eCol := ensures[ei].Pos(), ensures[ei].End()
			if eLine < sLine || (eLine == sLine && eCol < sCol) {
				merged = append(merged, item{true, ei})
				ei++
			} else {
				merged = append(merged, item{false, si})
				si++
			}
		}
	}

	lastStmt := len(stmts) - 1
	for _, it := range merged {
		if it.isEnsure {
			fc.emit(vm.ABx(vm.LOADK, regFlags[it.idx], trueIdx))
			continue
		}
		sd := discard
		if it.idx == lastStmt {
			sd = d
		}
		s := stmts[it.idx]
		if lb, ok := s.(ast.LetBind); ok {
			if err := fc.compileTrapLetBind(lb, sd, letRegs); err != nil {
				return err
			}
			continue
		}
		if err := fc.compileStmt(s, sd); err != nil {
			return err
		}
	}
	return nil
}

// compileTrapLetBind — как compileLetBind, но пишет в предаллоцированный
// слот (Verify: жив на BH-пути) при наличии в letRegs.
func (fc *funcCompiler) compileTrapLetBind(st ast.LetBind, d dest, letRegs map[string]int) error {
	ip, ok := st.Pat().(ast.IdentPattern)
	if !ok {
		return fmt.Errorf("срез: только простые связывания `name = expr`")
	}
	name := ip.IdentName()
	r, ok := letRegs[name]
	if ok {
		delete(letRegs, name)
	} else {
		r = fc.allocReg()
	}
	if err := fc.compileExpr(st.Val(), val(r)); err != nil {
		return err
	}
	fc.bindLocal(name, r)
	return fc.loadUnit(d)
}

// ---- match (§8.3) ----

// compileMatch: субъект в регистр, ветки — compileCaseBranches.
func (fc *funcCompiler) compileMatch(me ast.MatchExpr, d dest) error {
	mark := fc.nextReg

	sReg, err := fc.operand(me.MatchSubject())
	if err != nil {
		return err
	}
	if err := fc.compileCaseBranches(sReg, me.MatchBranches(), posOf(me), d); err != nil {
		return err
	}
	fc.releaseToMark(mark)
	return nil
}

// compileCaseBranches: ветки по порядку над R[sReg] — MATCHLOCAL+JMP к
// следующей ветке (схема веток recv). Ветки наследуют d (d.tail → хвостовые).
// Ни одна ветка не подошла — raise (:case_clause, val) с позицией pos (§10.4).
// Общий код match (§8.3) и with/else (§8.2).
func (fc *funcCompiler) compileCaseBranches(sReg int, branches []ast.MatchBranchArg, pos vm.SrcPos, d dest) error {
	mark := fc.nextReg

	var endJumps []int
	for _, br := range branches {
		brMark := fc.nextReg
		fc.pushScope()

		cp, err := fc.compilePattern(br.Pattern)
		if err != nil {
			fc.popScope()
			return err
		}
		patIdx := fc.chunk.AddPattern(cp)

		fc.pos = posOf(br.Pattern)
		fc.emit(vm.ABx(vm.MATCHLOCAL, sReg, patIdx))
		jFail := fc.emitJump(vm.JMP, 0)

		if err := fc.compileBranch(br.Body, d); err != nil {
			fc.popScope()
			return err
		}
		if !d.tail {
			endJumps = append(endJumps, fc.emitJump(vm.JMP, 0))
		}

		fc.patchHere(jFail)
		fc.popScope()
		fc.releaseToMark(brMark)
	}

	fc.pos = pos
	w0 := fc.allocReg()
	w1 := fc.allocReg()
	fc.emit(vm.ABx(vm.LOADK, w0, fc.konst(runtime.Atom("case_clause"))))
	fc.emit(vm.ABC(vm.MOVE, w1, sReg, 0))
	fc.emit(vm.ABC(vm.TUPLE, w0, w0, 2))
	fc.emit(vm.ABC(vm.RAISE, w0, 0, 0))

	for _, j := range endJumps {
		fc.patchHere(j)
	}
	fc.releaseToMark(mark)
	return nil
}

// ---- with/else (§8.2) ----

// compileWith: binds по порядку — значение в vReg, MATCHLOCAL+JMP на метку
// сбоя; затем тело (наследует d). На метке сбоя в vReg — первое несовпавшее
// значение: без else оно и есть результат (пропагация, не raise), с else —
// ветки compileCaseBranches, без совпадения — (:case_clause, val) (§10.4).
// Связывания binds видны binds ниже и телу, но не веткам else.
func (fc *funcCompiler) compileWith(we ast.WithExpr, d dest) error {
	mark := fc.nextReg
	vReg := fc.allocReg()

	fc.pushScope()
	var fails []int
	for _, it := range we.WithItems() {
		if err := fc.compileExpr(it.Expr, val(vReg)); err != nil {
			fc.popScope()
			return err
		}
		cp, err := fc.compilePattern(it.Pattern)
		if err != nil {
			fc.popScope()
			return err
		}
		patIdx := fc.chunk.AddPattern(cp)

		fc.pos = posOf(it.Pattern)
		fc.emit(vm.ABx(vm.MATCHLOCAL, vReg, patIdx))
		fails = append(fails, fc.emitJump(vm.JMP, 0))
	}

	var body ast.Expr = we.WithBody()
	if we.WithBody() == nil {
		body = ast.NewBlockStmt(nil, 0, 0)
	}
	if err := fc.compileBranch(body, d); err != nil {
		fc.popScope()
		return err
	}
	jEnd := -1
	if !d.tail {
		jEnd = fc.emitJump(vm.JMP, 0)
	}
	fc.popScope()
	fc.releaseToMark(vReg + 1)

	for _, j := range fails {
		fc.patchHere(j)
	}
	fc.pos = posOf(we)
	if elseBranches := we.WithElseBranches(); len(elseBranches) > 0 {
		branches := make([]ast.MatchBranchArg, 0, len(elseBranches))
		for _, eb := range elseBranches {
			branches = append(branches, ast.MatchBranchArg(eb))
		}
		if err := fc.compileCaseBranches(vReg, branches, posOf(we), d); err != nil {
			return err
		}
	} else {
		fc.finish(d, vReg)
	}

	if jEnd >= 0 {
		fc.patchHere(jEnd)
	}
	fc.releaseToMark(mark)
	return nil
}

// ---- recv (§7) ----

func (fc *funcCompiler) compileRecv(re ast.RecvExpr, d dest) error {
	mark := fc.nextReg
	mReg := fc.allocReg()

	afterTime := re.RecvAfterTime()
	afterBody := re.RecvAfterBody()
	hasAfter := afterBody != nil

	if hasAfter && afterTime != nil {
		tMark := fc.nextReg
		tReg := fc.allocReg()
		if err := fc.compileExpr(afterTime, val(tReg)); err != nil {
			return err
		}
		fc.emit(vm.ABC(vm.RECVTIMER, tReg, 0, 0))
		fc.releaseToMark(tMark)
	}

	recvTakeAt := fc.emit(vm.AsBx(vm.RECVTAKE, mReg, 0))

	var endJumps []int
	for _, br := range re.RecvBranches() {
		fc.pushScope()

		cp, err := fc.compilePattern(br.Pattern)
		if err != nil {
			fc.popScope()
			return err
		}
		patIdx := fc.chunk.AddPattern(cp)

		fc.emit(vm.ABx(vm.MATCHLOCAL, mReg, patIdx))
		fails := []int{fc.emitJump(vm.JMP, 0)}

		// Guard видит связывания паттерна; ложный — к следующей ветке (§12.4).
		if br.Guard != nil {
			gMark := fc.nextReg
			g := fc.allocReg()
			if err := fc.compileExpr(br.Guard, val(g)); err != nil {
				fc.popScope()
				return err
			}
			fc.pos = posOf(br.Guard)
			fails = append(fails, fc.emitJump(vm.JMPIFNOT, g))
			fc.releaseToMark(gMark)
		}

		if err := fc.compileBranch(br.Body, d); err != nil {
			fc.popScope()
			return err
		}

		jEnd := -1
		if !d.tail {
			jEnd = fc.emitJump(vm.JMP, 0)
		}

		for _, j := range fails {
			fc.patchHere(j)
		}
		if jEnd >= 0 {
			endJumps = append(endJumps, jEnd)
		}
		fc.popScope()
	}

	if elseBody := re.RecvElseBody(); elseBody != nil {
		fc.pushScope()
		if elseName := re.RecvElseName(); elseName != "" {
			// D-3: алиас имени else на регистр сообщения.
			fc.bindLocal(elseName, mReg)
		}
		if err := fc.compileBranch(elseBody, d); err != nil {
			fc.popScope()
			return err
		}
		if !d.tail {
			jEnd := fc.emitJump(vm.JMP, 0)
			endJumps = append(endJumps, jEnd)
		}
		fc.popScope()
	} else {
		// :recv_clause, msg → raise
		rclauseIdx := fc.konst(runtime.Atom("recv_clause"))
		w0 := fc.allocReg()
		w1 := fc.allocReg()
		fc.emit(vm.ABx(vm.LOADK, w0, rclauseIdx))
		fc.emit(vm.ABC(vm.MOVE, w1, mReg, 0))
		fc.emit(vm.ABC(vm.TUPLE, w0, w0, 2))
		fc.emit(vm.ABC(vm.RAISE, w0, 0, 0))
	}

	afterAt := len(fc.chunk.Code)
	if hasAfter {
		if err := fc.compileBranch(afterBody, d); err != nil {
			return err
		}
		if !d.tail {
			jEnd := fc.emitJump(vm.JMP, 0)
			endJumps = append(endJumps, jEnd)
		}
	}

	if hasAfter {
		sbx := afterAt - (recvTakeAt + 1)
		fc.chunk.Code[recvTakeAt] = vm.AsBx(vm.RECVTAKE, mReg, sbx)
	}

	endAt := len(fc.chunk.Code)
	for _, j := range endJumps {
		if err := fc.chunk.PatchJump(j, endAt); err != nil {
			fc.fail("patch: %v", err)
		}
	}

	fc.releaseToMark(mark)
	return nil
}

// ---- range / index ----

func (fc *funcCompiler) compileRange(re ast.RangeExpr, d dest) error {
	mark := fc.nextReg
	dst := fc.destReg(d)

	startReg, err := fc.operandInto(re.RangeStart(), dst)
	if err != nil {
		return err
	}
	endReg, err := fc.operand(re.RangeEnd())
	if err != nil {
		return err
	}

	fc.pos = posOf(re)
	fc.emit(vm.ABC(vm.RANGE, dst, startReg, endReg))
	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

func (fc *funcCompiler) compileIndex(ie ast.IndexExpr, d dest) error {
	mark := fc.nextReg
	dst := fc.destReg(d)

	objReg, err := fc.operandInto(ie.Obj(), dst)
	if err != nil {
		return err
	}
	idxReg, err := fc.operand(ie.Index())
	if err != nil {
		return err
	}

	fc.pos = posOf(ie)
	fc.emit(vm.ABC(vm.INDEX, dst, objReg, idxReg))
	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

// ---- lambda / closure ----

func (fc *funcCompiler) compileLambda(name string, params []string, body ast.Expr, d dest) error {
	if err := checkLambdaParams(params); err != nil {
		return err
	}
	child := fc.compiler.newFuncCompiler(fc)
	// Уникальный префикс на лямбду: иначе одноимённые локальные fn в
	// разных лямбдах одной функции перезаписывают друг друга в image.Functions (A-F6).
	child.prefix = fmt.Sprintf("%slambda$%d$", fc.prefix, fc.lambdaSeq)
	fc.lambdaSeq++

	child.chunk.NumParams = len(params)
	for i, p := range params {
		r := child.allocReg()
		if r != i {
			fc.fail("lambda: param %d in r%d", i, r)
		}
		if name, ok := strings.CutPrefix(p, ".."); ok {
			child.chunk.Variadic = true
			p = name
		}
		child.bindLocal(p, i)
	}

	if blk, ok := body.(*ast.BlockStmt); ok {
		stmts := blk.Body()
		if len(stmts) == 0 {
			scratch := child.allocReg()
			if err := child.loadUnit(dest{reg: scratch, tail: true}); err != nil {
				return err
			}
		} else {
			scratch := child.allocReg()
			if err := child.compileStmts(stmts, dest{reg: scratch, tail: true}); err != nil {
				return err
			}
		}
	} else {
		scratch := child.allocReg()
		if err := child.compileExpr(body, dest{reg: scratch, tail: true}); err != nil {
			return err
		}
	}
	child.chunk.NumRegs = child.maxReg

	fnName := name
	if fnName == "" {
		fnName = child.prefix
	}
	arity := len(params)
	if child.chunk.Variadic {
		arity = -1
	}
	fn := &vm.Function{Name: fnName, Arity: arity, Chunk: child.chunk}

	fnVal := vm.FuncValue(fn)
	fnIdx := fc.chunk.AddConstant(fnVal)

	mark := fc.nextReg
	dst := fc.destReg(d)

	base := fc.allocReg()
	fc.emit(vm.ABx(vm.LOADK, base, fnIdx))

	for j, uv := range child.upvalues {
		r := fc.allocReg()
		if r != base+1+j {
			fc.fail("closure: upvalue %d in r%d, want r%d", j, r, base+1+j)
		}
		if uv.isLocal {
			fc.emit(vm.ABC(vm.MOVE, r, uv.index, 0))
		} else {
			fc.emit(vm.ABC(vm.GETUPVAL, r, uv.index, 0))
		}
	}

	fc.emit(vm.ABC(vm.MAKECLOSURE, dst, base, len(child.upvalues)))
	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

// ---- patterns ----

func (fc *funcCompiler) compilePattern(pat ast.Pattern) (*vm.CompiledPattern, error) {
	switch p := pat.(type) {
	case ast.IdentPattern:
		slot := fc.allocReg()
		fc.bindLocal(p.IdentName(), slot)
		return &vm.CompiledPattern{Kind: vm.PatIdent, Slot: slot}, nil

	case ast.LiteralPattern:
		lit, err := parseLiteralValue(p.ValueStr())
		if err != nil {
			return nil, err
		}
		return &vm.CompiledPattern{Kind: vm.PatLiteral, Lit: lit}, nil

	case ast.PatternCtor:
		subs := make([]*vm.CompiledPattern, 0, len(p.CtorArgs()))
		for _, a := range p.CtorArgs() {
			sub, err := fc.compilePattern(a)
			if err != nil {
				return nil, err
			}
			subs = append(subs, sub)
		}
		return &vm.CompiledPattern{Kind: vm.PatCtor, Tag: p.CtorName(), Subs: subs}, nil

	case ast.PatternTuple:
		subs := make([]*vm.CompiledPattern, 0, len(p.TupleElems()))
		for _, a := range p.TupleElems() {
			sub, err := fc.compilePattern(a)
			if err != nil {
				return nil, err
			}
			subs = append(subs, sub)
		}
		return &vm.CompiledPattern{Kind: vm.PatTuple, Subs: subs}, nil

	case ast.PatternList:
		subs := make([]*vm.CompiledPattern, 0, len(p.ListElems()))
		for _, a := range p.ListElems() {
			sub, err := fc.compilePattern(a)
			if err != nil {
				return nil, err
			}
			subs = append(subs, sub)
		}
		restSlot := -1
		if p.ListHasRest() && p.ListRestName() != "" {
			restSlot = fc.allocReg()
			fc.bindLocal(p.ListRestName(), restSlot)
		}
		return &vm.CompiledPattern{
			Kind:     vm.PatList,
			Subs:     subs,
			HasRest:  p.ListHasRest(),
			RestSlot: restSlot,
		}, nil

	case ast.PatternMapAccessor:
		pairs := make([]vm.MapPatPair, 0, len(p.MapPairsAccessor()))
		for _, pair := range p.MapPairsAccessor() {
			key, err := fc.compileConstExpr(pair.Key)
			if err != nil {
				return nil, err
			}
			sub, err := fc.compilePattern(pair.Pat)
			if err != nil {
				return nil, err
			}
			pairs = append(pairs, vm.MapPatPair{Key: key, Value: sub})
		}
		return &vm.CompiledPattern{Kind: vm.PatMap, Pairs: pairs}, nil

	case ast.PatternAs:
		inner, err := fc.compilePattern(p.AsInner())
		if err != nil {
			return nil, err
		}
		slot := fc.allocReg()
		fc.bindLocal(p.AsName(), slot)
		return &vm.CompiledPattern{
			Kind: vm.PatAs, Inner: inner, AsSlot: slot,
		}, nil

	case ast.PatternWildcard:
		if pat.String() == "_" {
			return &vm.CompiledPattern{Kind: vm.PatWildcard}, nil
		}
		return nil, fmt.Errorf("срез: неподдерживаемый паттерн %T %q", pat, pat.String())
	}
	return nil, fmt.Errorf("срез: неподдерживаемый паттерн %T", pat)
}

func (fc *funcCompiler) compileConstExpr(e ast.Expr) (runtime.Value, error) {
	switch x := e.(type) {
	case ast.LiteralExpr:
		return parseLiteralValue(x.ValueStr())
	case ast.AtomExpr:
		return runtime.Atom(x.AtomName()), nil
	case ast.GroupingExpr:
		return fc.compileConstExpr(x.Inner())
	}
	return runtime.Unit, fmt.Errorf("map-паттерн: ключ должен быть литералом, got %T", e)
}

// ---- literals ----

func parseLiteralValue(s string) (runtime.Value, error) {
	switch s {
	case "()":
		return runtime.Unit, nil
	case "true":
		return runtime.Bool(true), nil
	case "false":
		return runtime.Bool(false), nil
	}
	if s == "" {
		return runtime.Unit, fmt.Errorf("пустой литерал")
	}
	if s[0] == ':' {
		return runtime.Atom(s[1:]), nil
	}
	if s[0] == '"' && s[len(s)-1] == '"' {
		return runtime.Str(decodeStrBody(s[1 : len(s)-1])), nil
	}
	if strings.HasPrefix(s, `dec"`) && strings.HasSuffix(s, `"`) {
		r, err := runtime.ParseDecimal(s[4 : len(s)-1])
		if err != nil {
			return runtime.Unit, err
		}
		return runtime.Decimal(r), nil
	}
	// Int: base by prefix (0x/0b/0o → 16/2/8, else 10). Arbitrary precision (§3.1, S-F7).
	if v, ok := parseIntLiteral(s); ok {
		return v, nil
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return runtime.Float(f), nil
	}
	return runtime.Unit, fmt.Errorf("неизвестный литерал %q", s)
}

// parseIntLiteral разбирает целочисленный литерал по префиксу основания.
// Underscores допускаются (лексер уже проверил позиции). Не-int строки → ok=false.
func parseIntLiteral(s string) (runtime.Value, bool) {
	clean := strings.ReplaceAll(s, "_", "")
	if clean == "" {
		return runtime.Unit, false
	}
	base := 10
	body := clean
	neg := false
	if body[0] == '+' || body[0] == '-' {
		neg = body[0] == '-'
		body = body[1:]
		if body == "" {
			return runtime.Unit, false
		}
	}
	if len(body) >= 2 && body[0] == '0' {
		switch body[1] {
		case 'x', 'X':
			base = 16
			body = body[2:]
		case 'b', 'B':
			base = 2
			body = body[2:]
		case 'o', 'O':
			base = 8
			body = body[2:]
		}
	}
	if body == "" {
		return runtime.Unit, false
	}
	n := new(big.Int)
	if _, ok := n.SetString(body, base); !ok {
		return runtime.Unit, false
	}
	if neg {
		n.Neg(n)
	}
	return runtime.IntBig(n), true
}

func decodeStrBody(s string) string {
	var sb strings.Builder
	for i := 0; i < len(s); {
		c := s[i]
		if c != '\\' || i+1 >= len(s) {
			sb.WriteByte(c)
			i++
			continue
		}
		esc := s[i+1]
		switch esc {
		case 'n':
			sb.WriteByte('\n')
			i += 2
		case 't':
			sb.WriteByte('\t')
			i += 2
		case 'r':
			sb.WriteByte('\r')
			i += 2
		case '0':
			sb.WriteByte(0)
			i += 2
		case '\\':
			sb.WriteByte('\\')
			i += 2
		case '"':
			sb.WriteByte('"')
			i += 2
		case 'u':
			if i+2 >= len(s) || s[i+2] != '{' {
				sb.WriteByte(c)
				i++
				continue
			}
			j := i + 3
			for j < len(s) && s[j] != '}' {
				j++
			}
			if j >= len(s) {
				sb.WriteByte(c)
				i++
				continue
			}
			var cp uint64
			if _, err := fmt.Sscanf(s[i+3:j], "%x", &cp); err == nil && cp <= 0x10FFFF {
				sb.WriteRune(rune(cp))
			}
			i = j + 1
		case '(':
			// Интерполяция — оставляем как есть.
			sb.WriteByte(c)
			sb.WriteByte(esc)
			i += 2
		default:
			sb.WriteByte(c)
			i++
		}
	}
	return sb.String()
}

func decodeBytesBody(s string) ([]byte, error) {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); {
		c := s[i]
		if c != '\\' {
			out = append(out, c)
			i++
			continue
		}
		if i+1 >= len(s) {
			return nil, fmt.Errorf("trailing backslash")
		}
		esc := s[i+1]
		switch esc {
		case 'n':
			out = append(out, 0x0A)
			i += 2
		case 't':
			out = append(out, 0x09)
			i += 2
		case 'r':
			out = append(out, 0x0D)
			i += 2
		case '0':
			out = append(out, 0x00)
			i += 2
		case '\\':
			out = append(out, 0x5C)
			i += 2
		case '"':
			out = append(out, 0x22)
			i += 2
		case 'x':
			if i+3 >= len(s) {
				return nil, fmt.Errorf("\\xHH requires two hex digits")
			}
			hi := hexVal(s[i+2])
			lo := hexVal(s[i+3])
			if hi < 0 || lo < 0 {
				return nil, fmt.Errorf("\\xHH requires two hex digits")
			}
			out = append(out, byte(hi<<4|lo))
			i += 4
		default:
			return nil, fmt.Errorf("invalid escape \\%c", esc)
		}
	}
	return out, nil
}

func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}
