package repl

import (
	"errors"
	"strings"

	"github.com/it1ro/brig-lang/internal/lexer"
)

// NeedMore сообщает, что ввод src не завершён и REPL ждёт следующей
// строки (§11.4 «Ввод»). Ввод продолжается, пока:
//
//  1. открыта скобка, строковый литерал или интерполяция;
//  2. последняя строка кончается заголовком блока — дальше по грамматике
//     ждётся INDENT (`fn f(x) ->`, `match v`, `recv`, `trap`, `with`,
//     `else`, …);
//  3. во вводе открыт INDENT-блок из п. 2.
//
// Пустая строка завершает ввод, если скобки и литералы закрыты.
// Решение принимается по токенам lexer.Lex; ошибка лексера, кроме
// оборванной конструкции, завершает ввод — её покажет Eval.
func NeedMore(src string) bool {
	toks, err := lexer.Lex(src)
	if err != nil {
		var le *lexer.Error
		// Литерал, оборванный на последней строке, может закрыться в
		// следующей; на более ранней — уже ошибка (строки однострочные).
		return errors.As(err, &le) && le.Incomplete && le.Line >= lastLine(src)
	}
	if endsWithBlankLine(src) {
		return false
	}
	for _, t := range toks {
		if t.Type == lexer.INDENT {
			return true
		}
	}
	return expectsBlock(lastLogicalLine(toks))
}

// lastLine — номер последней непустой строки src (1-based).
func lastLine(src string) int {
	lines := strings.Split(src, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return i + 1
		}
	}
	return 1
}

// endsWithBlankLine — последняя строка ввода пустая, и она не первая.
func endsWithBlankLine(src string) bool {
	lines := strings.Split(strings.TrimSuffix(src, "\n"), "\n")
	return len(lines) > 1 && strings.TrimSpace(lines[len(lines)-1]) == ""
}

// lastLogicalLine — токены последнего стейтмента верхнего уровня: от
// последнего NEWLINE вне скобок до конца, без завершающих NEWLINE/DEDENT/EOF.
func lastLogicalLine(toks []lexer.Token) []lexer.Token {
	end := len(toks)
	for end > 0 {
		switch toks[end-1].Type {
		case lexer.EOF, lexer.NEWLINE, lexer.DEDENT:
			end--
			continue
		}
		break
	}
	depth := 0
	start := end
	for ; start > 0; start-- {
		switch toks[start-1].Type {
		case lexer.RPAREN, lexer.RBRACKET, lexer.RBRACE:
			depth++
		case lexer.LPAREN, lexer.LBRACKET, lexer.LBRACE, lexer.VEC_OPEN, lexer.MAP_OPEN:
			depth--
		case lexer.NEWLINE:
			if depth == 0 {
				return toks[start:end]
			}
		}
	}
	return toks[:end]
}

// expectsBlock — строка кончается там, где грамматика ждёт
// `NEWLINE INDENT` блока (brig.ebnf: fn_body, if_expr, match_expr,
// recv_expr, with_expr, trap_expr, ensure_clause, else-клаузы).
func expectsBlock(line []lexer.Token) bool {
	if len(line) == 0 {
		return false
	}
	switch line[len(line)-1].Type {
	case lexer.OP_ARROW, lexer.KW_TRAP, lexer.KW_RECV, lexer.KW_WITH, lexer.KW_ELSE, lexer.KW_ENSURE:
		return true
	}
	// recv: `else reason` — клауза с именем причины (else_clause).
	if len(line) == 2 && line[0].Type == lexer.KW_ELSE && line[1].Type == lexer.LOWER_IDENT {
		return true
	}
	// `match e` — только блочная форма; `if e` без `then` — блочная.
	for i := len(line) - 1; i >= 0; i-- {
		switch line[i].Type {
		case lexer.OP_ARROW, lexer.KW_THEN:
			return false
		case lexer.KW_MATCH, lexer.KW_IF:
			return true
		}
	}
	return false
}

// IndentWidth — ширина уровня отступа, который ставит автоотступ консоли.
const IndentWidth = 4

// Indent — отступ новой строки ввода после src (текст до курсора):
// отступ последней строки src, а если она кончается заголовком блока
// (NeedMore, п. 2) — на уровень глубже.
func Indent(src string) string {
	line := src[strings.LastIndexByte(src, '\n')+1:]
	body := strings.TrimLeft(line, " ")
	indent := line[:len(line)-len(body)]
	toks, err := lexer.Lex(body)
	if err == nil && expectsBlock(lastLogicalLine(toks)) {
		indent += strings.Repeat(" ", IndentWidth)
	}
	return indent
}
