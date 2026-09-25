package ast

// Экспортируемые интерфейсы-аксессоры для компилятора/интерпретатора.
// Методы живут на приватных типах этого же пакета; наружу отдаются
// только интерфейсы. Это позволяет Треку C читать AST без ломки
// инкапсуляции и без дублирования структуры.
//
// v0.4.7: добавлен TrapExpr.
// v0.4.8: добавлены RecvExpr и Pattern* (нужны компилятору акторов).
// RecvBranchArg уже объявлен в construct.go — здесь не дублируется.

// --- выражения ---

// BinaryExpr — доступ к бинарному выражению.
type BinaryExpr interface {
	Expr
	OpStr() string
	Left() Expr
	Right() Expr
}

func (e *binaryExpr) OpStr() string { return e.op }
func (e *binaryExpr) Left() Expr    { return e.left }
func (e *binaryExpr) Right() Expr   { return e.right }

// UnaryExpr — доступ к унарному выражению.
type UnaryExpr interface {
	Expr
	OpStr() string
	Operand() Expr
}

func (e *unaryExpr) OpStr() string { return e.op }
func (e *unaryExpr) Operand() Expr { return e.expr }

// GroupingExpr — доступ к группированному выражению.
type GroupingExpr interface {
	Expr
	Inner() Expr
}

func (e *groupingExpr) Inner() Expr { return e.expr }

// LiteralExpr — доступ к литеральному выражению.
type LiteralExpr interface {
	Expr
	ValueStr() string
}

func (e *literalExpr) ValueStr() string { return e.value }

// VariableExpr — доступ к переменной выражению.
type VariableExpr interface {
	Expr
	Name() string
}

func (e *variableExpr) Name() string { return e.name }

// CallExpr — доступ к вызову функции.
type CallExpr interface {
	Expr
	Callee() Expr
	Args() []Expr
}

func (e *callExpr) Callee() Expr { return e.callee }
func (e *callExpr) Args() []Expr { return e.args }

// MemberExpr — доступ к выражению выбора полей/атрибутов.
type MemberExpr interface {
	Expr
	Obj() Expr
	MemberName() string
}

func (e *memberExpr) Obj() Expr          { return e.obj }
func (e *memberExpr) MemberName() string { return e.name }

// IndexExpr — доступ к индексному выражению.
type IndexExpr interface {
	Expr
	Obj() Expr
	Index() Expr
}

func (e *indexExpr) Obj() Expr   { return e.obj }
func (e *indexExpr) Index() Expr { return e.index }

// IfExpr — доступ к условному выражению if.
type IfExpr interface {
	Expr
	Cond() Expr
	ThenBody() Expr
	ElseIf() []IfBranch
	ElseBody() Expr
}

func (e *ifExpr) Cond() Expr     { return e.cond }
func (e *ifExpr) ThenBody() Expr { return e.thenBody }
func (e *ifExpr) ElseBody() Expr { return e.elseBody }
func (e *ifExpr) ElseIf() []IfBranch {
	out := make([]IfBranch, 0, len(e.elseIf))
	for _, b := range e.elseIf {
		out = append(out, IfBranch{Cond: b.cond, Then: b.thenBody})
	}
	return out
}

// AtomExpr — доступ к атому выражения.
type AtomExpr interface {
	Expr
	AtomName() string
}

func (e *atomExpr) AtomName() string { return e.ident }

// DecimalExpr — доступ к десятичному литералу.
type DecimalExpr interface {
	Expr
	ValueStr() string
}

func (e *decimalExpr) ValueStr() string { return e.value }

// BytesExpr — доступ к байтовому литералу.
type BytesExpr interface {
	Expr
	ValueStr() string
}

func (e *bytesExpr) ValueStr() string { return e.value }

// RegexExpr — доступ к регулярному выражению.
type RegexExpr interface {
	Expr
	ValueStr() string
}

func (e *regexExpr) ValueStr() string { return e.value }

// LambdaShort — доступ к короткой лямбде.
type LambdaShort interface {
	Expr
	ParamName() string
	Body() Expr
}

func (e *lambdaShortExpr) ParamName() string { return e.param }
func (e *lambdaShortExpr) Body() Expr        { return e.body }

// LambdaEmpty — доступ к лямбде с пустыми параметрами.
type LambdaEmpty interface {
	Expr
	Body() Expr
}

func (e *lambdaEmptyExpr) Body() Expr { return e.body }

// LambdaFull — доступ к полной лямбде.
type LambdaFull interface {
	Expr
	ParamNames() []string
	BlockBody() *BlockStmt
}

func (e *lambdaFullExpr) ParamNames() []string { return e.params }

// BlockBody возвращает тело блока полной лямбды.
func (e *lambdaFullExpr) BlockBody() *BlockStmt { return e.body }

// Body возвращает стейтменты блока.
func (b *BlockStmt) Body() []Stmt { return b.stmts }

// --- стейтменты ---

// LetBind — доступ к привязке let.
type LetBind interface {
	Stmt
	Pat() Pattern
	Val() Expr
}

func (s *letBind) Pat() Pattern { return s.pattern }
func (s *letBind) Val() Expr    { return s.value }

// ExprStmt — доступ к выражению-стейтменту.
type ExprStmt interface {
	Stmt
	ExprValue() Expr
}

func (s *exprStmt) ExprValue() Expr { return s.expr }

// LocalFnDecl — доступ к локальной декларации функции.
type LocalFnDecl interface {
	Stmt
	FnName() string
	Clauses() []LocalFnClauseArg
}

func (s *localFnDecl) FnName() string { return s.name }
func (s *localFnDecl) Clauses() []LocalFnClauseArg {
	out := make([]LocalFnClauseArg, 0, len(s.clauses))
	for _, c := range s.clauses {
		out = append(out, LocalFnClauseArg{
			Guard: c.guard, Params: c.params, Body: c.body,
		})
	}
	return out
}

// --- паттерны ---

// IdentPattern — доступ к идентификатору-паттерну.
type IdentPattern interface {
	Pattern
	IdentName() string
}

func (p *identPat) IdentName() string { return p.name }

// LiteralPattern — доступ к литеральному паттерну.
type LiteralPattern interface {
	Pattern
	ValueStr() string
}

func (p *literalPat) ValueStr() string { return p.value }

// PatternWildcard — `_` в паттерне.
type PatternWildcard interface {
	Pattern
}

// PatternCtor — доступ к конструкторному паттерну.
type PatternCtor interface {
	Pattern
	CtorName() string
	CtorArgs() []Pattern
}

func (p *constructorPat) CtorName() string { return p.name }
func (p *constructorPat) CtorArgs() []Pattern {
	out := make([]Pattern, len(p.fields))
	for i := range p.fields {
		out[i] = p.fields[i].pattern
	}
	return out
}

// PatternTuple — доступ к кортежному паттерну.
type PatternTuple interface {
	Pattern
	TupleElems() []Pattern
}

func (p *tuplePattern) TupleElems() []Pattern { return p.patterns }

// PatternAs — доступ к паттерну as.
type PatternAs interface {
	Pattern
	AsInner() Pattern
	AsName() string
}

func (p *asPat) AsInner() Pattern { return p.pattern }
func (p *asPat) AsName() string   { return p.ident }

// --- декларации ---

// FuncDecl — доступ к декларации функции.
type FuncDecl interface {
	Decl
	FnName() string
	FuncClauses() []FnClauseArg
}

func (d *funcDecl) FnName() string { return d.name }
func (d *funcDecl) FuncClauses() []FnClauseArg {
	out := make([]FnClauseArg, 0, len(d.clauses))
	for _, c := range d.clauses {
		out = append(out, FnClauseArg{
			Guard: c.guard, Params: c.params, Body: c.body,
		})
	}
	return out
}

// --- trap (v0.4.7, §10.2/§10.3) ---

// TrapExpr — экспортируемый аксессор к trap-выражению.
//
// Инлайн-форма: TrapInline() != nil, TrapBody() == nil.
// Блочная форма: TrapInline() == nil, TrapBody() != nil.
type TrapExpr interface {
	Expr
	// TrapInline возвращает выражение инлайн-формы trap(expr) или nil.
	TrapInline() Expr
	// TrapBody возвращает тело блочной формы (обычно *BlockStmt) или nil.
	TrapBody() Stmt
	// TrapEnsures возвращает ensure-выражения в текстовом порядке.
	// Runtime выполняет их LIFO (§10.3).
	TrapEnsures() []Expr
}

func (e *trapExpr) TrapInline() Expr { return e.expr }

func (e *trapExpr) TrapBody() Stmt {
	if e.body == nil {
		return nil
	}
	return e.body.stmt
}

func (e *trapExpr) TrapEnsures() []Expr {
	out := make([]Expr, len(e.ensures))
	for i := range e.ensures {
		out[i] = e.ensures[i].expr
	}
	return out
}

// --- recv (v0.4.8, §12.4) ---
//
// RecvBranchArg уже объявлен в construct.go — здесь только аксессор.

// RecvExpr — аксессор к recv-выражению для компилятора.
type RecvExpr interface {
	Expr
	RecvBranches() []RecvBranchArg
	RecvElseName() string
	RecvElseBody() Expr
	RecvAfterTime() Expr
	RecvAfterBody() Expr
}

func (e *recvExpr) RecvBranches() []RecvBranchArg {
	out := make([]RecvBranchArg, 0, len(e.branches))
	for _, b := range e.branches {
		out = append(out, RecvBranchArg{Pattern: b.pattern, Body: b.expr})
	}
	return out
}

func (e *recvExpr) RecvElseName() string { return e.elseName }
func (e *recvExpr) RecvElseBody() Expr   { return e.elseBody }
func (e *recvExpr) RecvAfterTime() Expr  { return e.afterTime }
func (e *recvExpr) RecvAfterBody() Expr  { return e.afterBody }

// --- v0.4.8: list/map patterns ---

// PatternList — аксессор к listPattern.
type PatternList interface {
	Pattern
	ListElems() []Pattern
	ListHasRest() bool
	ListRestName() string
}

func (p *listPattern) ListElems() []Pattern { return p.patterns }
func (p *listPattern) ListHasRest() bool    { return p.hasRest }
func (p *listPattern) ListRestName() string { return p.restName }

// PatternMapAccessor — аксессор к mapPattern.
type PatternMapAccessor interface {
	Pattern
	MapPairsAccessor() []MapPairArg
}

func (p *mapPattern) MapPairsAccessor() []MapPairArg {
	out := make([]MapPairArg, 0, len(p.pairs))
	for _, pair := range p.pairs {
		out = append(out, MapPairArg{Key: pair.key, Pat: pair.pat})
	}
	return out
}
