package ast

import (
	"bytes"
	"fmt"
)

// binaryExpr — бинарное выражение (a op b).
// op ∈ { "+", "-", "*", "/", "**", "to", "|>", "and", "or",
//
//	"==", "!=", "<", ">", "<=", ">=", "=>", ":" }
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

// unaryExpr — унарное выражение: -a, not a, ..xs (спред-как-унарный).
type unaryExpr struct {
	posEnd
	op   string // "-", "not", ".."
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
func (e *literalExpr) String() string {
	return e.value
}

// variableExpr — переменная или имя конструктора (lower/Upper).
type variableExpr struct {
	posEnd
	name string
}

func (e *variableExpr) IsExpression() bool { return true }
func (e *variableExpr) IsStatement() bool  { return false }
func (e *variableExpr) String() string {
	return e.name
}

// assignExpr — привязка (x = expr). Используется только внутри обёрток;
// грамматически let_bind — стейтмент, но парсер может строить assignExpr
// для REPL-строк и диагностики.
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

// callExpr — вызов функции/конструктора: f(args).
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

// memberExpr — доступ к члену: obj.name (постфиксная форма .name).
type memberExpr struct {
	posEnd
	obj  Expr
	name string
}

func (e *memberExpr) IsExpression() bool { return true }
func (e *memberExpr) IsStatement() bool  { return false }
func (e *memberExpr) String() string {
	return fmt.Sprintf("%s.%s", e.obj, e.name)
}

// indexExpr — индексация: obj[idx] (постфиксная форма [expr]).
type indexExpr struct {
	posEnd
	obj   Expr
	index Expr
}

func (e *indexExpr) IsExpression() bool { return true }
func (e *indexExpr) IsStatement() bool  { return false }
func (e *indexExpr) String() string {
	return fmt.Sprintf("%s[%s]", e.obj, e.index)
}

// ifExpr — if-выражение: блочная и однострочная формы (сводимы к одной структуре).
type ifExpr struct {
	posEnd
	cond     Expr
	thenBody Expr
	elseIf   []ifExpr // цепочка else-if (в грамматике не порождается, но оставлена)
	elseBody Expr     // nil, если ветки else нет
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

// matchBranch — ветка match.
type matchBranch struct {
	pattern Pattern
	expr    Expr
}

func (e *matchExpr) IsExpression() bool { return true }
func (e *matchExpr) IsStatement() bool  { return false }
func (e *matchExpr) String() string {
	return fmt.Sprintf("(match %s ...)", e.expr)
}

// recvExpr — recv-выражение.
type recvExpr struct {
	posEnd
	branches  []recvBranch
	elseBody  Expr // nil, если else отсутствует
	afterBody Expr // nil, если after отсутствует
}

// recvBranch — ветка recv.
type recvBranch struct {
	pattern Pattern
	expr    Expr
}

func (e *recvExpr) IsExpression() bool { return true }
func (e *recvExpr) IsStatement() bool  { return false }
func (e *recvExpr) String() string {
	return "(recv ...)"
}

// withExpr — with-выражение.
type withExpr struct {
	posEnd
	items    []withItem
	elseBody Expr // nil, если else отсутствует
}

// withItem — привязка with: pattern <- expr.
type withItem struct {
	pattern Pattern
	expr    Expr
}

func (e *withExpr) IsExpression() bool { return true }
func (e *withExpr) IsStatement() bool  { return false }
func (e *withExpr) String() string {
	return "(with ...)"
}

// trapExpr — trap-выражение: инлайн trap(expr) или блочная форма с ensure.
type trapExpr struct {
	posEnd
	expr    Expr        // для trap(expr); nil для блочной формы
	body    *bodyClause // для блочной формы; nil для trap(expr)
	ensures []ensureClause
}

// bodyClause — тело блочного trap (единственный стейтмент).
type bodyClause struct {
	stmt Stmt
}

// ensureClause — клауза ensure (выражение).
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
func (e *decimalExpr) String() string {
	return fmt.Sprintf("dec\"%s\"", e.value)
}

// bytesExpr — b"...".
type bytesExpr struct {
	posEnd
	value string
}

func (e *bytesExpr) IsExpression() bool { return true }
func (e *bytesExpr) IsStatement() bool  { return false }
func (e *bytesExpr) String() string {
	return fmt.Sprintf("b\"%s\"", e.value)
}

// regexExpr — rx"...".
type regexExpr struct {
	posEnd
	value string
}

func (e *regexExpr) IsExpression() bool { return true }
func (e *regexExpr) IsStatement() bool  { return false }
func (e *regexExpr) String() string {
	return fmt.Sprintf("rx\"%s\"", e.value)
}

// atomExpr — :name, :ready?.
type atomExpr struct {
	posEnd
	ident string // без ':' — "ok", "ready?"
}

func (e *atomExpr) IsExpression() bool { return true }
func (e *atomExpr) IsStatement() bool  { return false }
func (e *atomExpr) String() string {
	return ":" + e.ident
}
