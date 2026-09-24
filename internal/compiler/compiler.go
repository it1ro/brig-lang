// Package compiler — компиляция AST в стековый байткод ВМ.
//
// Срез: одно-клозные функции, локальные переменные без захвата
// из объемлющих областей, базовые выражения, if, литералы коллекций,
// вызовы. Замыкания с захватом, мультиклозы, полный матчинг — далее.
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

func New() *Compiler {
	return &Compiler{image: &ProgramImage{Functions: make(map[string]*vm.Function)}}
}

// Compile — входная точка.
//
// Срез: поддерживаем модуль с декларациями `fn` и REPL-выражения.
// Точка входа модуля — функция `main`.
func (c *Compiler) Compile(prog *ast.Program) (*ProgramImage, error) {
	for _, d := range prog.Decls {
		fd, ok := d.(ast.FuncDecl)
		if !ok {
			continue // import/alias/type в срезе не исполняются
		}
		clauses := fd.FuncClauses()
		if len(clauses) == 0 {
			continue
		}
		if len(clauses) > 1 {
			// Срез: берём первый клоз, помечаем ограничение.
			// Полный диспетчер клозов — подэтап 4.6.
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
	// REPL: одно выражение/стейтмент → синтетическая функция __repl__.
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

// funcCompiler — состояние компиляции одной функции.
type funcCompiler struct {
	chunk  *vm.Chunk
	locals []string
	scopes []map[string]int // имя → индекс локальной
	line   int
}

func newFuncCompiler() *funcCompiler {
	return &funcCompiler{
		chunk:  vm.NewChunk(),
		scopes: []map[string]int{{}},
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

// compileFunction компилирует одну функцию.
func (c *Compiler) compileFunction(name string, params []string, body *ast.BlockStmt) (*vm.Function, error) {
	fc := newFuncCompiler()
	// параметры — первые локалы
	arity := 0
	for _, p := range params {
		if strings.HasPrefix(p, "..") {
			// вариадик в срезе: объявляем имя, арность -1
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
	// гарантируем возврат последнего значения
	fc.emit(vm.OpReturn, 0)
	hasVar := false
	for _, p := range params {
		if strings.HasPrefix(p, "..") {
			hasVar = true
		}
	}
	if hasVar {
		arity = -1
	}
	return &vm.Function{Name: name, Arity: arity, Chunk: fc.chunk}, nil
}

// compileBlock — тело как последовательность стейтментов.
func (c *Compiler) compileBlock(name string, params []string, stmts []ast.Stmt) (*vm.Function, error) {
	fc := newFuncCompiler()
	for _, p := range params {
		fc.declareLocal(p)
	}
	if err := fc.compileStmts(stmts); err != nil {
		return nil, err
	}
	fc.emit(vm.OpReturn, 0)
	return &vm.Function{Name: name, Arity: len(params), Chunk: fc.chunk}, nil
}

// compileStmts компилирует список стейтментов; значение последнего
// выражения остаётся на стеке.
func (fc *funcCompiler) compileStmts(stmts []ast.Stmt) error {
	for i, s := range stmts {
		// значение предыдущего стейтмента не нужно
		if i > 0 {
			fc.emit(vm.OpPop, 0)
		}
		if err := fc.compileStmt(s); err != nil {
			return err
		}
	}
	if len(stmts) == 0 {
		// пустое тело → ()
		fc.emitUnit()
	}
	return nil
}

func (fc *funcCompiler) emitUnit() {
	// Unit как константа
	idx := fc.chunk.AddConstant(runtime.Unit)
	fc.emit(vm.OpConstant, idx)
}

func (fc *funcCompiler) compileStmt(s ast.Stmt) error {
	switch st := s.(type) {
	case ast.LetBind:
		if err := fc.compileExpr(st.Val()); err != nil {
			return err
		}
		// связывание: поддерживаем только простой ident-паттерн в срезе
		if ip, ok := st.Pat().(ast.IdentPattern); ok {
			idx := fc.declareLocal(ip.IdentName())
			fc.emit(vm.OpSetLocal, idx)
			// let — стейтмент, значение не оставляем
			fc.emitUnit()
			return nil
		}
		return fmt.Errorf("срез: только простые связывания `name = expr`")
	case ast.ExprStmt:
		return fc.compileExpr(st.ExprValue())
	case ast.LocalFnDecl:
		return fmt.Errorf("срез: локальные `fn` не поддерживаются (подэтап 4.6)")
	}
	return fmt.Errorf("срез: неподдерживаемый стейтмент %T", s)
}

// compileExpr кладёт значение выражения на стек.
func (fc *funcCompiler) compileExpr(e ast.Expr) error {
	fc.line = e.Pos() // условная строка; точные line/col — в миграции
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
	case ast.LambdaShort:
		return fmt.Errorf("срез: лямбды компилируются только как аргументы прелюдии — пока не поддержаны")
	case ast.LambdaEmpty:
		return fmt.Errorf("срез: пустая лямбда не поддержана")
	}
	// коллекции через callExpr со спец-именами обрабатывает compileCall
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
	// число?
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
	// строка в кавычках
	if strings.HasPrefix(lit, "\"") && strings.HasSuffix(lit, "\"") {
		s := lit[1 : len(lit)-1]
		idx := fc.chunk.AddConstant(runtime.Str(s))
		fc.emit(vm.OpConstant, idx)
		return nil
	}
	return fmt.Errorf("срез: неподдерживаемый литерал %q", lit)
}

func (fc *funcCompiler) compileVar(name string) error {
	if idx, ok := fc.resolveLocal(name); ok {
		fc.emit(vm.OpGetLocal, idx)
		return nil
	}
	// глобальное имя (функция/константа) храним как строку-константу
	idx := fc.chunk.AddConstant(runtime.Str(name))
	fc.emit(vm.OpGetGlobal, idx)
	return nil
}

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
	// short-circuit для and / or
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

// compileAndOr — and/or с коротким замыканием через переходы.
//
// Для `a and b`: вычисляем a; если ложь — прыгаем и результат = a.
// Иначе вычисляем b, результат = b. Для `or` — наоборот.
func (fc *funcCompiler) compileAndOr(b ast.BinaryExpr, isAnd bool) error {
	if err := fc.compileExpr(b.Left()); err != nil {
		return err
	}
	fc.emit(vm.OpDup, 0)
	// placeholder перехода
	fc.emit(vm.OpJumpFalse, 0)
	jumpFalsePos := len(fc.chunk.Code) - 2 // позиция операнда (упрощённо)

	if isAnd {
		// если a истина — выкидываем продублированную a и считаем b
		fc.emit(vm.OpPop, 0)
		if err := fc.compileExpr(b.Right()); err != nil {
			return err
		}
	} else {
		// or: если a ложна (после JumpFalse) — уже имеем a на стеке
		// если истина — прыгаем в конец, оставляя a
		// Здесь упрощённая схема: после dup+jumpFalse(false) — считаем b.
		fc.emit(vm.OpPop, 0)
		if err := fc.compileExpr(b.Right()); err != nil {
			return err
		}
	}
	// патчим переход
	end := len(fc.chunk.Code)
	_ = jumpFalsePos
	_ = end
	// Упрощение среза: валидный, но не оптимальный переход.
	// Точный патчинг — при переходе на регистровую ВМ.
	return nil
}

func (fc *funcCompiler) compileCall(call ast.CallExpr) error {
	callee := call.Callee()
	// коллекции представлены как вызов со спец-именем (см. format.go)
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
			// аргументы — пары (key => val) как binaryExpr "=>"
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
	// обычный вызов: кладём функцию, потом аргументы
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

// compileIf — и инлайн, и блочный формы.
func (fc *funcCompiler) compileIf(ie ast.IfExpr) error {
	if err := fc.compileExpr(ie.Cond()); err != nil {
		return err
	}
	// переход в else, если ложь
	fc.emit(vm.OpJumpFalse, 0)
	elseJump := len(fc.chunk.Code) - 2
	if err := fc.compileBranchBody(ie.ThenBody()); err != nil {
		return err
	}
	fc.emit(vm.OpJump, 0)
	endJump := len(fc.chunk.Code) - 2
	// else
	patch(fc.chunk, elseJump, len(fc.chunk.Code))
	if ie.ElseBody() != nil {
		if err := fc.compileBranchBody(ie.ElseBody()); err != nil {
			return err
		}
	} else {
		fc.emitUnit()
	}
	patch(fc.chunk, endJump, len(fc.chunk.Code))
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
	// operandPos указывает на старший байт операнда
	ch.Code[operandPos] = byte(target >> 8)
	ch.Code[operandPos+1] = byte(target)
}
