package ast

import (
	"bytes"
	"fmt"
)

// binaryExpr — бинарное выражение (a op b).
type binaryExpr struct {
	posEnd
	op   string // оператор: "+", "-", "*", "/", "**", "..", "..", "to", "|>", "and", "or", "==", "!=", "<", ">", "<=", ">="
	left Expr
	right Expr
}

func (e *binaryExpr) IsExpression() bool    { return true }
func (e *binaryExpr) String() string         { return fmt.Sprintf("(%s %s %s)", e.left, e.op, e.right) }
func (e *binaryExpr) IsStatement() bool      { return false }

// unaryExpr — унарное выражение (-a, !a, not a).
type unaryExpr struct {
	posEnd
	op    string // "-", "not"
	expr  Expr
}

func (e *unaryExpr) IsExpression() bool    { return true }
func (e *unaryExpr) String() string         { return fmt.Sprintf("(%s %s)", e.op, e.expr) }
func (e *unaryExpr) IsStatement() bool      { return false }

// groupingExpr — сгруппированное выражение (a).
type groupingExpr struct {
	posEnd
	expr Expr
}

func (e *groupingExpr) IsExpression() bool    { return true }
func (e *groupingExpr) String() string         { return fmt.Sprintf("(%s)", e.expr) }
func (e *groupingExpr) IsStatement() bool      { return false }

// literalExpr — литеральное значение (число, строка, атом, булево, unit).
type literalExpr struct {
	posEnd
	value string // текстовое представление литерала
}

func (e *literalExpr) IsExpression() bool    { return true }
func (e *literalExpr) String() string         { return e.value }
func (e *literalExpr) IsStatement() bool      { return false }

// variableExpr — переменная (идентификатор).
type variableExpr struct {
	posEnd
	name string // имя переменной
}

func (e *variableExpr) IsExpression() bool    { return true }
func (e *variableExpr) String() string         { return e.name }
func (e *variableExpr) IsStatement() bool      { return false }

// assignExpr — привязка (x = expr).
type assignExpr struct {
	posEnd
	name string
	value Expr
}

func (e *assignExpr) IsExpression() bool    { return true }
func (e *assignExpr) String() string         { return fmt.Sprintf("%s = %s", e.name, e.value) }
func (e *assignExpr) IsStatement() bool      { return false }

// callExpr — вызов функции.
type callExpr struct {
	posEnd
	callee  Expr
	args    []Expr // аргументы (с возможностью спреда ..args)
}

// IsExpression — всегда true для callExpr.
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

// pipeExpr — pipe-выражение (a |> f).
type pipeExpr struct {
	posEnd
	expr   Expr    // выражение слева от pipe
	callee Expr    // функция/конструкция справа
	args   []Expr  // аргументы (если callee(args))
}

func (e *pipeExpr) IsExpression() bool    { return true }
func (e *pipeExpr) String() string         { return fmt.Sprintf("%s |> %s", e.expr, e.callee) }
func (e *pipeExpr) IsStatement() bool      { return false }

// ifExpr — if-выражение.
// corresponds to: if expr NEWLINE INDENT stmt_list DEDENT [ else_if_clause ]
type ifExpr struct {
	posEnd
	cond    Expr   // условие
thenBody Expr   // тело then
elseIf  []ifExpr // цепочка else-if
elseBody Expr   // тело else (или nil, если else absent)
}

func (e *ifExpr) IsExpression() bool    { return true }
func (e *ifExpr) String() string         { return fmt.Sprintf("if %s then %s else %s", e.cond, e.thenBody, e.elseBody) }
func (e *ifExpr) IsStatement() bool      { return false }

// matchExpr — match-выражение.
// corresponds to: match expr NEWLINE INDENT match_branch_list DEDENT
type matchExpr struct {
	posEnd
	expr      Expr      // выражение для матчинга
	branches  []matchBranch // списки веток
}

type matchBranch struct {
	pattern Pattern
	expr    Expr
}

func (e *matchExpr) IsExpression() bool    { return true }
func (e *matchExpr) String() string         { return fmt.Sprintf("match %s", e.expr) }
func (e *matchExpr) IsStatement() bool      { return false }

// recvExpr — recv-выражение.
// corresponds to: recv NEWLINE INDENT recv_branch_list DEDENT [ else_clause ] [ after_clause ]
type recvExpr struct {
	posEnd
	branches  []recvBranch // списки веток recv
	elseBody  Expr         // тело else (если есть)
	afterBody Expr         // тело after (если есть)
}

type recvBranch struct {
	pattern Pattern
	expr    Expr
}

func (e *recvExpr) IsExpression() bool    { return true }
func (e *recvExpr) String() string         { return "recv ..." }
func (e *recvExpr) IsStatement() bool      { return false }

// withExpr — with-выражение.
// corresponds to: with NEWLINE INDENT with_item_list DEDENT [ with_else ]
type withExpr struct {
	posEnd
	items  []withItem   // список привязей
	elseBody Expr        // тело else (если есть)
}

type withItem struct {
	pattern Pattern
	expr    Expr
}

func (e *withExpr) IsExpression() bool    { return true }
func (e *withExpr) String() string         { return "with ..." }
func (e *withExpr) IsStatement() bool      { return false }

// trapExpr — trap-выражение.
// corresponds to: trap ( expr ) | trap NEWLINE body_clause { ensure_clause }
type trapExpr struct {
	posEnd
	expr    Expr       // выражение (для trap (expr))
	body    *bodyClause // тело блока (для trap NEWLINE ...)
	ensures []ensureClause // ensure-clauses
}

type bodyClause struct {
	stmt Stmt // тело блока
}

type ensureClause struct {
	expr Expr // выражение ensure
}

func (e *trapExpr) IsExpression() bool    { return true }
func (e *trapExpr) String() string         { return "trap ..." }
func (e *trapExpr) IsStatement() bool      { return false }

// lambdaShortExpr — короткая лямбда: x -> expr.
type lambdaShortExpr struct {
	posEnd
	param string // имя параметра
	body  Expr   // тело выражения
}

func (e *lambdaShortExpr) IsExpression() bool    { return true }
func (e *lambdaShortExpr) String() string         { return fmt.Sprintf("(%s -> %s)", e.param, e.body) }
func (e *lambdaShortExpr) IsStatement() bool      { return false }

// lambdaFullExpr — полная лямбда: fn (x) -> body.
type lambdaFullExpr struct {
	posEnd
	params []string // список параметров
	body   BlockStmt // тело блока
}

func (e *lambdaFullExpr) IsExpression() bool    { return true }
func (e *lambdaFullExpr) String() string         { return fmt.Sprintf("fn(%s) -> ...", join(e.params, ", ")) }
func (e *lambdaFullExpr) IsStatement() bool      { return false }

// lambdaEmptyExpr — пустая лямбда: () -> expr.
type lambdaEmptyExpr struct {
	posEnd
	body Expr // тело выражения
}

func (e *lambdaEmptyExpr) IsExpression() bool    { return true }
func (e *lambdaEmptyExpr) String() string         { return "() -> " + e.body.String() }
func (e *lambdaEmptyExpr) IsStatement() bool      { return false }

// rangeExpr — range выражение: 1 to 10.
type rangeExpr struct {
	posEnd
	start Expr
	end   Expr
}

func (e *rangeExpr) IsExpression() bool    { return true }
func (e *rangeExpr) String() string         { return fmt.Sprintf("%s to %s", e.start, e.end) }
func (e *rangeExpr) IsStatement() bool      { return false }

// decimalExpr — decimal литерал: dec"1.50".
type decimalExpr struct {
	posEnd
	value string // текстовое представление dec"..."
}

func (e *decimalExpr) IsExpression() bool    { return true }
func (e *decimalExpr) String() string         { return e.value }
func (e *decimalExpr) IsStatement() bool      { return false }

// bytesExpr — bytes литерал: b"\x89PNG".
type bytesExpr struct {
	posEnd
	value string // текстовое представление b"..."
}

func (e *bytesExpr) IsExpression() bool    { return true }
func (e *bytesExpr) String() string         { return e.value }
func (e *bytesExpr) IsStatement() bool      { return false }

// regexExpr — regex литерал: rx"...".
type regexExpr struct {
	posEnd
	value string // текстовое представление rx"..."
}

func (e *regexExpr) IsExpression() bool    { return true }
func (e *regexExpr) String() string         { return e.value }
func (e *regexExpr) IsStatement() bool      { return false }

// atomExpr — атомарное выражение: :ok, :ready?.
type atomExpr struct {
	posEnd
	ident string // имя атома (без :)
}

func (e *atomExpr) IsExpression() bool    { return true }
func (e *atomExpr) String() string         { return ":" + e.ident }
func (e *atomExpr) IsStatement() bool      { return false }