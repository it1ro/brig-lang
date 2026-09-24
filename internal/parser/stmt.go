package parser

import (
	"github.com/it1ro/brig-lang/internal/ast"
	"github.com/it1ro/brig-lang/internal/lexer"
)

// stmt_list ::= stmt { NEWLINE stmt } [ NEWLINE ]
func (p *parser) parseStmtList(until lexer.TokenType) ([]ast.Stmt, error) {
	var stmts []ast.Stmt
	p.skipNewlines()
	for !p.at(until) && !p.at(lexer.EOF) {
		s, err := p.parseStmt()
		if err != nil {
			return nil, err
		}
		stmts = append(stmts, s)
		p.skipNewlines()
	}
	return stmts, nil
}

// stmt ::= let_bind | local_fn_decl | expr_stmt
func (p *parser) parseStmt() (ast.Stmt, error) {
	if p.at(lexer.KW_FN) && p.peek(1).Type == lexer.LOWER_IDENT {
		return p.parseLocalFnDecl()
	}

	start := p.cur()
	// Попытка let_bind: pattern '=' expr, с откатом.
	save := p.pos
	pat, err := p.parsePattern()
	if err == nil && p.at(lexer.OP_ASSIGN) {
		p.advance()
		val, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		return ast.NewLetBind(pat, val, start.Line, start.Col), nil
	}
	p.pos = save

	e, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	return ast.NewExprStmt(e, start.Line, start.Col), nil
}

// local_fn_decl ::= fn_clause+
// Все клаузы имеют одно имя; парсер собирает их в один localFnDecl.
func (p *parser) parseLocalFnDecl() (ast.Stmt, error) {
	var clauses []ast.LocalFnClauseArg
	var name string
	start := p.cur()

	for p.at(lexer.KW_FN) && p.peek(1).Type == lexer.LOWER_IDENT {
		p.advance() // fn
		n := p.advance().Lit
		if name == "" {
			name = n
		} else if n != name {
			break
		}
		c, err := p.parseFnClauseRest()
		if err != nil {
			return nil, err
		}
		clauses = append(clauses, c)
		// После inline-тела парсер стоит на NEWLINE; после блочного —
		// на KW_FN (лексер не эмитит NEWLINE после DEDENT, если следующий
		// токен не клауза). Пробуем продолжить в обоих случаях.
		save := p.pos
		p.skipNewlines()
		if p.at(lexer.KW_FN) && p.peek(1).Type == lexer.LOWER_IDENT && p.peek(1).Lit == name {
			continue
		}
		p.pos = save
		break
	}
	return ast.NewLocalFnDecl(name, clauses, start.Line, start.Col), nil
}

// fn_clause ::= "fn" LOWER_IDENT "(" [ params ] ")" [ "when" expr ] "->" fn_body
// вызывается после прочтения "fn" LOWER_IDENT.
func (p *parser) parseFnClauseRest() (ast.LocalFnClauseArg, error) {
	params, err := p.parseParams()
	if err != nil {
		return ast.LocalFnClauseArg{}, err
	}
	var guard string
	if p.match(lexer.KW_WHEN) {
		save := p.pos
		g, err := p.parseExpr()
		if err == nil {
			guard = g.String()
		} else {
			p.pos = save
		}
	}
	if _, err := p.expect(lexer.OP_ARROW, "'->'"); err != nil {
		return ast.LocalFnClauseArg{}, err
	}
	body, err := p.parseFnBody()
	if err != nil {
		return ast.LocalFnClauseArg{}, err
	}
	return ast.LocalFnClauseArg{Guard: guard, Params: params, Body: body}, nil
}

// params ::= param { sep param } [ sep ]
// param ::= pattern | ".." LOWER_IDENT
func (p *parser) parseParams() ([]string, error) {
	if _, err := p.expect(lexer.LPAREN, "'('"); err != nil {
		return nil, err
	}
	var out []string
	if p.at(lexer.RPAREN) {
		p.advance()
		return out, nil
	}
	for {
		if p.at(lexer.OP_DOTDOT) {
			p.advance()
			if !p.at(lexer.LOWER_IDENT) {
				return nil, p.errf("expected name after '..' in params")
			}
			out = append(out, ".."+p.advance().Lit)
		} else {
			pat, err := p.parsePattern()
			if err != nil {
				return nil, err
			}
			out = append(out, pat.String())
		}
		if p.match(lexer.COMMA) {
			if p.at(lexer.RPAREN) {
				break
			}
			continue
		}
		break
	}
	if _, err := p.expect(lexer.RPAREN, "')'"); err != nil {
		return nil, err
	}
	return out, nil
}

// fn_body ::= expr | NEWLINE INDENT stmt_list DEDENT
func (p *parser) parseFnBody() (*ast.BlockStmt, error) {
	if p.at(lexer.NEWLINE) {
		p.advance()
		if _, err := p.expect(lexer.INDENT, "INDENT"); err != nil {
			return nil, err
		}
		stmts, err := p.parseStmtList(lexer.DEDENT)
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(lexer.DEDENT, "DEDENT"); err != nil {
			return nil, err
		}
		return ast.NewBlockStmt(stmts, p.cur().Line, p.cur().Col), nil
	}
	// Однострочное тело — оборачиваем в блок из одного expr_stmt.
	e, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	st := ast.NewExprStmt(e, e.Pos(), e.End())
	return ast.NewBlockStmt([]ast.Stmt{st}, e.Pos(), e.End()), nil
}

// fn_decl ::= fn_clause+ (top-level)
func (p *parser) parseFnDecl() (ast.Decl, error) {
	var clauses []ast.FnClauseArg
	var name string
	start := p.cur()

	for p.at(lexer.KW_FN) && p.peek(1).Type == lexer.LOWER_IDENT {
		p.advance()
		n := p.advance().Lit
		if name == "" {
			name = n
		} else if n != name {
			break
		}
		params, err := p.parseParams()
		if err != nil {
			return nil, err
		}
		var guard string
		if p.match(lexer.KW_WHEN) {
			save := p.pos
			g, err := p.parseExpr()
			if err == nil {
				guard = g.String()
			} else {
				p.pos = save
			}
		}
		if _, err := p.expect(lexer.OP_ARROW, "'->'"); err != nil {
			return nil, err
		}
		body, err := p.parseFnBody()
		if err != nil {
			return nil, err
		}
		clauses = append(clauses, ast.FnClauseArg{Guard: guard, Params: params, Body: body})
		save := p.pos
		p.skipNewlines()
		if p.at(lexer.KW_FN) && p.peek(1).Type == lexer.LOWER_IDENT && p.peek(1).Lit == name {
			continue
		}
		p.pos = save
		break
	}
	return ast.NewFuncDecl(name, clauses, start.Line, start.Col), nil
}
