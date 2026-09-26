package ast

import (
	"bytes"
	"fmt"
)

// binaryExpr — бинарное выражение (a op b).
type binaryExpr struct {
	posEnd
	op    string
	left  Expr
	right Expr
}

func (e *binaryExpr) IsExpression() bool { return true }
func (e *binaryExpr) IsStatement() bool  { return false }
func (e *binaryExpr) String() string {
	return fmt.Sprintf("(%s %s %s)", e.left, e.op, e.right)
}

// unaryExpr — унарное выражение: -a, not a, ..xs.
type unaryExpr struct {
	posEnd
	op   string
	expr Expr
}

func (e *unaryExpr) IsExpression() bool { return true }
func (e *unaryExpr) IsStatement() bool  { return false }
func (e *unaryExpr) String() string {
	return fmt.Sprintf("(%s %s)", e.op, e.expr)
}

// groupingExpr — сгруппированное выражение: (expr).
type groupingExpr struct {
	posEnd
	expr Expr
}

func (e *groupingExpr) IsExpression() bool { return true }
func (e *groupingExpr) IsStatement() bool  { return false }
func (e *groupingExpr) String() string {
	return fmt.Sprintf("(%s)", e.expr)
}

// literalExpr — литеральное значение (число, строка, bool, unit "()").
type literalExpr struct {
	posEnd
	value string
}

func (e *literalExpr) IsExpression() bool { return true }
func (e *literalExpr) IsStatement() bool  { return false }
func (e *literalExpr) String() string     { return e.value }

// variableExpr — переменная или имя конструктора.
type variableExpr struct {
	posEnd
	name string
}

func (e *variableExpr) IsExpression() bool { return true }
func (e *variableExpr) IsStatement() bool  { return false }
func (e *variableExpr) String() string     { return e.name }

// assignExpr — привязка (x = expr).
type assignExpr struct {
	posEnd
	name  string
	value Expr
}

func (e *assignExpr) IsExpression() bool { return true }
func (e *assignExpr) IsStatement() bool  { return false }
func (e *assignExpr) String() string {
	return fmt.Sprintf("%s = %s", e.name, e.value)
}

// callExpr — вызов функции/конструктора.
type callExpr struct {
	posEnd
	callee Expr
	args   []Expr
}

func (e *callExpr) IsExpression() bool { return true }
func (e *callExpr) IsStatement() bool  { return false }
func (e *callExpr) String() string {
	if len(e.args) == 0 {
		return fmt.Sprintf("%s()", e.callee)
	}
	return fmt.Sprintf("%s(%s)", e.callee, argsToString(e.args))
}

func argsToString(args []Expr) string {
	if len(args) == 0 {
		return ""
	}
	buf := bytes.Buffer{}
	buf.WriteString(args[0].String())
	for _, a := range args[1:] {
		buf.WriteString(", ")
		buf.WriteString(a.String())
	}
	return buf.String()
}

// pipeExpr — конвейер: expr |> callee(args).
type pipeExpr struct {
	posEnd
	expr   Expr
	callee Expr
	args   []Expr
}

func (e *pipeExpr) IsExpression() bool { return true }
func (e *pipeExpr) IsStatement() bool  { return false }
func (e *pipeExpr) String() string {
	if len(e.args) == 0 {
		return fmt.Sprintf("%s |> %s", e.expr, e.callee)
	}
	return fmt.Sprintf("%s |> %s(%s)", e.expr, e.callee, argsToString(e.args))
}

// memberExpr — доступ к члену: obj.name.
type memberExpr struct {
	posEnd
	obj  Expr
	name string
}

func (e *memberExpr) IsExpression() bool { return true }
func (e *memberExpr) IsStatement() bool  { return false }
func (e *memberExpr) String() string     { return fmt.Sprintf("%s.%s", e.obj, e.name) }

// indexExpr — индексация: obj[idx].
type indexExpr struct {
	posEnd
	obj   Expr
	index Expr
}

func (e *indexExpr) IsExpression() bool { return true }
func (e *indexExpr) IsStatement() bool  { return false }
func (e *indexExpr) String() string     { return fmt.Sprintf("%s[%s]", e.obj, e.index) }

// ifExpr — if-выражение.
type ifExpr struct {
	posEnd
	cond     Expr
	thenBody Expr
	elseIf   []ifExpr
	elseBody Expr
}

func (e *ifExpr) IsExpression() bool { return true }
func (e *ifExpr) IsStatement() bool  { return false }
func (e *ifExpr) String() string {
	if e.elseBody == nil {
		return fmt.Sprintf("(if %s %s)", e.cond, e.thenBody)
	}
	return fmt.Sprintf("(if %s %s %s)", e.cond, e.thenBody, e.elseBody)
}

// matchExpr — match-выражение.
type matchExpr struct {
	posEnd
	expr     Expr
	branches []matchBranch
}

type matchBranch struct {
	pattern Pattern
	expr    Expr
}

func (e *matchExpr) IsExpression() bool { return true }
func (e *matchExpr) IsStatement() bool  { return false }
func (e *matchExpr) String() string     { return fmt.Sprintf("(match %s ...)", e.expr) }

// recvExpr — recv-выражение.
//
// Поля после рефакторинга:
//
//	elseName  — имя из "else <name>"; "" если клаузы нет;
//	elseBody  — тело else (BlockStmt);
//	afterTime — выражение таймаута в "after N ->"; nil если клаузы нет;
//	afterBody — тело after.
type recvExpr struct {
	posEnd
	branches  []recvBranch
	elseName  string
	elseBody  Expr
	afterTime Expr
	afterBody Expr
}

type recvBranch struct {
	pattern Pattern
	guard   Expr
	expr    Expr
}

func (e *recvExpr) IsExpression() bool { return true }
func (e *recvExpr) IsStatement() bool  { return false }
func (e *recvExpr) String() string {
	return fmt.Sprintf("(recv %d branches, else=%q, after=%v)",
		len(e.branches), e.elseName, e.afterBody != nil)
}

// withExpr — with-выражение.
//
// Поля после рефакторинга:
//
//	items        — bind_stmt (pattern <- expr);
//	body         — оставшиеся стейтменты тела (может быть nil);
//	elseBranches — ветки with-else.
type withExpr struct {
	posEnd
	items        []withItem
	body         *BlockStmt
	elseBranches []withElseBranch
}

type withItem struct {
	pattern Pattern
	expr    Expr
}

type withElseBranch struct {
	pattern Pattern
	body    Expr
}

func (e *withExpr) IsExpression() bool { return true }
func (e *withExpr) IsStatement() bool  { return false }
func (e *withExpr) String() string {
	return fmt.Sprintf("(with %d binds, else=%d)", len(e.items), len(e.elseBranches))
}

// trapExpr — trap-выражение: инлайн trap(expr) или блочная форма с ensure.
type trapExpr struct {
	posEnd
	expr    Expr
	body    *bodyClause
	ensures []ensureClause
}

type bodyClause struct {
	stmt Stmt
}

type ensureClause struct {
	expr Expr
}

func (e *trapExpr) IsExpression() bool { return true }
func (e *trapExpr) IsStatement() bool  { return false }
func (e *trapExpr) String() string {
	switch {
	case e.expr != nil:
		return fmt.Sprintf("(trap %s)", e.expr)
	case e.body != nil:
		return fmt.Sprintf("(trap %s)", e.body.stmt)
	default:
		return "(trap)"
	}
}

// lambdaShortExpr — короткая лямбда: x -> expr.
type lambdaShortExpr struct {
	posEnd
	param string
	body  Expr
}

func (e *lambdaShortExpr) IsExpression() bool { return true }
func (e *lambdaShortExpr) IsStatement() bool  { return false }
func (e *lambdaShortExpr) String() string {
	return fmt.Sprintf("(lambda %s -> %s)", e.param, e.body)
}

// lambdaFullExpr — полная лямбда: fn (params) -> block.
type lambdaFullExpr struct {
	posEnd
	params []string
	body   *BlockStmt
}

func (e *lambdaFullExpr) IsExpression() bool { return true }
func (e *lambdaFullExpr) IsStatement() bool  { return false }
func (e *lambdaFullExpr) String() string {
	return fmt.Sprintf("(fn (%s) -> %s)", join(e.params, ", "), e.body)
}

// lambdaEmptyExpr — пустая лямбда: () -> expr.
type lambdaEmptyExpr struct {
	posEnd
	body Expr
}

func (e *lambdaEmptyExpr) IsExpression() bool { return true }
func (e *lambdaEmptyExpr) IsStatement() bool  { return false }
func (e *lambdaEmptyExpr) String() string {
	return fmt.Sprintf("(() -> %s)", e.body)
}

// rangeExpr — диапазон: a to b.
type rangeExpr struct {
	posEnd
	start Expr
	end   Expr
}

func (e *rangeExpr) IsExpression() bool { return true }
func (e *rangeExpr) IsStatement() bool  { return false }
func (e *rangeExpr) String() string {
	return fmt.Sprintf("(to %s %s)", e.start, e.end)
}

// decimalExpr — dec"...".
type decimalExpr struct {
	posEnd
	value string
}

func (e *decimalExpr) IsExpression() bool { return true }
func (e *decimalExpr) IsStatement() bool  { return false }
func (e *decimalExpr) String() string     { return fmt.Sprintf("dec\"%s\"", e.value) }

// bytesExpr — b"...".
type bytesExpr struct {
	posEnd
	value string
}

func (e *bytesExpr) IsExpression() bool { return true }
func (e *bytesExpr) IsStatement() bool  { return false }
func (e *bytesExpr) String() string     { return fmt.Sprintf("b\"%s\"", e.value) }

// regexExpr — rx"...".
type regexExpr struct {
	posEnd
	value string
}

func (e *regexExpr) IsExpression() bool { return true }
func (e *regexExpr) IsStatement() bool  { return false }
func (e *regexExpr) String() string     { return fmt.Sprintf("rx\"%s\"", e.value) }

// atomExpr — :name, :ready?.
type atomExpr struct {
	posEnd
	ident string
}

func (e *atomExpr) IsExpression() bool { return true }
func (e *atomExpr) IsStatement() bool  { return false }
func (e *atomExpr) String() string     { return ":" + e.ident }

// interpExpr — строковая интерполяция "a \(x) b" (S-F1 / T-53).
// parts имеет len(exprs)+1 элементов; plain-строка без \(...) остаётся literalExpr.
type interpExpr struct {
	posEnd
	parts []string
	exprs []Expr
}

func (e *interpExpr) IsExpression() bool { return true }
func (e *interpExpr) IsStatement() bool  { return false }
func (e *interpExpr) String() string {
	var buf bytes.Buffer
	buf.WriteByte('"')
	for i, part := range e.parts {
		buf.WriteString(part)
		if i < len(e.exprs) {
			buf.WriteString(`\(`)
			buf.WriteString(e.exprs[i].String())
			buf.WriteByte(')')
		}
	}
	buf.WriteByte('"')
	return buf.String()
}

// Sealed-маркеры для литеральных интерфейсов. Без них type switch
// по ast.LiteralExpr / ast.BytesExpr / ast.RegexExpr / ast.DecimalExpr
// неоднозначен: method set у всех четырёх идентичен.
func (e *literalExpr) literalMarker() {}
func (e *decimalExpr) decimalMarker() {}
func (e *bytesExpr) bytesMarker()     {}
func (e *regexExpr) regexMarker()     {}
