// Package compiler — компиляция AST в стековый байткод ВМ.
//
// Трек α: локальные функции (hoisting + манглированные имена),
// замыкания (capture-by-value), лямбды, взаимная рекурсия.
// v0.4.7: trap/ensure (§10.2, §10.3).
// v0.4.8: акторы — spawn/send/recv/watch + OpMatchLocal (§12).
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

// allocTemp резервирует анонимный локальный слот под временное значение.
// Используется trap/recv.
func (fc *funcCompiler) allocTemp() int {
	idx := len(fc.locals)
	fc.locals = append(fc.locals, "") // анонимный слот
	return idx
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
	case ast.DecimalExpr:
		return fmt.Errorf("срез: decimal не реализован")
	case ast.BytesExpr:
		return fmt.Errorf("срез: bytes не реализован")
	case ast.RegexExpr:
		return fmt.Errorf("срез: regex не реализован")
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
	case ast.RecvExpr:
		return fc.compileRecv(ex)
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
//	CONSTANT fnIdx
//	GETLOCAL x          # захват 0
//	GETUPVALUE y        # захват 1 (если вложенная)
//	MAKECLOSURE 2
func (fc *funcCompiler) compileLambda(name string, params []string, body ast.Expr) error {
	child := fc.compiler.newFuncCompiler(fc)
	child.prefix = fc.prefix + "lambda$"

	for _, p := range params {
		child.declareLocal(p)
	}

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

// ---- вызовы ----

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

		// ---- v0.4.8: actor primitives ----
		case "spawn":
			if len(call.Args()) != 1 {
				return fmt.Errorf("spawn требует 1 аргумент (fn)")
			}
			if err := fc.compileExpr(call.Args()[0]); err != nil {
				return err
			}
			fc.emit(vm.OpSpawn, 0)
			return nil
		case "spawn_linked":
			if len(call.Args()) != 1 {
				return fmt.Errorf("spawn_linked требует 1 аргумент (fn)")
			}
			if err := fc.compileExpr(call.Args()[0]); err != nil {
				return err
			}
			fc.emit(vm.OpSpawn, 1)
			return nil
		case "send":
			if len(call.Args()) != 2 {
				return fmt.Errorf("send требует 2 аргумента (pid, msg)")
			}
			if err := fc.compileExpr(call.Args()[0]); err != nil {
				return err
			}
			if err := fc.compileExpr(call.Args()[1]); err != nil {
				return err
			}
			fc.emit(vm.OpSend, 0)
			return nil
		case "self":
			if len(call.Args()) != 0 {
				return fmt.Errorf("self не принимает аргументов")
			}
			fc.emit(vm.OpSelf, 0)
			return nil
		case "make_ref":
			if len(call.Args()) != 0 {
				return fmt.Errorf("make_ref не принимает аргументов")
			}
			fc.emit(vm.OpMakeRef, 0)
			return nil
		case "watch":
			if len(call.Args()) != 1 {
				return fmt.Errorf("watch требует 1 аргумент (pid)")
			}
			if err := fc.compileExpr(call.Args()[0]); err != nil {
				return err
			}
			fc.emit(vm.OpWatch, 0)
			return nil
		case "unwatch":
			if len(call.Args()) != 1 {
				return fmt.Errorf("unwatch требует 1 аргумент (ref)")
			}
			if err := fc.compileExpr(call.Args()[0]); err != nil {
				return err
			}
			fc.emit(vm.OpUnwatch, 0)
			return nil
		case "mailbox_size":
			if len(call.Args()) != 1 {
				return fmt.Errorf("mailbox_size требует 1 аргумент (pid)")
			}
			if err := fc.compileExpr(call.Args()[0]); err != nil {
				return err
			}
			fc.emit(vm.OpMailboxSize, 0)
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

	fc.chunk.PatchOperand(elseJump, len(vm.ChunkCode(fc.chunk)))
	if ie.ElseBody() != nil {
		if err := fc.compileBranchBody(ie.ElseBody()); err != nil {
			return err
		}
	} else {
		fc.emitUnit()
	}
	fc.chunk.PatchOperand(endJump, len(vm.ChunkCode(fc.chunk)))
	return nil
}

func (fc *funcCompiler) compileBranchBody(body ast.Expr) error {
	if blk, ok := body.(*ast.BlockStmt); ok {
		return fc.compileStmts(blk.Body())
	}
	return fc.compileExpr(body)
}

// ---- trap / ensure (v0.4.7, §10.2/§10.3) ----

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
// Схема байткода (v0.4.7, A2):
//
//	TRAPBEGIN outer              ; ловит raise из ensures (в success и exception)
//	  TRAPBEGIN body             ; ловит raise из тела
//	    <body>
//	  TRAPEND                    ; тело успешно
//	  <ensure[N-1]> POP          ; LIFO
//	  ...
//	  <ensure[0]>   POP
//	  MAKEOK                     ; Ok(v)
//	  TRAPEND                    ; outer ok
//	  JMP done
//	body_handler:
//	  SETLOCAL exc               ; сохраняем исходную ошибку
//	  <ensure[N-1]> POP          ; LIFO (под outer)
//	  ...
//	  <ensure[0]>   POP
//	  TRAPEND                    ; outer ok
//	  GETLOCAL exc
//	  MAKEERROR                  ; Error(exc)
//	  JMP done
//	outer_handler:
//	  MAKEERROR                  ; Error(ensure_raise_value)
//	done:
//
// Семантика §10.3 (упрощение среза):
//   - если ensure падает, оставшиеся ensure НЕ выполняются;
//   - побеждает последняя ошибка (упрощённая форма «последняя побеждает»).
//     TODO(подэтап 4.9): полная семантика «оставшиеся ensure всё равно
//     выполняются» — вместе с переработкой кадров под акторы.
//
// TCO-инвариант (§15.3, принцип #11): TCO внутри области активного ensure
// должен быть отключён. В текущей стековой ВМ TCO вообще нет — формально
// инвариант соблюдён; при миграции на регистровую ВМ это точка внимания.
//
// Dual-compile ensures (дважды: в success и exception путях) безопасен,
// потому что ensure — выражение (не блок), локалы в родительской функции
// не создаёт. Локальные fn в ensure невозможны по грамматике.
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

	// --- Быстрый путь: без ensure ---
	if len(ensures) == 0 {
		fc.emit(vm.OpTrapBegin, 0)
		beginPos := fc.chunk.OperandPos()

		if err := fc.compileStmts(stmts); err != nil {
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

	// --- Полный путь: с ensure ---

	// outer — ловит raise из ensures.
	fc.emit(vm.OpTrapBegin, 0)
	outerBegin := fc.chunk.OperandPos()

	// body — ловит raise из тела.
	fc.emit(vm.OpTrapBegin, 0)
	bodyBegin := fc.chunk.OperandPos()

	if err := fc.compileStmts(stmts); err != nil {
		return err
	}

	fc.emit(vm.OpTrapEnd, 0) // закрываем body

	// Слот для сохранения исходной ошибки (используется в body_handler).
	// Резервируем ПОСЛЕ компиляции тела, чтобы не пересечься с локалами тела.
	excSlot := fc.allocTemp()

	// Success-путь: ensures LIFO, затем Ok(v).
	for i := len(ensures) - 1; i >= 0; i-- {
		if err := fc.compileExpr(ensures[i]); err != nil {
			return err
		}
		fc.emit(vm.OpPop, 0)
	}
	fc.emit(vm.OpMakeOk, 0)
	fc.emit(vm.OpTrapEnd, 0) // закрываем outer
	fc.emit(vm.OpJump, 0)
	successJump := fc.chunk.OperandPos()

	// --- body_handler: raise из тела ---
	bodyHandlerAddr := len(vm.ChunkCode(fc.chunk))
	fc.chunk.PatchOperand(bodyBegin, bodyHandlerAddr)

	fc.emit(vm.OpSetLocal, excSlot)
	for i := len(ensures) - 1; i >= 0; i-- {
		if err := fc.compileExpr(ensures[i]); err != nil {
			return err
		}
		fc.emit(vm.OpPop, 0)
	}
	fc.emit(vm.OpTrapEnd, 0) // закрываем outer (нормальное завершение handler)
	fc.emit(vm.OpGetLocal, excSlot)
	fc.emit(vm.OpMakeError, 0)
	fc.emit(vm.OpJump, 0)
	bodyEndJump := fc.chunk.OperandPos()

	// --- outer_handler: raise из ensures (в success или exception пути) ---
	outerHandlerAddr := len(vm.ChunkCode(fc.chunk))
	fc.chunk.PatchOperand(outerBegin, outerHandlerAddr)

	// handleRaise уже снял outer и положил значение ошибки на стек.
	fc.emit(vm.OpMakeError, 0)

	endAddr := len(vm.ChunkCode(fc.chunk))
	fc.chunk.PatchOperand(successJump, endAddr)
	fc.chunk.PatchOperand(bodyEndJump, endAddr)
	return nil
}

// ---- v0.4.8: recv (§12.4) ----

// compileRecv компилирует recv-выражение.
//
// Схема:
//
//	[<after-ms>] RECVTIMER              ; только если after задан
//	RECVTAKE msgSlot afterAddr           ; afterAddr = 0xFFFF если no after
//	MATCHLOCAL msgSlot p1                ; p1.FailAddr → next1
//	  <body1>
//	  JMP end
//	next1: MATCHLOCAL msgSlot p2 ...
//	  ...
//	nextN:
//	  <else body или raise(:recv_clause, msg)>
//	  JMP end
//	afterAddr:
//	  <after body или raise(:recv_clause, msg)>
//	end:
func (fc *funcCompiler) compileRecv(re ast.RecvExpr) error {
	msgSlot := fc.allocTemp()

	afterTime := re.RecvAfterTime()
	afterBody := re.RecvAfterBody()
	hasAfter := afterTime != nil

	if hasAfter {
		if err := fc.compileExpr(afterTime); err != nil {
			return err
		}
		fc.emit(vm.OpRecvTimer, 0)
	}

	// OpRecvTake <msgSlot> <afterAddr>
	if hasAfter {
		fc.chunk.EmitTwo(vm.OpRecvTake, msgSlot, 0, fc.line)
	} else {
		fc.chunk.EmitTwo(vm.OpRecvTake, msgSlot, 0xFFFF, fc.line)
	}
	afterOperandPos := fc.chunk.Operand2Pos()

	// Ветки.
	var endJumps []int
	for _, br := range re.RecvBranches() {
		cp, err := fc.compilePattern(br.Pattern)
		if err != nil {
			return err
		}
		patIdx := fc.chunk.AddPattern(cp)

		// MATCHLOCAL msgSlot patIdx
		fc.chunk.EmitTwo(vm.OpMatchLocal, msgSlot, patIdx, fc.line)

		if err := fc.compileBranchBody(br.Body); err != nil {
			return err
		}
		// JMP end
		fc.emit(vm.OpJump, 0)
		endJumps = append(endJumps, fc.chunk.OperandPos())

		// FailAddr = текущий адрес (начало следующей ветки).
		cp.FailAddr = len(vm.ChunkCode(fc.chunk))
	}

	// Ни одна ветка не подошла → else или raise.
	var noMatchEnd int
	if re.RecvElseBody() != nil {
		if err := fc.compileBranchBody(re.RecvElseBody()); err != nil {
			return err
		}
		fc.emit(vm.OpJump, 0)
		noMatchEnd = fc.chunk.OperandPos()
	} else {
		// raise(:recv_clause, msg)
		idx := fc.chunk.AddConstant(runtime.Atom("recv_clause"))
		fc.emit(vm.OpConstant, idx)
		fc.emit(vm.OpGetLocal, msgSlot)
		fc.emit(vm.OpTuple, 2)
		fc.emit(vm.OpRaise, 0)
	}

	// afterAddr — здесь.
	afterAddr := len(vm.ChunkCode(fc.chunk))
	if hasAfter {
		if err := fc.compileBranchBody(afterBody); err != nil {
			return err
		}
	} else {
		// no after: raise(:recv_clause, msg) — недостижимо, но валидно.
		idx := fc.chunk.AddConstant(runtime.Atom("recv_clause"))
		fc.emit(vm.OpConstant, idx)
		fc.emit(vm.OpGetLocal, msgSlot)
		fc.emit(vm.OpTuple, 2)
		fc.emit(vm.OpRaise, 0)
	}

	// Патчим afterAddr у OpRecvTake.
	if hasAfter {
		fc.chunk.PatchOperand(afterOperandPos, afterAddr)
	}

	// Патчим все JMP end.
	endAddr := len(vm.ChunkCode(fc.chunk))
	for _, pos := range endJumps {
		fc.chunk.PatchOperand(pos, endAddr)
	}
	if noMatchEnd != 0 {
		fc.chunk.PatchOperand(noMatchEnd, endAddr)
	}
	return nil
}

// ---- v0.4.8: компиляция паттернов ----

// compilePattern компилирует AST-паттерн в vm.CompiledPattern.
//
// ВАЖНО: `ast.PatternWildcard` — пустой интерфейс (только `Pattern`),
// поэтому ему удовлетворяет ЛЮБОЙ паттерн. В type switch его надо
// проверять ПОСЛЕДНИМ, иначе он перехватит Ident/Literal/Ctor/Tuple/As.
func (fc *funcCompiler) compilePattern(pat ast.Pattern) (*vm.CompiledPattern, error) {
	switch p := pat.(type) {
	case ast.IdentPattern:
		slot := fc.declareLocal(p.IdentName())
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
		return &vm.CompiledPattern{
			Kind: vm.PatCtor,
			Tag:  p.CtorName(),
			Subs: subs,
		}, nil

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

	case ast.PatternAs:
		inner, err := fc.compilePattern(p.AsInner())
		if err != nil {
			return nil, err
		}
		slot := fc.declareLocal(p.AsName())
		return &vm.CompiledPattern{
			Kind:   vm.PatAs,
			Inner:  inner,
			AsSlot: slot,
		}, nil

	// PatternWildcard — catch-all, обязан быть последним.
	case ast.PatternWildcard:
		return &vm.CompiledPattern{Kind: vm.PatWildcard}, nil
	}
	return nil, fmt.Errorf("срез: неподдерживаемый паттерн %T", pat)
}

// parseLiteralValue превращает строку литерала в runtime.Value.
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
		return runtime.Str(s[1 : len(s)-1]), nil
	}
	if i, err := strconv.ParseInt(strings.ReplaceAll(s, "_", ""), 0, 64); err == nil {
		return runtime.Int(i), nil
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return runtime.Float(f), nil
	}
	return runtime.Unit, fmt.Errorf("неизвестный литерал %q", s)
}
