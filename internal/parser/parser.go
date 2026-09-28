// Package parser implements the recursive descent parser for Brig.
// Grammar source of truth: brig.ebnf (Part II, A1).
package parser

import (
	"fmt"

	"github.com/it1ro/brig-lang/internal/ast"
	"github.com/it1ro/brig-lang/internal/lexer"
)

// Mode выбирает диалект разбора: модуль, REPL или script (§11.3).
type Mode int

const (
	// ModeModule is module parsing mode.
	ModeModule Mode = iota // ModeModule is module parsing mode
	// ModeRepl selects REPL parsing mode.
	ModeRepl // ModeRepl is REPL parsing mode
	// ModeScript — файл без module (§11.3): те же инструкции, что repl_input.
	ModeScript
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
	switch mode {
	case ModeRepl, ModeScript:
		// script ::= [ NEWLINE ] [ repl_input ] EOF (§11.3).
		// Пустые строки блок не закрывают: это делает лексер файла, не парсер.
		return p.parseRepl()
	default:
		return p.parseModule()
	}
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

// ParseReplInput разбирает порцию ввода REPL (repl_input, §11.4): одну
// или несколько repl_line. Каждая repl_line — отдельный Program (своя
// top-level область), порядок — порядок ввода.
func ParseReplInput(src string) ([]*ast.Program, error) {
	toks, err := lexer.Lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks, mode: ModeRepl}
	return p.parseReplInput()
}

// parseRepl — ModeRepl для ParseProgram: repl_input одним Program.
func (p *parser) parseRepl() (*ast.Program, error) {
	lines, err := p.parseReplInput()
	if err != nil {
		return nil, err
	}
	prog := &ast.Program{}
	for _, l := range lines {
		prog.Decls = append(prog.Decls, l.Decls...)
		prog.Stmts = append(prog.Stmts, l.Stmts...)
	}
	return prog, nil
}

// repl_input ::= repl_line { NEWLINE repl_line } [ NEWLINE ]
func (p *parser) parseReplInput() ([]*ast.Program, error) {
	var lines []*ast.Program
	p.skipNewlines()
	for !p.at(lexer.EOF) {
		l, err := p.parseReplLine()
		if err != nil {
			return nil, err
		}
		lines = append(lines, l)
		if err := p.expectReplLineEnd(); err != nil {
			return nil, err
		}
		p.skipNewlines()
	}
	return lines, nil
}

// repl_line ::= import_decl | alias_decl | stmt
//
// type и pub fn в script и REPL — ошибка парсинга (§11.3): им место в модуле.
func (p *parser) parseReplLine() (*ast.Program, error) {
	switch p.cur().Type {
	case lexer.KW_TYPE:
		return nil, p.errf("type is not allowed in script or repl")
	case lexer.KW_PUB:
		return nil, p.errf("pub fn is not allowed in script or repl")
	case lexer.KW_MODULE:
		return nil, p.errf("module is not allowed in script or repl")
	}
	prog := &ast.Program{}
	if p.at(lexer.KW_IMPORT) || p.at(lexer.KW_ALIAS) {
		var d ast.Decl
		var err error
		if p.at(lexer.KW_IMPORT) {
			d, err = p.parseImportDecl()
		} else {
			d, err = p.parseAliasDecl()
		}
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

// expectReplLineEnd проверяет, что repl_line кончается NEWLINE, EOF или
// блоком (DEDENT), как стейтмент в stmt_list (T-156 #211): хвост на той
// же строке не отбрасывается молча.
func (p *parser) expectReplLineEnd() error {
	if p.pos > 0 && p.toks[p.pos-1].Type == lexer.DEDENT {
		return nil
	}
	if !p.at(lexer.NEWLINE) && !p.at(lexer.EOF) {
		return p.errf("unexpected token after repl statement: %s", p.cur().Type)
	}
	return nil
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
	case lexer.KW_FN, lexer.KW_PUB:
		return p.parseFnDecl()
	}
	return nil, p.errf("module top-level allows only module/import/alias/type/fn/pub fn, got %s",
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

// scanQualified дочитывает ModuleName после прочитанного первого
// сегмента first: `{ "." UPPER_IDENT }`. Квалифицированное имя в
// record_literal, record_pattern, constructor_pattern и type_primary —
// последний сегмент тип или конструктор, предыдущие — локальное имя
// модуля (§11.1, §A.1 п.13).
func (p *parser) scanQualified(first string) string {
	for p.at(lexer.OP_DOT) && p.peek(1).Type == lexer.UPPER_IDENT {
		p.advance()
		first += "." + p.advance().Lit
	}
	return first
}

// qualifiedRecordAhead: с текущего UPPER_IDENT начинается литерал записи
// `A.B.T{` (ModuleName, затем "{").
func (p *parser) qualifiedRecordAhead() bool {
	i := 1
	for p.peek(i).Type == lexer.OP_DOT && p.peek(i+1).Type == lexer.UPPER_IDENT {
		i += 2
	}
	return p.peek(i).Type == lexer.LBRACE
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
