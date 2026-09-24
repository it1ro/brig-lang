package ast

import (
	"bytes"
	"fmt"
)

// letBind — привязка переменной: pattern = expr.
// corresponds to grammar: let_bind ::= pattern "=" expr
type letBind struct {
	posEnd
	pattern Pattern
	value   Expr
}

func (e *letBind) IsExpression() bool    { return false }
func (e *letBind) String() string         { return fmt.Sprintf("%s = %s", e.pattern, e.value) }
func (e *letBind) IsStatement() bool      { return true }

// exprStmt — выражение-стейтмент: просто expr.
// corresponds to grammar: expr_stmt ::= expr
type exprStmt struct {
	posEnd
	expr Expr
}

func (e *exprStmt) IsExpression() bool    { return false }
func (e *exprStmt) String() string         { return e.expr.String() }
func (e *exprStmt) IsStatement() bool      { return true }

// localFnDecl — локальная функция.
// corresponds to grammar: local_fn_decl ::= fn_clause+
type localFnDecl struct {
	posEnd
	clauses []localFnClause
}

type localFnClause struct {
	posEnd
	recv  string // имя受 recipient (может быть пустым)
	guard string // guard expression (опционально)
	body  BlockStmt
}

func (e *localFnDecl) IsExpression() bool    { return false }
func (e *localFnDecl) String() string         { return fmt.Sprintf("local fn with %d clauses", len(e.clauses)) }
func (e *localFnDecl) IsStatement() bool      { return true }

// blockStmt — блок стейтментов (INDENT ... DEDENT).
type blockStmt struct {
	posEnd
	stmts []Stmt
}

func (e *blockStmt) IsExpression() bool    { return false }
func (e *blockStmt) String() string         { return fmt.Sprintf("block(%d stmts)", len(e.stmts)) }
func (e *blockStmt) IsStatement() bool      { return true }

// String helpers
func join(ss []string, sep string) string {
	if len(ss) == 0 {
		return ""
	}
	if len(ss) == 1 {
		return ss[0]
	}
	buf := bytes.Buffer{}
	buf.WriteString(ss[0])
	for _, s := range ss[1:] {
		buf.WriteString(sep)
		buf.WriteString(s)
	}
	return buf.String()
}