// Package parser implements the Brig parser.
//
// Этап 1 (Трек B, A2): структурный скелет — прогон токенов лексера и
// проверка инвариантов: отсутствие ILLEGAL, соответствие top-level режиму
// module|repl (§9). Полный recursive descent по brig.ebnf (A1) — этап 2.
package parser

import (
	"fmt"

	"github.com/it1ro/brig-lang/internal/lexer"
)

// Mode — режим парсинга (§9).
type Mode int

const (
	// ModeModule — файл-модуль: top-level только декларации (module/import/alias/type/fn).
	ModeModule Mode = iota
	// ModeRepl — REPL: разрешены top-level let и выражения (§10.7).
	ModeRepl
)

// Error — ошибка парсинга с позицией.
type Error struct {
	Line, Col int
	Msg       string
}

func (e *Error) Error() string {
	return fmt.Sprintf("parse error %d:%d: %s", e.Line, e.Col, e.Msg)
}

// Parse разбирает src в режиме mode. Этап 1: лексер + инварианты.
func Parse(mode Mode, src string) error {
	toks, err := lexer.Lex(src)
	if err != nil {
		return err
	}
	return checkTopLevel(mode, toks)
}

// checkTopLevel: в mode=module top-level стейтменты — только
// module/import/alias/type/fn; top-level let/expr — ошибка парсинга (§9).
func checkTopLevel(mode Mode, toks []lexer.Token) error {
	if mode == ModeModule {
		if err := verifyModuleTopLevel(toks); err != nil {
			return err
		}
	}
	return nil
}

var topLevelOK = map[lexer.TokenType]bool{
	lexer.KW_MODULE: true, lexer.KW_IMPORT: true, lexer.KW_ALIAS: true,
	lexer.KW_TYPE: true, lexer.KW_FN: true,
}

// verifyModuleTopLevel проходит top-level стейтменты (глубина 0) и проверяет
// первый токен. NEWLINE на глубине 0 начинает новый стейтмент.
func verifyModuleTopLevel(toks []lexer.Token) error {
	depth := 0
	firstTok := true
	for i, tk := range toks {
		switch tk.Type {
		case lexer.NEWLINE:
			firstTok = true
		case lexer.INDENT:
			depth++
			firstTok = true
		case lexer.DEDENT:
			depth--
			firstTok = true
		case lexer.EOF:
			return nil
		default:
			if depth == 0 && firstTok && !topLevelOK[tk.Type] {
				return &Error{Line: tk.Line, Col: tk.Col,
					Msg: fmt.Sprintf("top-level %q forbidden in module mode (only module/import/alias/type/fn)", tk.Lit)}
			}
			firstTok = false
		}
		_ = i
	}
	return nil
}
