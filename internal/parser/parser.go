// Package parser implements the recursive descent parser for Brig.
// Grammar source of truth: brig.ebnf (Part II, A1).
package parser

import (
	"fmt"

	"github.com/it1ro/brig-lang/internal/ast"
	"github.com/it1ro/brig-lang/internal/lexer"
)

// Mode выбирает диалект разбора: модуль (top-level только декларации)
// или REPL (top-level let/expr, §9.3).
type Mode int

const (
	ModeModule Mode = iota
	ModeRepl
)

// Parse — публичная точка входа: лексинг + парсинг.
func Parse(mode Mode, src string) error {
	toks, err := lexer.Lex(src)
	if err != nil {
		return err
	}
	p := &parser{toks: toks, mode: mode}
	return p.parse()
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

// skipNewlines пропускает подряд идущие NEWLINE.
func (p *parser) skipNewlines() {
	for p.at(lexer.NEWLINE) {
		p.advance()
	}
}

// ---- entry ----

func (p *parser) parse() error {
	if p.mode == ModeRepl {
		return p.parseRepl()
	}
	return p.parseModule()
}

// program ::= [ module_decl NEWLINE ] { NEWLINE decl } [ NEWLINE ] EOF
func (p *parser) parseModule() error {
	p.skipNewlines()

	// module_decl?
	if p.at(lexer.KW_MODULE) {
		if err := p.parseModuleDecl(); err != nil {
			return err
		}
		if _, err := p.expect(lexer.NEWLINE, "NEWLINE after module declaration"); err != nil {
			return err
		}
	}

	p.skipNewlines()

	for !p.at(lexer.EOF) {
		if err := p.parseTopDecl(); err != nil {
			return err
		}
		p.skipNewlines()
	}
	return nil
}

// repl_line ::= import_decl | alias_decl | stmt
func (p *parser) parseRepl() error {
	p.skipNewlines()
	if p.at(lexer.EOF) {
		return nil
	}
	if p.at(lexer.KW_IMPORT) {
		_, err := p.parseImportDecl()
		return err
	}
	if p.at(lexer.KW_ALIAS) {
		_, err := p.parseAliasDecl()
		return err
	}
	_, err := p.parseStmt()
	return err
}

// module_decl ::= "module" ModuleName
func (p *parser) parseModuleDecl() error {
	if _, err := p.expect(lexer.KW_MODULE, "'module'"); err != nil {
		return err
	}
	return p.parseModuleName()
}

// ModuleName ::= UPPER_IDENT { "." UPPER_IDENT }
func (p *parser) parseModuleName() error {
	if _, err := p.expect(lexer.UPPER_IDENT, "module name"); err != nil {
		return err
	}
	for p.at(lexer.OP_DOT) {
		p.advance()
		if _, err := p.expect(lexer.UPPER_IDENT, "module segment"); err != nil {
			return err
		}
	}
	return nil
}

// decl ::= import_decl | alias_decl | type_decl | fn_decl
func (p *parser) parseTopDecl() error {
	switch p.cur().Type {
	case lexer.KW_IMPORT:
		_, err := p.parseImportDecl()
		return err
	case lexer.KW_ALIAS:
		_, err := p.parseAliasDecl()
		return err
	case lexer.KW_TYPE:
		_, err := p.parseTypeDecl()
		return err
	case lexer.KW_FN:
		_, err := p.parseFnDecl()
		return err
	}
	return p.errf("module top-level allows only module/import/alias/type/fn, got %s", p.cur().Type)
}

// import_decl ::= "import" ModuleName
func (p *parser) parseImportDecl() (ast.Decl, error) {
	start := p.pos
	kw, _ := p.expect(lexer.KW_IMPORT, "'import'")
	if err := p.parseModuleName(); err != nil {
		return nil, err
	}
	// Собираем имя модуля из токенов.
	parts := []string{}
	for i := start + 1; i < p.pos; i++ {
		parts = append(parts, p.toks[i].Lit)
	}
	name := joinDots(parts)
	return ast.NewImportDecl(name, kw.Line, p.toks[p.pos-1].Col), nil
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
