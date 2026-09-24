package ast

// Экспортируемые интерфейсы-аксессоры для компилятора/интерпретатора.
// Методы живут на приватных типах этого же пакета; наружу отдаются
// только интерфейсы. Это позволяет Треку C читать AST без ломки
// инкапсуляции и без дублирования структуры.

// --- выражения ---

type BinaryExpr interface {
	Expr
	OpStr() string
	Left() Expr
	Right() Expr
}

func (e *binaryExpr) OpStr() string { return e.op }
func (e *binaryExpr) Left() Expr    { return e.left }
func (e *binaryExpr) Right() Expr   { return e.right }

type UnaryExpr interface {
	Expr
	OpStr() string
	Operand() Expr
}

func (e *unaryExpr) OpStr() string { return e.op }
func (e *unaryExpr) Operand() Expr { return e.expr }

type GroupingExpr interface {
	Expr
	Inner() Expr
}

func (e *groupingExpr) Inner() Expr { return e.expr }

type LiteralExpr interface {
	Expr
	ValueStr() string
}

func (e *literalExpr) ValueStr() string { return e.value }

type VariableExpr interface {
	Expr
	Name() string
}

func (e *variableExpr) Name() string { return e.name }

type CallExpr interface {
	Expr
	Callee() Expr
	Args() []Expr
}

func (e *callExpr) Callee() Expr { return e.callee }
func (e *callExpr) Args() []Expr { return e.args }

type MemberExpr interface {
	Expr
	Obj() Expr
	MemberName() string
}

func (e *memberExpr) Obj() Expr          { return e.obj }
func (e *memberExpr) MemberName() string { return e.name }

type IndexExpr interface {
	Expr
	Obj() Expr
	Index() Expr
}

func (e *indexExpr) Obj() Expr   { return e.obj }
func (e *indexExpr) Index() Expr { return e.index }

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

type AtomExpr interface {
	Expr
	AtomName() string
}

func (e *atomExpr) AtomName() string { return e.ident }

type DecimalExpr interface {
	Expr
	ValueStr() string
}

func (e *decimalExpr) ValueStr() string { return e.value }

type BytesExpr interface {
	Expr
	ValueStr() string
}

func (e *bytesExpr) ValueStr() string { return e.value }

type RegexExpr interface {
	Expr
	ValueStr() string
}

func (e *regexExpr) ValueStr() string { return e.value }

type LambdaShort interface {
	Expr
	ParamName() string
	Body() Expr
}

func (e *lambdaShortExpr) ParamName() string { return e.param }
func (e *lambdaShortExpr) Body() Expr        { return e.body }

type LambdaEmpty interface {
	Expr
	Body() Expr
}

func (e *lambdaEmptyExpr) Body() Expr { return e.body }

type LambdaFull interface {
	Expr
	ParamNames() []string
	BlockBody() *BlockStmt
}

func (e *lambdaFullExpr) ParamNames() []string  { return e.params }
func (e *lambdaFullExpr) BlockBody() *BlockStmt { return e.body }

// BlockStmt уже экспортирован; даём доступ к стейтментам.
func (b *BlockStmt) Body() []Stmt { return b.stmts }

// --- стейтменты ---

type LetBind interface {
	Stmt
	Pat() Pattern
	Val() Expr
}

func (s *letBind) Pat() Pattern { return s.pattern }
func (s *letBind) Val() Expr    { return s.value }

type ExprStmt interface {
	Stmt
	ExprValue() Expr
}

func (s *exprStmt) ExprValue() Expr { return s.expr }

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

type IdentPattern interface {
	Pattern
	IdentName() string
}

func (p *identPat) IdentName() string { return p.name }

type LiteralPattern interface {
	Pattern
	ValueStr() string
}

func (p *literalPat) ValueStr() string { return p.value }

// --- декларации ---

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
