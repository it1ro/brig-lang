// Package compiler — компиляция AST в стековый байткод ВМ.
//
// v0.4.7: trap/ensure (§10.2, §10.3).
// v0.4.8: акторы — spawn/send/recv/watch + OpMatchLocal (§12).
// v0.4.9: Range (§4.3), Index (§4.4/§4.5), модули Vec/Map (§4.4/§4.5),
// Bytes (§3.2).
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
	compiler *Compiler
	parent   *funcCompiler
	prefix   string
	chunk    *vm.Chunk
	locals   []string
	scopes   []map[string]int
	localFns map[string]string
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

// declareLocal — резервирует локальный слот. Проверяет границу maxLocals
// (Sprint 6.2): превышение — ошибка компиляции, а не внутренняя ошибка ВМ.
func (fc *funcCompiler) declareLocal(name string) int {
	idx := len(fc.locals)
	if idx >= vm.MaxLocals {
		panic(fmt.Sprintf(
			"compiler: function %q declares more than %d locals (last: %q)",
			fc.prefix, vm.MaxLocals, name))
	}
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

// allocTemp — резервирует анонимный слот. Та же проверка границы.
func (fc *funcCompiler) allocTemp() int {
	idx := len(fc.locals)
	if idx >= vm.MaxLocals {
		panic(fmt.Sprintf(
			"compiler: function %q declares more than %d locals (temp)",
			fc.prefix, vm.MaxLocals))
	}
	fc.locals = append(fc.locals, "")
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
	for _, s := range stmts {
		if lfd, ok := s.(ast.LocalFnDecl); ok {
			name := lfd.FnName()
			mangled := fc.prefix + name
			fc.localFns[name] = mangled
		}
	}

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

func (fc *funcCompiler) compileLocalFn(decl ast.LocalFnDecl) error {
	name := decl.FnName()
	clauses := decl.Clauses()
	if len(clauses) == 0 {
		return fmt.Errorf("local fn %s: нет клозов", name)
	}
	cl := clauses[0]

	mangled := fc.localFns[name]

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
	fc.compiler.image.Functions[mangled] = fn

	idx := fc.declareLocal(name)
	fnVal := vm.FuncValue(fn)
	fnIdx := fc.chunk.AddConstant(fnVal)
	fc.emit(vm.OpConstant, fnIdx)
	fc.emit(vm.OpSetLocal, idx)
	fc.emitUnit()

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
		return fc.compileBytesLiteral(ex.ValueStr())
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
	case ast.RangeExpr:
		return fc.compileRangeExpr(ex)
	case ast.IndexExpr:
		return fc.compileIndexExpr(ex)
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

// compileBytesLiteral декодирует body байтового литерала (текст между
// кавычками, escape-последовательности сохранены лексером as-is, см.
// lexer.scanBytes) и грузит как константу типа Bytes (§3.2, A4.2).
func (fc *funcCompiler) compileBytesLiteral(body string) error {
	b, err := decodeBytesBody(body)
	if err != nil {
		return fmt.Errorf("bytes literal: %w", err)
	}
	idx := fc.chunk.AddConstant(runtime.Bytes(b))
	fc.emit(vm.OpConstant, idx)
	return nil
}

// decodeBytesBody разворачивает escape-последовательности Bytes
// (§C.3): \n \t \r \0 \\ \" \xHH. Лексер уже проверил корректность,
// но сканер возвращает raw-тело без декодирования.
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

// compileRangeExpr компилирует `start to end` в OpRange (Sprint 5.1).
func (fc *funcCompiler) compileRangeExpr(re ast.RangeExpr) error {
	if err := fc.compileExpr(re.RangeStart()); err != nil {
		return err
	}
	if err := fc.compileExpr(re.RangeEnd()); err != nil {
		return err
	}
	fc.emit(vm.OpRange, 0)
	return nil
}

// compileIndexExpr компилирует `obj[idx]` в OpIndex (Sprint 5.3, 5.4).
func (fc *funcCompiler) compileIndexExpr(ie ast.IndexExpr) error {
	if err := fc.compileExpr(ie.Obj()); err != nil {
		return err
	}
	if err := fc.compileExpr(ie.Index()); err != nil {
		return err
	}
	fc.emit(vm.OpIndex, 0)
	return nil
}

func (fc *funcCompiler) compileVar(name string) error {
	if idx, ok := fc.resolveLocal(name); ok {
		fc.emit(vm.OpGetLocal, idx)
		return nil
	}
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
	for i, uv := range fc.upvalues {
		if uv.name == name {
			fc.emit(vm.OpGetUpvalue, i)
			return nil
		}
	}
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
	gidx := fc.chunk.AddConstant(runtime.Str(name))
	fc.emit(vm.OpGetGlobal, gidx)
	return nil
}

// ---- лямбды и замыкания ----

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

	// Модульный dispatch для Vec.* / Map.* / Str.* / Bytes.*.
	if me, ok := callee.(ast.MemberExpr); ok {
		if obj, ok := me.Obj().(ast.VariableExpr); ok {
			mod := obj.Name()
			if isPreludeModule(mod) {
				fullName := mod + "." + me.MemberName()
				gidx := fc.chunk.AddConstant(runtime.Str(fullName))
				fc.emit(vm.OpGetGlobal, gidx)
				for _, a := range call.Args() {
					if err := fc.compileExpr(a); err != nil {
						return err
					}
				}
				fc.emit(vm.OpCall, len(call.Args()))
				return nil
			}
		}
	}

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

// isPreludeModule — имена Upper-модулей прелюдии, для которых compileCall
// выполняет dispatch по имени "Mod.func" (Vec/Map/Str/Bytes).
func isPreludeModule(name string) bool {
	switch name {
	case "Vec", "Map", "Str", "Bytes":
		return true
	}
	return false
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

// ---- trap / ensure ----

func (fc *funcCompiler) compileTrap(te ast.TrapExpr) error {
	if inline := te.TrapInline(); inline != nil {
		return fc.compileInlineTrap(inline)
	}
	return fc.compileBlockTrap(te)
}

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

func (fc *funcCompiler) compileBlockTrap(te ast.TrapExpr) error {
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

	valueSlot := fc.allocTemp()
	errSlot := fc.allocTemp()
	noErrorIdx := fc.chunk.AddConstant(runtime.Atom("no_error"))

	fc.emit(vm.OpTrapBegin, 0)
	outerBegin := fc.chunk.OperandPos()

	fc.emit(vm.OpTrapBegin, 0)
	bodyBegin := fc.chunk.OperandPos()

	if err := fc.compileStmts(stmts); err != nil {
		return err
	}

	fc.emit(vm.OpTrapEnd, 0)
	fc.emit(vm.OpSetLocal, valueSlot)
	fc.emit(vm.OpConstant, noErrorIdx)
	fc.emit(vm.OpSetLocal, errSlot)
	fc.emit(vm.OpJump, 0)
	bodyOkJump := fc.chunk.OperandPos()

	bodyHandlerAddr := len(vm.ChunkCode(fc.chunk))
	fc.chunk.PatchOperand(bodyBegin, bodyHandlerAddr)
	fc.emit(vm.OpSetLocal, errSlot)

	runEnsuresAddr := len(vm.ChunkCode(fc.chunk))
	fc.chunk.PatchOperand(bodyOkJump, runEnsuresAddr)

	for i := len(ensures) - 1; i >= 0; i-- {
		fc.emit(vm.OpTrapBegin, 0)
		ensureBegin := fc.chunk.OperandPos()

		if err := fc.compileExpr(ensures[i]); err != nil {
			return err
		}
		fc.emit(vm.OpPop, 0)
		fc.emit(vm.OpTrapEnd, 0)
		fc.emit(vm.OpJump, 0)
		ensureSkipJump := fc.chunk.OperandPos()

		ehAddr := len(vm.ChunkCode(fc.chunk))
		fc.chunk.PatchOperand(ensureBegin, ehAddr)
		fc.emit(vm.OpSetLocal, errSlot)

		afterAddr := len(vm.ChunkCode(fc.chunk))
		fc.chunk.PatchOperand(ensureSkipJump, afterAddr)
	}

	fc.emit(vm.OpGetLocal, errSlot)
	fc.emit(vm.OpConstant, noErrorIdx)
	fc.emit(vm.OpEq, 0)
	fc.emit(vm.OpJumpFalse, 0)
	errorJump := fc.chunk.OperandPos()

	fc.emit(vm.OpGetLocal, valueSlot)
	fc.emit(vm.OpMakeOk, 0)
	fc.emit(vm.OpJump, 0)
	endJump := fc.chunk.OperandPos()

	errAddr := len(vm.ChunkCode(fc.chunk))
	fc.chunk.PatchOperand(errorJump, errAddr)
	fc.emit(vm.OpGetLocal, errSlot)
	fc.emit(vm.OpMakeError, 0)

	finalAddr := len(vm.ChunkCode(fc.chunk))
	fc.chunk.PatchOperand(endJump, finalAddr)
	fc.emit(vm.OpTrapEnd, 0)
	fc.emit(vm.OpJump, 0)
	doneJump := fc.chunk.OperandPos()

	outerAddr := len(vm.ChunkCode(fc.chunk))
	fc.chunk.PatchOperand(outerBegin, outerAddr)
	fc.emit(vm.OpMakeError, 0)

	doneAddr := len(vm.ChunkCode(fc.chunk))
	fc.chunk.PatchOperand(doneJump, doneAddr)
	return nil
}

// ---- recv ----

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

	if hasAfter {
		fc.chunk.EmitTwo(vm.OpRecvTake, msgSlot, 0, fc.line)
	} else {
		fc.chunk.EmitTwo(vm.OpRecvTake, msgSlot, 0xFFFF, fc.line)
	}
	afterOperandPos := fc.chunk.Operand2Pos()

	var endJumps []int
	for _, br := range re.RecvBranches() {
		cp, err := fc.compilePattern(br.Pattern)
		if err != nil {
			return err
		}
		patIdx := fc.chunk.AddPattern(cp)

		fc.chunk.EmitTwo(vm.OpMatchLocal, msgSlot, patIdx, fc.line)

		if err := fc.compileBranchBody(br.Body); err != nil {
			return err
		}
		fc.emit(vm.OpJump, 0)
		endJumps = append(endJumps, fc.chunk.OperandPos())

		cp.FailAddr = len(vm.ChunkCode(fc.chunk))
	}

	var noMatchEnd int
	if re.RecvElseBody() != nil {
		if err := fc.compileBranchBody(re.RecvElseBody()); err != nil {
			return err
		}
		fc.emit(vm.OpJump, 0)
		noMatchEnd = fc.chunk.OperandPos()
	} else {
		idx := fc.chunk.AddConstant(runtime.Atom("recv_clause"))
		fc.emit(vm.OpConstant, idx)
		fc.emit(vm.OpGetLocal, msgSlot)
		fc.emit(vm.OpTuple, 2)
		fc.emit(vm.OpRaise, 0)
	}

	afterAddr := len(vm.ChunkCode(fc.chunk))
	if hasAfter {
		if err := fc.compileBranchBody(afterBody); err != nil {
			return err
		}
	} else {
		idx := fc.chunk.AddConstant(runtime.Atom("recv_clause"))
		fc.emit(vm.OpConstant, idx)
		fc.emit(vm.OpGetLocal, msgSlot)
		fc.emit(vm.OpTuple, 2)
		fc.emit(vm.OpRaise, 0)
	}

	if hasAfter {
		fc.chunk.PatchOperand(afterOperandPos, afterAddr)
	}

	endAddr := len(vm.ChunkCode(fc.chunk))
	for _, pos := range endJumps {
		fc.chunk.PatchOperand(pos, endAddr)
	}
	if noMatchEnd != 0 {
		fc.chunk.PatchOperand(noMatchEnd, endAddr)
	}
	return nil
}

// ---- компиляция паттернов ----

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
			Kind: vm.PatCtor, Tag: p.CtorName(), Subs: subs,
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
			restSlot = fc.declareLocal(p.ListRestName())
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
		slot := fc.declareLocal(p.AsName())
		return &vm.CompiledPattern{
			Kind: vm.PatAs, Inner: inner, AsSlot: slot,
		}, nil

	case ast.PatternWildcard:
		return &vm.CompiledPattern{Kind: vm.PatWildcard}, nil
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

// Image возвращает собранный ProgramImage (для REPL, Sprint 6.2).
func (c *Compiler) Image() *ProgramImage { return c.image }

// CompileReplLine компилирует одну REPL-строку как
//
//	fn (<names...>) -> <stmt>
//
// где `names` — все видимые в REPL имена в порядке появления. Параметры
// становятся локалами этой функции; лямбды внутри тела захватывают их
// как upvalues (closure snapshot, N12, §11.4).
//
// Возвращает скомпилированную функцию, имя нового связывания (пусто,
// если стейтмент — выражение), и ошибку.
func (c *Compiler) CompileReplLine(names []string, s ast.Stmt) (*vm.Function, string, error) {
	fc := c.newFuncCompiler(nil)
	fc.prefix = "__repl__$"

	for _, n := range names {
		fc.declareLocal(n)
	}

	var newName string
	switch st := s.(type) {
	case ast.LetBind:
		ip, ok := st.Pat().(ast.IdentPattern)
		if !ok {
			return nil, "", fmt.Errorf("repl: only simple `name = expr` bindings are supported")
		}
		newName = ip.IdentName()
		if err := fc.compileExpr(st.Val()); err != nil {
			return nil, "", err
		}
	case ast.ExprStmt:
		if err := fc.compileExpr(st.ExprValue()); err != nil {
			return nil, "", err
		}
	default:
		return nil, "", fmt.Errorf("repl: unsupported statement %T", s)
	}

	fc.emit(vm.OpReturn, 0)
	fn := &vm.Function{Name: "__repl__", Arity: len(names), Chunk: fc.chunk}
	return fn, newName, nil
}
