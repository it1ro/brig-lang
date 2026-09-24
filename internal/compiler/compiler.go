// Package compiler — компиляция AST в стековый байткод ВМ.
//
// Трек α: локальные функции (hoisting + манглированные имена),
// замыкания (capture-by-value), лямбды, взаимная рекурсия.
package compiler

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/it1ro/brig-lang/internal/ast"
	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/vm"
)

// ProgramImage — результат компиляции модуля.
type ProgramImage struct {
	Functions map[string]*vm.Function
	Main      *vm.Function
}

// Compiler переводит AST в ProgramImage.
type Compiler struct {
	image *ProgramImage
}

// New создаёт компилятор.
func New() *Compiler {
	return &Compiler{image: &ProgramImage{Functions: make(map[string]*vm.Function)}}
}

// Compile — входная точка.
func (c *Compiler) Compile(prog *ast.Program) (*ProgramImage, error) {
	for _, d := range prog.Decls {
		fd, ok := d.(ast.FuncDecl)
		if !ok {
			continue
		}
		clauses := fd.FuncClauses()
		if len(clauses) == 0 {
			continue
		}
		cl := clauses[0]
		fn, err := c.compileFunction(fd.FnName(), cl.Params, cl.Body)
		if err != nil {
			return nil, fmt.Errorf("fn %s: %w", fd.FnName(), err)
		}
		c.image.Functions[fd.FnName()] = fn
		if fd.FnName() == "main" {
			c.image.Main = fn
		}
	}
	if len(prog.Stmts) > 0 {
		fn, err := c.compileBlock("__repl__", nil, prog.Stmts)
		if err != nil {
			return nil, err
		}
		c.image.Functions["__repl__"] = fn
		c.image.Main = fn
	}
	return c.image, nil
}

// ---- funcCompiler ----

// upvalueInfo — описание захваченной переменной.
type upvalueInfo struct {
	name    string
	isLocal bool // true: захват из локалей родителя
	index   int  // индекс в локалях (или upvalues) родителя
}

// funcCompiler — состояние компиляции одной функции.
type funcCompiler struct {
	compiler *Compiler     // обратная ссылка для доступа к ProgramImage
	parent   *funcCompiler // объемлющая область (nil для top-level)
	prefix   string        // префикс для манглирования: "main$"
	chunk    *vm.Chunk
	locals   []string
	scopes   []map[string]int
	localFns map[string]string // имя в исходнике → манглированное имя
	upvalues []upvalueInfo
	line     int
}

// newFuncCompiler создаёт компилятор функции.
func (c *Compiler) newFuncCompiler(parent *funcCompiler) *funcCompiler {
	return &funcCompiler{
		compiler: c,
		parent:   parent,
		chunk:    vm.NewChunk(),
		scopes:   []map[string]int{{}},
		localFns: make(map[string]string),
	}
}

func (fc *funcCompiler) declareLocal(name string) int {
	idx := len(fc.locals)
	fc.locals = append(fc.locals, name)
	fc.scopes[len(fc.scopes)-1][name] = idx
	return idx
}

func (fc *funcCompiler) resolveLocal(name string) (int, bool) {
	for i := len(fc.scopes) - 1; i >= 0; i-- {
		if idx, ok := fc.scopes[i][name]; ok {
			return idx, true
		}
	}
	return 0, false
}

func (fc *funcCompiler) emit(op vm.OpCode, operand int) {
	fc.chunk.Emit(op, operand, fc.line)
}

func (fc *funcCompiler) emitUnit() {
	idx := fc.chunk.AddConstant(runtime.Unit)
	fc.emit(vm.OpConstant, idx)
}

// ---- компиляция функций ----

func (c *Compiler) compileFunction(name string, params []string, body *ast.BlockStmt) (*vm.Function, error) {
	fc := c.newFuncCompiler(nil)
	fc.prefix = name + "$"
	arity := 0
	for _, p := range params {
		if strings.HasPrefix(p, "..") {
			fc.declareLocal(strings.TrimPrefix(p, ".."))
			continue
		}
		fc.declareLocal(p)
		arity++
	}
	if body != nil {
		if err := fc.compileStmts(body.Body()); err != nil {
			return nil, err
		}
	}
	fc.emit(vm.OpReturn, 0)
	for _, p := range params {
		if strings.HasPrefix(p, "..") {
			arity = -1
		}
	}
	return &vm.Function{Name: name, Arity: arity, Chunk: fc.chunk}, nil
}

func (c *Compiler) compileBlock(name string, params []string, stmts []ast.Stmt) (*vm.Function, error) {
	fc := c.newFuncCompiler(nil)
	fc.prefix = name + "$"
	for _, p := range params {
		fc.declareLocal(p)
	}
	if err := fc.compileStmts(stmts); err != nil {
		return nil, err
	}
	fc.emit(vm.OpReturn, 0)
	return &vm.Function{Name: name, Arity: len(params), Chunk: fc.chunk}, nil
}

// ---- стейтменты ----

func (fc *funcCompiler) compileStmts(stmts []ast.Stmt) error {
	// Фаза 1: hoisting — собираем имена локальных функций.
	// Это позволяет ссылаться на них до момента компиляции тела
	// (взаимная рекурсия: is_even ↔ is_odd).
	for _, s := range stmts {
		if lfd, ok := s.(ast.LocalFnDecl); ok {
			name := lfd.FnName()
			mangled := fc.prefix + name
			fc.localFns[name] = mangled
		}
	}

	// Фаза 2: компиляция в порядке следования.
	for i, s := range stmts {
		if i > 0 {
			fc.emit(vm.OpPop, 0)
		}
		if err := fc.compileStmt(s); err != nil {
			return err
		}
	}
	if len(stmts) == 0 {
		fc.emitUnit()
	}
	return nil
}

func (fc *funcCompiler) compileStmt(s ast.Stmt) error {
	switch st := s.(type) {
	case ast.LetBind:
		if err := fc.compileExpr(st.Val()); err != nil {
			return err
		}
		if ip, ok := st.Pat().(ast.IdentPattern); ok {
			idx := fc.declareLocal(ip.IdentName())
			fc.emit(vm.OpSetLocal, idx)
			fc.emitUnit()
			return nil
		}
		return fmt.Errorf("срез: только простые связывания `name = expr`")
	case ast.ExprStmt:
		return fc.compileExpr(st.ExprValue())
	case ast.LocalFnDecl:
		return fc.compileLocalFn(st)
	}
	return fmt.Errorf("срез: неподдерживаемый стейтмент %T", s)
}

// compileLocalFn компилирует локальную функцию.
//
// Стратегия: функция компилируется как отдельный *vm.Function
// с манглированным именем (prefix + name) и регистрируется в
// ProgramImage. При запуске модуля все функции из ProgramImage
// попадают в глобальную таблицу ВМ, что обеспечивает:
//   - взаимную рекурсию (обе функции видны через OpGetGlobal);
//   - простую рекурсию (функция видит себя через OpGetGlobal).
//
// В теле родительской функции локальная функция хранится как
// локальная переменная (OpSetLocal) для быстрого доступа.
func (fc *funcCompiler) compileLocalFn(decl ast.LocalFnDecl) error {
	name := decl.FnName()
	clauses := decl.Clauses()
	if len(clauses) == 0 {
		return fmt.Errorf("local fn %s: нет клозов", name)
	}
	cl := clauses[0] // срез: первый клоз

	mangled := fc.localFns[name]

	// Компилируем тело как отдельную функцию.
	child := fc.compiler.newFuncCompiler(fc)
	child.prefix = mangled + "$"

	arity := 0
	for _, p := range cl.Params {
		child.declareLocal(p)
		arity++
	}
	if cl.Body != nil {
		if err := child.compileStmts(cl.Body.Body()); err != nil {
			return fmt.Errorf("local fn %s: %w", name, err)
		}
	}
	child.emit(vm.OpReturn, 0)

	fn := &vm.Function{Name: mangled, Arity: arity, Chunk: child.chunk}

	// Регистрируем в ProgramImage → попадёт в глобалы при запуске.
	fc.compiler.image.Functions[mangled] = fn

	// В родительской функции: сохраняем как локальную переменную.
	idx := fc.declareLocal(name)
	fnVal := vm.FuncValue(fn)
	fnIdx := fc.chunk.AddConstant(fnVal)
	fc.emit(vm.OpConstant, fnIdx)
	fc.emit(vm.OpSetLocal, idx)
	fc.emitUnit() // let-стейтмент оставляет ()

	return nil
}

// ---- выражения ----

func (fc *funcCompiler) compileExpr(e ast.Expr) error {
	fc.line = e.Pos()
	switch ex := e.(type) {
	case ast.LiteralExpr:
		return fc.compileLiteral(ex.ValueStr())
	case ast.AtomExpr:
		idx := fc.chunk.AddConstant(runtime.Atom(ex.AtomName()))
		fc.emit(vm.OpConstant, idx)
		return nil
	case ast.DecimalExpr, ast.BytesExpr, ast.RegexExpr:
		return fmt.Errorf("срез: сигилы не реализованы")
	case ast.VariableExpr:
		return fc.compileVar(ex.Name())
	case ast.GroupingExpr:
		return fc.compileExpr(ex.Inner())
	case ast.UnaryExpr:
		return fc.compileUnary(ex)
	case ast.BinaryExpr:
		return fc.compileBinary(ex)
	case ast.CallExpr:
		return fc.compileCall(ex)
	case ast.IfExpr:
		return fc.compileIf(ex)
	case ast.TrapExpr:
		return fc.compileTrap(ex)
	case ast.LambdaShort:
		return fc.compileLambda("", []string{ex.ParamName()}, ex.Body())
	case ast.LambdaEmpty:
		return fc.compileLambda("", nil, ex.Body())
	case ast.LambdaFull:
		return fc.compileLambda("", ex.ParamNames(), ex.BlockBody())
	}
	return fmt.Errorf("срез: неподдерживаемое выражение %T", e)
}

func (fc *funcCompiler) compileLiteral(lit string) error {
	switch lit {
	case "()":
		fc.emitUnit()
		return nil
	case "true":
		idx := fc.chunk.AddConstant(runtime.Bool(true))
		fc.emit(vm.OpConstant, idx)
		return nil
	case "false":
		idx := fc.chunk.AddConstant(runtime.Bool(false))
		fc.emit(vm.OpConstant, idx)
		return nil
	}
	if i, err := strconv.ParseInt(strings.ReplaceAll(lit, "_", ""), 0, 64); err == nil {
		idx := fc.chunk.AddConstant(runtime.Int(i))
		fc.emit(vm.OpConstant, idx)
		return nil
	}
	if f, err := strconv.ParseFloat(lit, 64); err == nil {
		idx := fc.chunk.AddConstant(runtime.Float(f))
		fc.emit(vm.OpConstant, idx)
		return nil
	}
	if strings.HasPrefix(lit, "\"") && strings.HasSuffix(lit, "\"") {
		s := lit[1 : len(lit)-1]
		idx := fc.chunk.AddConstant(runtime.Str(s))
		fc.emit(vm.OpConstant, idx)
		return nil
	}
	return fmt.Errorf("срез: неподдерживаемый литерал %q", lit)
}

// compileVar разрешает переменную в порядке приоритета:
//  1. собственные локалы → OpGetLocal
//  2. локальные функции (свои или родителя) → OpGetGlobal(mangled)
//     (Проверяем ДО захвата upvalues, потому что LocalFnDecl хосятся
//     в глобалы для взаимной рекурсии и не должны захватываться как
//     upvalues, даже если они объявлены как локалы в родителе).
//  3. уже захваченные upvalues → OpGetUpvalue
//  4. локалы родителя → захват (upvalue) + OpGetUpvalue
//  5. глобалы → OpGetGlobal(name)
func (fc *funcCompiler) compileVar(name string) error {
	// 1. Собственные локалы.
	if idx, ok := fc.resolveLocal(name); ok {
		fc.emit(vm.OpGetLocal, idx)
		return nil
	}
	// 2. Локальные функции (манглированные имена).
	if mangled, ok := fc.localFns[name]; ok {
		gidx := fc.chunk.AddConstant(runtime.Str(mangled))
		fc.emit(vm.OpGetGlobal, gidx)
		return nil
	}
	if fc.parent != nil {
		if mangled, ok := fc.parent.localFns[name]; ok {
			gidx := fc.chunk.AddConstant(runtime.Str(mangled))
			fc.emit(vm.OpGetGlobal, gidx)
			return nil
		}
	}
	// 3. Уже захваченные upvalues.
	for i, uv := range fc.upvalues {
		if uv.name == name {
			fc.emit(vm.OpGetUpvalue, i)
			return nil
		}
	}
	// 4. Локалы родителя → захват.
	if fc.parent != nil {
		if idx, ok := fc.parent.resolveLocal(name); ok {
			uvIdx := len(fc.upvalues)
			fc.upvalues = append(fc.upvalues, upvalueInfo{
				name: name, isLocal: true, index: idx,
			})
			fc.emit(vm.OpGetUpvalue, uvIdx)
			return nil
		}
	}
	// 5. Глобал.
	gidx := fc.chunk.AddConstant(runtime.Str(name))
	fc.emit(vm.OpGetGlobal, gidx)
	return nil
}

// ---- лямбды и замыкания ----

// compileLambda компилирует лямбду как замыкание.
//
// Байткод в родителе:
//
//	CONSTANT fnIdx      # функция из пула констант
//	GETLOCAL x          # захват 0
//	GETUPVALUE y        # захват 1 (если вложенная)
//	MAKECLOSURE 2       # pop 2 захвата + pop функцию → closure
func (fc *funcCompiler) compileLambda(name string, params []string, body ast.Expr) error {
	child := fc.compiler.newFuncCompiler(fc)
	child.prefix = fc.prefix + "lambda$"

	for _, p := range params {
		child.declareLocal(p)
	}

	// Компилируем тело.
	if blk, ok := body.(*ast.BlockStmt); ok {
		if err := child.compileStmts(blk.Body()); err != nil {
			return err
		}
	} else {
		if err := child.compileExpr(body); err != nil {
			return err
		}
	}
	child.emit(vm.OpReturn, 0)

	arity := len(params)
	fn := &vm.Function{Name: name, Arity: arity, Chunk: child.chunk}
	fnVal := vm.FuncValue(fn)
	fnIdx := fc.chunk.AddConstant(fnVal)

	// Эмитим: push функция, push каждый захват, MAKECLOSURE.
	fc.emit(vm.OpConstant, fnIdx)
	for _, uv := range child.upvalues {
		if uv.isLocal {
			fc.emit(vm.OpGetLocal, uv.index)
		} else {
			fc.emit(vm.OpGetUpvalue, uv.index)
		}
	}
	fc.emit(vm.OpMakeClosure, len(child.upvalues))

	return nil
}

// ---- унарные / бинарные операторы ----

func (fc *funcCompiler) compileUnary(u ast.UnaryExpr) error {
	if err := fc.compileExpr(u.Operand()); err != nil {
		return err
	}
	switch u.OpStr() {
	case "-":
		fc.emit(vm.OpNeg, 0)
	case "not":
		fc.emit(vm.OpNot, 0)
	default:
		return fmt.Errorf("срез: унарный оператор %q", u.OpStr())
	}
	return nil
}

func (fc *funcCompiler) compileBinary(b ast.BinaryExpr) error {
	switch b.OpStr() {
	case "and":
		return fc.compileAndOr(b, true)
	case "or":
		return fc.compileAndOr(b, false)
	}
	if err := fc.compileExpr(b.Left()); err != nil {
		return err
	}
	if err := fc.compileExpr(b.Right()); err != nil {
		return err
	}
	switch b.OpStr() {
	case "+":
		fc.emit(vm.OpAdd, 0)
	case "-":
		fc.emit(vm.OpSub, 0)
	case "*":
		fc.emit(vm.OpMul, 0)
	case "/":
		fc.emit(vm.OpDiv, 0)
	case "div":
		fc.emit(vm.OpIntDiv, 0)
	case "rem":
		fc.emit(vm.OpRem, 0)
	case "**":
		fc.emit(vm.OpPow, 0)
	case "==":
		fc.emit(vm.OpEq, 0)
	case "!=":
		fc.emit(vm.OpNeq, 0)
	case "<":
		fc.emit(vm.OpLt, 0)
	case ">":
		fc.emit(vm.OpGt, 0)
	case "<=":
		fc.emit(vm.OpLe, 0)
	case ">=":
		fc.emit(vm.OpGe, 0)
	default:
		return fmt.Errorf("срез: оператор %q", b.OpStr())
	}
	return nil
}

// compileAndOr — and/or с коротким замыканием.
//
// `a and b`: dup a → jumpfalse SHORT → pop a → eval b → jump END
// `a or b`:  dup a → jumptrue  SHORT → pop a → eval b → jump END
func (fc *funcCompiler) compileAndOr(b ast.BinaryExpr, isAnd bool) error {
	if err := fc.compileExpr(b.Left()); err != nil {
		return err
	}
	fc.emit(vm.OpDup, 0)

	jumpOp := vm.OpJumpFalse
	if !isAnd {
		jumpOp = vm.OpJumpTrue
	}
	fc.emit(jumpOp, 0)
	jumpPos := fc.chunk.OperandPos()

	fc.emit(vm.OpPop, 0)
	if err := fc.compileExpr(b.Right()); err != nil {
		return err
	}
	fc.emit(vm.OpJump, 0)
	endJumpPos := fc.chunk.OperandPos()

	fc.chunk.PatchOperand(jumpPos, len(vm.ChunkCode(fc.chunk)))
	fc.chunk.PatchOperand(endJumpPos, len(vm.ChunkCode(fc.chunk)))
	return nil
}

// ---- вызовы и коллекции ----

func (fc *funcCompiler) compileCall(call ast.CallExpr) error {
	callee := call.Callee()
	if ve, ok := callee.(ast.VariableExpr); ok {
		switch ve.Name() {
		case "()":
			for _, a := range call.Args() {
				if err := fc.compileExpr(a); err != nil {
					return err
				}
			}
			fc.emit(vm.OpTuple, len(call.Args()))
			return nil
		case "[]":
			for _, a := range call.Args() {
				if err := fc.compileExpr(a); err != nil {
					return err
				}
			}
			fc.emit(vm.OpList, len(call.Args()))
			return nil
		case "%[]":
			for _, a := range call.Args() {
				if err := fc.compileExpr(a); err != nil {
					return err
				}
			}
			fc.emit(vm.OpVector, len(call.Args()))
			return nil
		case "%{}":
			n := 0
			for _, a := range call.Args() {
				pair, ok := a.(ast.BinaryExpr)
				if !ok || pair.OpStr() != "=>" {
					return fmt.Errorf("срез: элемент мапы должен быть парой =>")
				}
				if err := fc.compileExpr(pair.Left()); err != nil {
					return err
				}
				if err := fc.compileExpr(pair.Right()); err != nil {
					return err
				}
				n++
			}
			fc.emit(vm.OpMap, n)
			return nil
		}
	}
	if err := fc.compileExpr(callee); err != nil {
		return err
	}
	for _, a := range call.Args() {
		if err := fc.compileExpr(a); err != nil {
			return err
		}
	}
	fc.emit(vm.OpCall, len(call.Args()))
	return nil
}

// ---- if ----

func (fc *funcCompiler) compileIf(ie ast.IfExpr) error {
	if err := fc.compileExpr(ie.Cond()); err != nil {
		return err
	}
	fc.emit(vm.OpJumpFalse, 0)
	elseJump := len(vm.ChunkCode(fc.chunk)) - 2

	if err := fc.compileBranchBody(ie.ThenBody()); err != nil {
		return err
	}
	fc.emit(vm.OpJump, 0)
	endJump := len(vm.ChunkCode(fc.chunk)) - 2

	patch(fc.chunk, elseJump, len(vm.ChunkCode(fc.chunk)))
	if ie.ElseBody() != nil {
		if err := fc.compileBranchBody(ie.ElseBody()); err != nil {
			return err
		}
	} else {
		fc.emitUnit()
	}
	patch(fc.chunk, endJump, len(vm.ChunkCode(fc.chunk)))
	return nil
}

func (fc *funcCompiler) compileBranchBody(body ast.Expr) error {
	if blk, ok := body.(*ast.BlockStmt); ok {
		return fc.compileStmts(blk.Body())
	}
	return fc.compileExpr(body)
}

// patch пишет 16-битный операнд в позицию операнда инструкции.
func patch(ch *vm.Chunk, operandPos, target int) {
	code := vm.ChunkCode(ch)
	code[operandPos] = byte(target >> 8)
	code[operandPos+1] = byte(target)
}

// ---- trap / ensure (v0.4.7, §10.2/§10.3) ----

// allocTemp резервирует анонимный локальный слот под временное значение.
func (fc *funcCompiler) allocTemp() int {
	idx := len(fc.locals)
	fc.locals = append(fc.locals, "") // анонимный слот
	return idx
}

// compileTrap различает инлайн-форму trap(expr) и блочную.
func (fc *funcCompiler) compileTrap(te ast.TrapExpr) error {
	if inline := te.TrapInline(); inline != nil {
		return fc.compileInlineTrap(inline)
	}
	return fc.compileBlockTrap(te)
}

// compileInlineTrap: trap(expr) — синтаксический сахар для блочной формы
// с одним стейтментом expr и без ensure (§10.2).
func (fc *funcCompiler) compileInlineTrap(inner ast.Expr) error {
	fc.emit(vm.OpTrapBegin, 0)
	beginPos := fc.chunk.OperandPos()

	if err := fc.compileExpr(inner); err != nil {
		return err
	}

	fc.emit(vm.OpTrapEnd, 0)
	fc.emit(vm.OpMakeOk, 0)
	fc.emit(vm.OpJump, 0)
	endJumpPos := fc.chunk.OperandPos()

	handlerAddr := len(vm.ChunkCode(fc.chunk))
	fc.chunk.PatchOperand(beginPos, handlerAddr)

	fc.emit(vm.OpMakeError, 0)

	endAddr := len(vm.ChunkCode(fc.chunk))
	fc.chunk.PatchOperand(endJumpPos, endAddr)
	return nil
}

// compileBlockTrap компилирует блочную форму trap с ensure-клаузами.
//
// Схема байткода:
//
//	TRAPBEGIN handler
//	  <body>                 ; оставляет значение тела
//	TRAPEND
//	  <ensure[N-1]> POP      ; LIFO: ensure выполняются в обратном порядке
//	  ...
//	  <ensure[0]>   POP
//	  MAKEOK
//	  JMP done
//	handler:
//	  SETLOCAL exc            ; только если есть ensure
//	  <ensure[N-1]> POP      ; тот же LIFO-порядок
//	  ...
//	  <ensure[0]>   POP
//	  GETLOCAL exc
//	  MAKEERROR
//	done:
//
// Замечание: ensure-выражения компилируются дважды (в success- и
// exception-путях). Если ensure сам бросает raise в success-пути,
// исключение распространяется наружу — это осознанное упрощение
// среза (полная семантика §10.3 с заменой ошибки — отдельный подэтап).
func (fc *funcCompiler) compileBlockTrap(te ast.TrapExpr) error {
	var stmts []ast.Stmt
	switch body := te.TrapBody().(type) {
	case *ast.BlockStmt:
		stmts = body.Body()
	case nil:
		// пустое тело — валидно, вернёт ()
	default:
		stmts = []ast.Stmt{body}
	}
	ensures := te.TrapEnsures()

	fc.emit(vm.OpTrapBegin, 0)
	beginPos := fc.chunk.OperandPos()

	if err := fc.compileStmts(stmts); err != nil {
		return err
	}
	fc.emit(vm.OpTrapEnd, 0)

	// Success-путь: ensures LIFO, затем Ok(value).
	for i := len(ensures) - 1; i >= 0; i-- {
		if err := fc.compileExpr(ensures[i]); err != nil {
			return err
		}
		fc.emit(vm.OpPop, 0)
	}
	fc.emit(vm.OpMakeOk, 0)
	fc.emit(vm.OpJump, 0)
	endJumpPos := fc.chunk.OperandPos()

	// Слот для значения исключения резервируется после компиляции тела
	// и success-пути — чтобы не пересечься с локалами тела.
	excSlot := -1
	if len(ensures) > 0 {
		excSlot = fc.allocTemp()
	}

	handlerAddr := len(vm.ChunkCode(fc.chunk))
	fc.chunk.PatchOperand(beginPos, handlerAddr)

	if excSlot >= 0 {
		fc.emit(vm.OpSetLocal, excSlot)
	}
	for i := len(ensures) - 1; i >= 0; i-- {
		if err := fc.compileExpr(ensures[i]); err != nil {
			return err
		}
		fc.emit(vm.OpPop, 0)
	}
	if excSlot >= 0 {
		fc.emit(vm.OpGetLocal, excSlot)
	}
	fc.emit(vm.OpMakeError, 0)

	endAddr := len(vm.ChunkCode(fc.chunk))
	fc.chunk.PatchOperand(endJumpPos, endAddr)
	return nil
}
