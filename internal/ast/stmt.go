package ast

import (
	"bytes"
	"fmt"
)

// letBind — pattern = expr.
type letBind struct {
	posEnd
	pattern Pattern
	value   Expr
}

// IsExpression сознательно нет: метод сделал бы *letBind реализацией Expr,
// и Pretty/Walk выбрали бы case Expr раньше case Stmt (S-F13).
func (e *letBind) IsStatement() bool { return true }
func (e *letBind) String() string    { return fmt.Sprintf("%s = %s", e.pattern, e.value) }

// exprStmt — просто expr.
type exprStmt struct {
	posEnd
	expr Expr
}

func (e *exprStmt) IsStatement() bool { return true }
func (e *exprStmt) String() string    { return e.expr.String() }

// localFnDecl — локальная функция (одна или несколько клауз одного имени).
type localFnDecl struct {
	posEnd
	name    string
	clauses []localFnClause
}

type localFnClause struct {
	posEnd
	guard  Expr
	params []Pattern
	body   *BlockStmt
}

func (e *localFnDecl) IsStatement() bool { return true }
func (e *localFnDecl) String() string {
	return fmt.Sprintf("local fn %s with %d clauses", e.name, len(e.clauses))
}

// BlockStmt — блок стейтментов.
type BlockStmt struct {
	posEnd
	stmts []Stmt
}

// IsExpression reports whether the statement is an expression.
func (e *BlockStmt) IsExpression() bool { return false }

// IsStatement reports whether the statement is a statement.
func (e *BlockStmt) IsStatement() bool { return true }
func (e *BlockStmt) String() string {
	return fmt.Sprintf("block(%d stmts)", len(e.stmts))
}

// Stmts returns the statements in the block.
func (e *BlockStmt) Stmts() []Stmt { return e.stmts }

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
