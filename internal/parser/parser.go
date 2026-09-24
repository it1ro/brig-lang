// Package parser implements the recursive descent parser for Brig.
// Grammar source of truth: brig.ebnf (Part II, A1).
package parser

import (
	"fmt"

	"github.com/it1ro/brig-lang/internal/ast"
	"github.com/it1ro/brig-lang/internal/lexer"
)

// Mode выбирает диалект разбора: модуль или REPL (§9.3).
type Mode int

const (
	ModeModule Mode = iota // ModeModule is module parsing mode
	ModeRepl
)

// Parse — совместимая обёртка: только проверка без возврата AST.
// Для golden-тестов и round-trip используйте ParseProgram.
func Parse(mode Mode, src string) error {
	_, err := ParseProgram(mode, src)
	return err
}

// ParseProgram — основной вход: лексинг + парсинг, возвращает AST.
func ParseProgram(mode Mode, src string) (*ast.Program, error) {
	toks, err := lexer.Lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks, mode: mode}
	if mode == ModeRepl {
		return p.parseRepl()
	}
	return p.parseModule()
}

// Error — ошибка парсинга с позицией (формат E.1).
type Error struct {
	Line, Col int
	Msg       string
}

func (e *Error) Error() string {
	return fmt.Sprintf("parse error %d:%d: %s", e.Line, e.Col, e.Msg)
}

type parser struct {
	toks []lexer.Token
	pos  int
	mode Mode
}

// ---- helpers ----

func (p *parser) cur() lexer.Token {
	if p.pos >= len(p.toks) {
		return lexer.Token{Type: lexer.EOF}
	}
	return p.toks[p.pos]
}

func (p *parser) peek(n int) lexer.Token {
	i := p.pos + n
	if i >= len(p.toks) {
		return lexer.Token{Type: lexer.EOF}
	}
	return p.toks[i]
}

func (p *parser) at(t lexer.TokenType) bool { return p.cur().Type == t }

func (p *parser) advance() lexer.Token {
	t := p.cur()
	if p.pos < len(p.toks) {
		p.pos++
	}
	return t
}

func (p *parser) match(t lexer.TokenType) bool {
	if p.at(t) {
		p.advance()
		return true
	}
	return false
}

func (p *parser) expect(t lexer.TokenType, what string) (lexer.Token, error) {
	if !p.at(t) {
		return lexer.Token{}, p.errf("expected %s, got %s", what, p.cur().Type)
	}
	return p.advance(), nil
}

func (p *parser) errf(format string, args ...any) error {
	t := p.cur()
	return &Error{Line: t.Line, Col: t.Col, Msg: fmt.Sprintf(format, args...)}
}

func (p *parser) skipNewlines() {
	for p.at(lexer.NEWLINE) {
		p.advance()
	}
}

// ---- entry ----

// program ::= [ module_decl NEWLINE ] { NEWLINE decl } [ NEWLINE ] EOF
func (p *parser) parseModule() (*ast.Program, error) {
	prog := &ast.Program{}
	p.skipNewlines()

	if p.at(lexer.KW_MODULE) {
		p.advance()
		name, err := p.scanModuleName()
		if err != nil {
			return nil, err
		}
		prog.Module = name
		if _, err := p.expect(lexer.NEWLINE, "NEWLINE after module declaration"); err != nil {
			return nil, err
		}
	}

	p.skipNewlines()
	for !p.at(lexer.EOF) {
		d, err := p.parseTopDecl()
		if err != nil {
			return nil, err
		}
		prog.Decls = append(prog.Decls, d)
		p.skipNewlines()
	}
	return prog, nil
}

// repl_line ::= import_decl | alias_decl | stmt
func (p *parser) parseRepl() (*ast.Program, error) {
	prog := &ast.Program{}
	p.skipNewlines()
	if p.at(lexer.EOF) {
		return prog, nil
	}
	if p.at(lexer.KW_IMPORT) {
		d, err := p.parseImportDecl()
		if err != nil {
			return nil, err
		}
		prog.Decls = append(prog.Decls, d)
		return prog, nil
	}
	if p.at(lexer.KW_ALIAS) {
		d, err := p.parseAliasDecl()
		if err != nil {
			return nil, err
		}
		prog.Decls = append(prog.Decls, d)
		return prog, nil
	}
	s, err := p.parseStmt()
	if err != nil {
		return nil, err
	}
	prog.Stmts = append(prog.Stmts, s)
	return prog, nil
}

// decl ::= import_decl | alias_decl | type_decl | fn_decl
func (p *parser) parseTopDecl() (ast.Decl, error) {
	switch p.cur().Type {
	case lexer.KW_IMPORT:
		return p.parseImportDecl()
	case lexer.KW_ALIAS:
		return p.parseAliasDecl()
	case lexer.KW_TYPE:
		return p.parseTypeDecl()
	case lexer.KW_FN:
		return p.parseFnDecl()
	}
	return nil, p.errf("module top-level allows only module/import/alias/type/fn, got %s",
		p.cur().Type)
}

// import_decl ::= "import" ModuleName
func (p *parser) parseImportDecl() (ast.Decl, error) {
	kw, _ := p.expect(lexer.KW_IMPORT, "'import'")
	name, err := p.scanModuleName()
	if err != nil {
		return nil, err
	}
	return ast.NewImportDecl(name, kw.Line, kw.Col), nil
}

// alias_decl ::= "alias" ModuleName "as" ModuleName
func (p *parser) parseAliasDecl() (ast.Decl, error) {
	kw, _ := p.expect(lexer.KW_ALIAS, "'alias'")
	orig, err := p.scanModuleName()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.KW_AS, "'as'"); err != nil {
		return nil, err
	}
	alias, err := p.scanModuleName()
	if err != nil {
		return nil, err
	}
	return ast.NewAliasDecl(orig, alias, kw.Line, kw.Col), nil
}

// scanModuleName читает ModuleName и возвращает его как "A.B.C".
func (p *parser) scanModuleName() (string, error) {
	if !p.at(lexer.UPPER_IDENT) {
		return "", p.errf("expected module name, got %s", p.cur().Type)
	}
	parts := []string{p.advance().Lit}
	for p.at(lexer.OP_DOT) {
		p.advance()
		if !p.at(lexer.UPPER_IDENT) {
			return "", p.errf("expected module segment after '.'")
		}
		parts = append(parts, p.advance().Lit)
	}
	return joinDots(parts), nil
}

func joinDots(parts []string) string {
	out := ""
	for i, s := range parts {
		if i > 0 {
			out += "."
		}
		out += s
	}
	return out
}
