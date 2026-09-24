package ast

import "fmt"

// Program — корень разбора. В module-режиме заполнен Module и Decls;
// в repl-режиме — Stmts (и, возможно, Decls для import/alias).
type Program struct {
	posEnd
	Module string
	Decls  []Decl
	Stmts  []Stmt
}

func (p *Program) String() string {
	return fmt.Sprintf("Program(module=%q, decls=%d, stmts=%d)",
		p.Module, len(p.Decls), len(p.Stmts))
}

// IsTopLevel сообщает, что Program — корневой узел верхнего уровня.
func (p *Program) IsTopLevel() bool { return true }

var _ Node = (*Program)(nil)
