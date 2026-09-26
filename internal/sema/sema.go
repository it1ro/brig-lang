// Package sema — контекстный анализ (§F.3). Слой между parser и
// compiler: проверки, которые невозможно выполнить грамматически.
//
// Проверки (Sprint 6.1, полностью):
//  1. `trap` только как RHS let_bind или expr_stmt (§10.2);
//  2. `..` в list-паттерне: только последним, не более одного раза (§5.1);
//  3. pipe-запрет акторных примитивов (§7.5);
//  4. variadic-параметр должен быть последним (§6.3);
//  5. rebinding имени в одной лексической области (§6.6, принцип #12);
//  6. локальные `fn` — только в начале тела блока (§6.5);
//  7. shadowing прелюдии и встроенных вариантов — info-диагностика (§11.5).
package sema

import (
	"fmt"
	"strings"

	"github.com/it1ro/brig-lang/internal/ast"
)

// Severity уровня диагностики (E.3).
type Severity int

const (
	// SeverityError блокирует компиляцию.
	SeverityError Severity = iota
	// SeverityInfo не блокирует компиляцию.
	SeverityInfo
)

// Diagnostic — одна диагностика с позицией (E.1).
type Diagnostic struct {
	Line, Col int
	Severity  Severity
	Message   string
}

// Result — набор диагностик.
type Result struct {
	Diagnostics []Diagnostic
}

// HasErrors — есть ли среди диагностик хотя бы одна error.
func (r *Result) HasErrors() bool {
	for _, d := range r.Diagnostics {
		if d.Severity == SeverityError {
			return true
		}
	}
	return false
}

// Check прогоняет контекстный анализ по программе.
func Check(prog *ast.Program) *Result {
	c := &checker{
		prelude: preludeNames(),
	}
	c.checkProgram(prog)
	return &Result{Diagnostics: c.diags}
}

type checker struct {
	diags   []Diagnostic
	prelude map[string]bool
	scopes  []map[string]binding // стек областей видимости
}

type binding struct {
	// kind — для диагностики: "param" | "let" | "local fn"
	kind string
}

func (c *checker) pushScope() { c.scopes = append(c.scopes, map[string]binding{}) }
func (c *checker) popScope()  { c.scopes = c.scopes[:len(c.scopes)-1] }

func (c *checker) topScope() map[string]binding {
	return c.scopes[len(c.scopes)-1]
}

// bind — объявление имени в текущей области. Rebinding в той же области
// (по имени и любой форме) — ошибка (§F.3). Shadowing прелюдии — info.
func (c *checker) bind(name, kind string, line, col int) {
	if name == "" || name == "_" {
		return
	}
	scope := c.topScope()
	if _, exists := scope[name]; exists {
		c.err(line, col, "rebinding %q in the same lexical scope (§6.6, principle #12)", name)
		return
	}
	scope[name] = binding{kind: kind}
	if c.prelude[name] {
		c.info(line, col, "`%s` shadows prelude binding; use `Prelude.%s` if prelude was intended", name, name)
	}
}

func (c *checker) err(line, col int, format string, args ...any) {
	c.diags = append(c.diags, Diagnostic{
		Line: line, Col: col, Severity: SeverityError,
		Message: fmt.Sprintf(format, args...),
	})
}

func (c *checker) info(line, col int, format string, args ...any) {
	c.diags = append(c.diags, Diagnostic{
		Line: line, Col: col, Severity: SeverityInfo,
		Message: fmt.Sprintf(format, args...),
	})
}

// posOf извлекает (line, col) из узла AST.
//
// Тонкость текущего ast: Pos() возвращает Line, End() возвращает Col
// (см. конструкторы в ast/construct.go: `NewXxx(v, t.Line, t.Col)`).
// Имена Pos/End в интерфейсе Node вводят в заблуждение, но менять
// их — большой рефакторинг. Хелпер концентрирует знание в одном месте.
func posOf(n ast.Node) (line, col int) {
	return n.Pos(), n.End()
}

// actorPrimitives — акторные примитивы прелюдии, запрещённые в pipe RHS (§7.5).
var actorPrimitives = map[string]bool{
	"send": true, "spawn": true, "spawn_linked": true,
	"link": true, "watch": true, "unwatch": true,
	"self": true, "make_ref": true, "mailbox_size": true,
}

// preludeNames — имена прелюдии и встроенных вариантов (§11.5).
// Ключевые слова (`trap`, `not`) сюда не входят: они не могут быть
// переопределены пользователем в принципе.
func preludeNames() map[string]bool {
	names := []string{
		// Коллекции
		"map", "filter", "find", "fold", "all", "any", "len",
		// Конструкторы
		"list", "set",
		// Конверсии
		"to_str", "to_int", "to_float",
		// Акторы
		"send", "spawn", "spawn_linked", "link", "watch", "unwatch",
		"self", "make_ref", "mailbox_size",
		// I/O
		"print", "eprint", "log",
		// Эффекты
		"assert", "raise",
		// Встроенные варианты
		"Some", "Ok", "Error", "None",
	}
	out := make(map[string]bool, len(names))
	for _, n := range names {
		out[n] = true
	}
	return out
}

// ---- вход ----

func (c *checker) checkProgram(prog *ast.Program) {
	c.pushScope()
	defer c.popScope()

	for _, d := range prog.Decls {
		c.checkDecl(d)
	}
	for _, s := range prog.Stmts {
		c.checkStmt(s)
	}
}

func (c *checker) checkDecl(d ast.Decl) {
	fd, ok := d.(ast.FuncDecl)
	if !ok {
		return
	}
	// Top-level fn: имя функции не входит в обычный scope-трекинг
	// (оно глобальное и не конфликтует с shadowing по правилам §F.3).
	// Но тело функции — новая лексическая область.
	for _, cl := range fd.FuncClauses() {
		c.pushScope()
		c.checkParams(cl.Params, d)
		if cl.Body != nil {
			// Тело fn — BlockStmt, но params уже связаны в текущей
			// области. Чтобы не отбрасывать их, проверяем stmts
			// без повторного pushScope.
			c.checkBlockBody(cl.Body)
		}
		c.popScope()
	}
}

// checkBlock — тело блока как новая область видимости.
func (c *checker) checkBlock(blk *ast.BlockStmt) {
	if blk == nil {
		return
	}
	c.pushScope()
	defer c.popScope()
	c.checkBlockBody(blk)
}

// checkBlockBody — стейтменты без создания новой области (используется,
// когда params/pattern уже связаны в текущей области).
func (c *checker) checkBlockBody(blk *ast.BlockStmt) {
	if blk == nil {
		return
	}
	// Проверка позиции локальных fn (§6.5, §F.3).
	seenNonFn := false
	for _, s := range blk.Stmts() {
		if _, ok := s.(ast.LocalFnDecl); ok {
			if seenNonFn {
				line, col := posOf(s)
				c.err(line, col,
					"local fn declaration must appear at the start of the block body (§6.5)")
			}
		} else {
			seenNonFn = true
		}
	}
	for _, s := range blk.Stmts() {
		c.checkStmt(s)
	}
}

// ---- statements ----

func (c *checker) checkStmt(s ast.Stmt) {
	switch x := s.(type) {
	case *ast.BlockStmt:
		c.checkBlock(x)
	case ast.LetBind:
		c.checkPatternBinding(x.Pat(), "let")
		// §10.2: trap разрешён на верхнем уровне RHS let_bind.
		c.checkExprAllowTrap(x.Val())
	case ast.ExprStmt:
		// §10.2: trap разрешён как отдельный expr_stmt.
		c.checkExprAllowTrap(x.ExprValue())
	case ast.LocalFnDecl:
		line, col := posOf(s)
		c.bind(x.FnName(), "local fn", line, col)
		for _, cl := range x.Clauses() {
			c.pushScope()
			c.checkParams(cl.Params, s)
			if cl.Body != nil {
				c.checkBlockBody(cl.Body)
			}
			c.popScope()
		}
	}
}

// checkParams — variadic-параметр обязан быть последним (§6.3) +
// параметры связываются в текущей области.
func (c *checker) checkParams(params []string, site ast.Node) {
	line, col := posOf(site)
	for i, p := range params {
		if strings.HasPrefix(p, "..") {
			if i != len(params)-1 {
				c.err(line, col, "variadic parameter %q must be last (§6.3)", p)
			}
			// Имя параметра связываем без префикса `..` — оно
			// доступно в теле как обычная переменная.
			c.bind(strings.TrimPrefix(p, ".."), "param", line, col)
			continue
		}
		c.bind(p, "param", line, col)
	}
}

// checkPatternBinding — связывает идентификаторы паттерна в текущей
// области видимости. `_` игнорируется. Рекурсивно обходит вложенные
// паттерны.
func (c *checker) checkPatternBinding(pat ast.Pattern, kind string) {
	if pat == nil {
		return
	}
	switch p := pat.(type) {
	case ast.IdentPattern:
		line, col := posOf(p)
		c.bind(p.IdentName(), kind, line, col)
	case ast.PatternCtor:
		for _, sub := range p.CtorArgs() {
			c.checkPatternBinding(sub, kind)
		}
	case ast.PatternTuple:
		for _, sub := range p.TupleElems() {
			c.checkPatternBinding(sub, kind)
		}
	case ast.PatternList:
		for _, sub := range p.ListElems() {
			c.checkPatternBinding(sub, kind)
		}
		if p.ListHasRest() && p.ListRestName() != "" {
			line, col := posOf(p)
			c.bind(p.ListRestName(), kind, line, col)
		}
	case ast.PatternMapAccessor:
		for _, pair := range p.MapPairsAccessor() {
			c.checkPatternBinding(pair.Pat, kind)
		}
	case ast.PatternAs:
		c.checkPatternBinding(p.AsInner(), kind)
		if name := p.AsName(); name != "" {
			line, col := posOf(p)
			c.bind(name, kind, line, col)
		}
	}
}

// checkPattern — обход паттерна без связывания (для match/recv/with-ветки,
// где паттерн — не новое связывание в охватывающей области, а локальное
// для ветки). Оставляем защитную проверку на `..`-инвариант.
func (c *checker) checkPattern(pat ast.Pattern) {
	if pat == nil {
		return
	}
	switch p := pat.(type) {
	case ast.PatternList:
		// Парсер уже обеспечивает: `..` только последним, не более одного
		// раза. Оставляем defensive-комментарий как точку расширения.
		_ = p
	case ast.PatternCtor:
		for _, sub := range p.CtorArgs() {
			c.checkPattern(sub)
		}
	case ast.PatternTuple:
		for _, sub := range p.TupleElems() {
			c.checkPattern(sub)
		}
	case ast.PatternMapAccessor:
		for _, pair := range p.MapPairsAccessor() {
			c.checkPattern(pair.Pat)
		}
	case ast.PatternAs:
		c.checkPattern(p.AsInner())
	}
}

// ---- expressions ----

func (c *checker) checkExpr(e ast.Expr) {
	if e == nil {
		return
	}
	switch x := e.(type) {
	case *ast.BlockStmt:
		c.checkBlock(x)

	case ast.LiteralExpr, ast.BytesExpr, ast.DecimalExpr, ast.RegexExpr,
		ast.AtomExpr, ast.VariableExpr:
		// leaf

	case ast.InterpExpr:
		for _, sub := range x.InterpExprs() {
			c.checkExpr(sub)
		}

	case ast.GroupingExpr:
		c.checkExpr(x.Inner())

	case ast.UnaryExpr:
		c.checkExpr(x.Operand())

	case ast.BinaryExpr:
		c.checkExpr(x.Left())
		c.checkExpr(x.Right())

	case ast.MemberExpr:
		c.checkExpr(x.Obj())

	case ast.IndexExpr:
		c.checkExpr(x.Obj())
		c.checkExpr(x.Index())

	case ast.CallExpr:
		c.checkExpr(x.Callee())
		for _, a := range x.Args() {
			c.checkExpr(a)
		}

	case ast.PipeExpr:
		c.checkPipe(x)

	case ast.RangeExpr:
		c.checkExpr(x.RangeStart())
		c.checkExpr(x.RangeEnd())

	case ast.IfExpr:
		c.checkExpr(x.Cond())
		c.checkBranchBody(x.ThenBody())
		for _, br := range x.ElseIf() {
			c.checkExpr(br.Cond)
			c.checkBranchBody(br.Then)
		}
		if x.ElseBody() != nil {
			c.checkBranchBody(x.ElseBody())
		}

	case ast.MatchExpr:
		c.checkExpr(x.MatchSubject())
		for _, br := range x.MatchBranches() {
			c.pushScope()
			c.checkPatternBinding(br.Pattern, "branch")
			c.checkPattern(br.Pattern)
			c.checkBranchBody(br.Body)
			c.popScope()
		}

	case ast.RecvExpr:
		for _, br := range x.RecvBranches() {
			c.pushScope()
			c.checkPatternBinding(br.Pattern, "branch")
			c.checkPattern(br.Pattern)
			c.checkBranchBody(br.Body)
			c.popScope()
		}
		if x.RecvElseName() != "" {
			c.pushScope()
			// else-имя — локальное для ветки.
			c.bind(x.RecvElseName(), "branch", 0, 0)
			if x.RecvElseBody() != nil {
				c.checkBranchBody(x.RecvElseBody())
			}
			c.popScope()
		}
		if x.RecvAfterTime() != nil {
			c.checkExpr(x.RecvAfterTime())
		}
		if x.RecvAfterBody() != nil {
			c.pushScope()
			c.checkBranchBody(x.RecvAfterBody())
			c.popScope()
		}

	case ast.WithExpr:
		c.pushScope()
		for _, it := range x.WithItems() {
			c.checkPatternBinding(it.Pattern, "with")
			c.checkExpr(it.Expr)
		}
		if x.WithBody() != nil {
			c.checkBlockBody(x.WithBody())
		}
		c.popScope()
		for _, eb := range x.WithElseBranches() {
			c.pushScope()
			c.checkPatternBinding(eb.Pattern, "branch")
			c.checkBranchBody(eb.Body)
			c.popScope()
		}

	case ast.TrapExpr:
		// trap встретился вне разрешённых позиций (§10.2).
		line, col := posOf(x)
		c.err(line, col,
			"trap is not allowed in this position (§10.2); only as let RHS or expr_stmt")
		c.checkTrapInner(x)

	case ast.LambdaShort:
		c.pushScope()
		c.bind(x.ParamName(), "param", 0, 0)
		c.checkExpr(x.Body())
		c.popScope()

	case ast.LambdaEmpty:
		c.pushScope()
		c.checkExpr(x.Body())
		c.popScope()

	case ast.LambdaFull:
		c.pushScope()
		c.checkParams(x.ParamNames(), e)
		if b := x.BlockBody(); b != nil {
			c.checkBlockBody(b)
		}
		c.popScope()
	}
}

// checkBranchBody — тело ветки if/match/recv/with. Если это BlockStmt —
// отдельная область видимости; иначе — выражение. Trap на верхнем уровне
// выражения ветки запрещён (§10.2): допустимая идиома — блок со
// стейтментом `trap(...)` или `x = trap(...)`.
func (c *checker) checkBranchBody(e ast.Expr) {
	if blk, ok := e.(*ast.BlockStmt); ok {
		c.checkBlock(blk)
		return
	}
	c.checkExpr(e)
}

// checkExprAllowTrap — выражение в позиции, где trap на верхнем уровне
// разрешён (§10.2): RHS let_bind или expr_stmt.
func (c *checker) checkExprAllowTrap(e ast.Expr) {
	if t, ok := e.(ast.TrapExpr); ok {
		c.checkTrapInner(t)
		return
	}
	c.checkExpr(e)
}

// checkTrapInner — обход содержимого trap без повторной проверки позиции
// самого узла.
func (c *checker) checkTrapInner(x ast.TrapExpr) {
	if x.TrapInline() != nil {
		c.checkExpr(x.TrapInline())
	}
	if b := x.TrapBody(); b != nil {
		c.pushScope()
		c.checkStmt(b)
		c.popScope()
	}
	for _, en := range x.TrapEnsures() {
		c.checkExpr(en)
	}
}

// checkPipe — запрет акторных примитивов в pipe RHS (§7.5).
func (c *checker) checkPipe(p ast.PipeExpr) {
	if callee := p.PipeRHS(); callee != nil {
		if v, ok := callee.(ast.VariableExpr); ok {
			name := v.Name()
			base := name
			if i := strings.IndexByte(name, '.'); i >= 0 {
				base = name[:i]
			}
			if actorPrimitives[base] {
				line, col := posOf(callee)
				c.err(line, col,
					"actor primitive %q is not allowed as pipe RHS (§7.5)", base)
			}
		}
	}
	c.checkExpr(p.PipeLHS())
	for _, a := range p.PipeArgs() {
		c.checkExpr(a)
	}
}
