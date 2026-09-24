package lexer

import (
	"fmt"
)

// Error — ошибка лексера с позицией (A3: ошибки всегда line:col).
type Error struct {
	Line, Col int
	Msg       string
}

func (e *Error) Error() string {
	return fmt.Sprintf("lex error %d:%d: %s", e.Line, e.Col, e.Msg)
}

func errf(line, col int, format string, args ...any) *Error {
	return &Error{Line: line, Col: col, Msg: fmt.Sprintf(format, args...)}
}

// Lex токенизирует src: полный список токенов программы, включая
// offside-терминалы NEWLINE/INDENT/DEDENT (A5.2) и EOF.
func Lex(src string) ([]Token, error) { return newLexer(src).run() }

type lexer struct {
	src string

	// A5.1 состояние лексера.
	indentStack []int // начинается с [0]
	parenDepth  int   // глубина () [] {} %[] %{}
	stmtIndent  int   // отступ первой строки текущего стейтмента
	firstLine   bool  // первая ли логическая строка файла

	line int // текущая физическая строка (1-based)
	pos  int // текущая позиция чтения в src

	lastEndedNL bool // предыдущая физическая строка закончилась \n
	tokens      []Token
}

func newLexer(src string) *lexer {
	return &lexer{
		src:         src,
		indentStack: []int{0},
		firstLine:   true,
		line:        1,
	}
}

func (l *lexer) run() ([]Token, error) {
	for {
		pl, ok := l.nextPhysLine()
		if !ok {
			break
		}
		if !pl.hasTok {
			continue // пустые строки и строки-комментарии не влияют на offside (A5.2)
		}
		if err := l.processLine(pl); err != nil {
			return nil, err
		}
	}

	// EOF: финальный NEWLINE (логическая строка завершена), затем DEDENT* и EOF.
	if !l.firstLine {
		l.emit(NEWLINE, "\n")
	}
	for l.top() > 0 {
		l.indentStack = l.indentStack[:len(l.indentStack)-1]
		l.emit(DEDENT, "")
	}
	if l.parenDepth > 0 {
		return nil, errf(l.line, 1, "unclosed bracket at EOF")
	}
	l.emit(EOF, "")
	return l.tokens, nil
}

// processLine обрабатывает одну непустую физическую строку по A5.2.
func (l *lexer) processLine(pl physLine) error {
	if l.parenDepth > 0 {
		// Внутри скобок offside отключён: NEWLINE/INDENT/DEDENT не эмитятся.
		// TODO(A5.4): offside-блоки внутри скобок (fn/match/... внутри %{...})
		// — отдельный мини-проход lex_offside_block с якорем column_of(opener).
		return l.lexLine(pl)
	}

	indent := countLeadingSpaces(pl.text)
	ft := firstToken(pl.text)
	// Строка-продолжение (A5.2): только если есть предыдущий стейтмент
	// (не первая строка). Отступ строго больше stmt_indent; stmt_indent НЕ
	// изменяется (СУ-004).
	if !l.firstLine && ft != "" && continuationOps[ft] {
		if indent <= l.stmtIndent {
			return errf(pl.line, 1, "continuation must be indented more than statement")
		}
		return l.lexLine(pl)
	}

	if !l.firstLine {
		l.emit(NEWLINE, "\n")
	}

	switch {
	case indent > l.top():
		l.indentStack = append(l.indentStack, indent)
		l.emit(INDENT, "")
	case indent < l.top():
		for l.top() > indent {
			l.indentStack = l.indentStack[:len(l.indentStack)-1]
			l.emit(DEDENT, "")
		}
		if l.top() != indent {
			return errf(pl.line, 1, "inconsistent dedent")
		}
	}

	l.stmtIndent = indent
	l.firstLine = false
	return l.lexLine(pl)
}

func (l *lexer) top() int { return l.indentStack[len(l.indentStack)-1] }

func (l *lexer) emit(typ TokenType, lit string) {
	l.tokens = append(l.tokens, Token{Type: typ, Lit: lit, Line: l.line, Col: 1})
}

// physLine — одна физическая строка.
type physLine struct {
	text   string
	line   int
	hasTok bool
}

// nextPhysLine читает следующую физическую строку; ok=false при EOF.
// Пустые и комментарий-только строки пропускаются (A5.2).
func (l *lexer) nextPhysLine() (physLine, bool) {
	for l.pos < len(l.src) {
		start := l.pos
		for l.pos < len(l.src) && l.src[l.pos] != '\n' {
			l.pos++
		}
		endedNL := false
		if l.pos < len(l.src) && l.src[l.pos] == '\n' {
			l.pos++
			endedNL = true
		}
		l.lastEndedNL = endedNL
		end := l.pos
		if endedNL {
			end = l.pos - 1 // без '\n'
		}
		text := l.src[start:end]
		l.line++
		if !blankOrComment(text) {
			return physLine{text: text, line: l.line - 1, hasTok: true}, true
		}
	}
	return physLine{}, false
}

// blankOrComment — true, если строка пустая или содержит только комментарий.
func blankOrComment(s string) bool {
	i := skipSpacesIdx(s, 0)
	return i >= len(s) || s[i] == '#'
}

// lexLine сканирует все токены одной физической строки (без offside-эмиссии).
func (l *lexer) lexLine(pl physLine) error {
	text := pl.text
	i := 0
	n := len(text)
	for i < n {
		c := text[i]
		switch c {
		case ' ':
			i++
			continue
		case '\t':
			// Табы запрещены (design §1: "Только пробелы, tabs запрещены").
			return errf(pl.line, i+1, "tab character is forbidden")
		case '#':
			return nil // комментарий до конца строки
		}

		// №3–6 (A3.2): строки/сигилы.
		if c == '"' {
			body, end, err := scanString(text, i, pl.line)
			if err != nil {
				return err
			}
			l.addToken(Token{Type: STRING, Lit: body}, pl, i)
			i = end
			continue
		}
		if prefixAt(text, i, `b"`) {
			body, end, err := scanBytes(text, i, pl.line)
			if err != nil {
				return err
			}
			l.addToken(Token{Type: BYTES, Lit: body}, pl, i)
			i = end
			continue
		}
		if prefixAt(text, i, `rx"`) {
			body, end, err := scanRegex(text, i, pl.line)
			if err != nil {
				return err
			}
			l.addToken(Token{Type: REGEX, Lit: body}, pl, i)
			i = end
			continue
		}
		if prefixAt(text, i, `dec"`) {
			body, end, err := scanDecimal(text, i, pl.line)
			if err != nil {
				return err
			}
			l.addToken(Token{Type: DECIMAL, Lit: body}, pl, i)
			i = end
			continue
		}

		// №7 (KR-006): ':' → ATOM (после ':' обязаны [a-z] или '_') или COLON.
		if c == ':' {
			if i+1 < n && (isLower(text[i+1]) || text[i+1] == '_') {
				j := scanIdent(text, i+1)
				l.addToken(Token{Type: ATOM, Lit: text[i:j]}, pl, i)
				i = j
				continue
			}
			l.addToken(Token{Type: COLON, Lit: ":"}, pl, i)
			i++
			continue
		}

		// №8–9: числа.
		if (c == '0' && i+1 < n && (text[i+1] == 'x' || text[i+1] == 'b' || text[i+1] == 'o')) ||
			isDecDigit(c) {
			lit, end, err := scanNumber(text, i, pl.line)
			if err != nil {
				return err
			}
			typ := INT
			if containsAny(lit, ".eE") {
				typ = FLOAT
			}
			l.addToken(Token{Type: typ, Lit: lit}, pl, i)
			i = end
			continue
		}

		// №10: UPPER_IDENT.
		if isUpper(c) {
			j := scanIdent(text, i)
			l.addToken(Token{Type: UPPER_IDENT, Lit: text[i:j]}, pl, i)
			i = j
			continue
		}

		// №11: LOWER_IDENT / WILDCARD (+ шаг 11a — ключевые слова, KR-004).
		if isLower(c) {
			j := scanIdent(text, i)
			word := text[i:j]
			if !hasSuffixQ(word) {
				if kw, ok := keywords[word]; ok {
					l.addToken(Token{Type: kw, Lit: word}, pl, i)
					i = j
					continue
				}
			}
			l.addToken(Token{Type: LOWER_IDENT, Lit: word}, pl, i)
			i = j
			continue
		}
		if c == '_' {
			// '_' — WILDCARD; '_x'/'_1' — ошибка (правила идентификаторов, §1).
			if i+1 < n && (isLower(text[i+1]) || isUpper(text[i+1]) || isDecDigit(text[i+1])) {
				return errf(pl.line, i+1, "identifier must not start with '_'")
			}
			l.addToken(Token{Type: WILDCARD, Lit: "_"}, pl, i)
			i++
			continue
		}

		// №12: операторы/разделители — самый длинный подходящий.
		if c == '%' {
			if i+1 < n && text[i+1] == '[' {
				l.addToken(Token{Type: VEC_OPEN, Lit: "%["}, pl, i)
				i += 2
				continue
			}
			if i+1 < n && text[i+1] == '{' {
				l.addToken(Token{Type: MAP_OPEN, Lit: "%{"}, pl, i)
				i += 2
				continue
			}
			// КР-005: одиночный '%' — ошибка лексера.
			return errf(pl.line, i+1, "lone '%%' is not an operator (KR-005)")
		}
		consumed, typ, ok := l.scanOperator(text, i)
		if !ok {
			return errf(pl.line, i+1, "unexpected character %q", c)
		}
		l.addToken(Token{Type: typ, Lit: text[i : i+consumed]}, pl, i)
		i += consumed
	}
	return nil
}

// scanOperator возвращает длину и тип оператора (longest match) или ok=false.
// Обновляет parenDepth для () [] {} %[] %{} (A5.4).
func (l *lexer) scanOperator(text string, i int) (int, TokenType, bool) {
	n := len(text)
	for _, op := range twoCharOps {
		if i+1 < n && text[i] == op.text[0] && text[i+1] == op.text[1] {
			return 2, op.typ, true
		}
	}
	if i >= n {
		return 0, ILLEGAL, false
	}
	switch text[i] {
	case '+':
		return 1, OP_PLUS, true
	case '-':
		return 1, OP_MINUS, true
	case '*':
		return 1, OP_STAR, true
	case '/':
		return 1, OP_SLASH, true
	case '<':
		return 1, OP_LT, true
	case '>':
		return 1, OP_GT, true
	case '=':
		return 1, OP_ASSIGN, true
	case '.':
		// '..' уже проверен; одиночный '.' — постфикс ('.name') или (см. §2.1,
		// '.5' — ошибка лексера; контекстная проверка: смотри lexLine).
		return 1, OP_DOT, true
	case '(':
		l.parenDepth++
		return 1, LPAREN, true
	case ')':
		if l.parenDepth > 0 {
			l.parenDepth--
		}
		return 1, RPAREN, true
	case '[':
		l.parenDepth++
		return 1, LBRACKET, true
	case ']':
		if l.parenDepth > 0 {
			l.parenDepth--
		}
		return 1, RBRACKET, true
	case '{':
		l.parenDepth++
		return 1, LBRACE, true
	case '}':
		if l.parenDepth > 0 {
			l.parenDepth--
		}
		return 1, RBRACE, true
	case ',':
		return 1, COMMA, true
	case ';':
		return 1, SEMICOLON, true
	}
	return 0, ILLEGAL, false
}

func (l *lexer) addToken(t Token, pl physLine, i int) {
	t.Line = pl.line
	t.Col = i + 1
	l.tokens = append(l.tokens, t)
}

// ---- Вспомогательные сканеры ----

func prefixAt(s string, i int, p string) bool {
	return i+len(p) <= len(s) && s[i:i+len(p)] == p
}

func skipSpacesIdx(s string, i int) int {
	for i < len(s) && s[i] == ' ' {
		i++
	}
	return i
}

func countLeadingSpaces(s string) int { return skipSpacesIdx(s, 0) }

// firstToken возвращает текст первого токена строки (для continuation-проверки).
func firstToken(s string) string {
	i := skipSpacesIdx(s, 0)
	if i >= len(s) || s[i] == '#' {
		return ""
	}
	j := i
	c := s[j]
	switch {
	case isLower(c) || isUpper(c) || c == '_':
		for j < len(s) && (isLower(s[j]) || isUpper(s[j]) || isDecDigit(s[j]) || s[j] == '_') {
			j++
		}
		// 'and?' — предикатный идентификатор, НЕ keyword 'and' (шаг 11a).
		if j < len(s) && s[j] == '?' {
			j++
		}
	case isDecDigit(c):
		for j < len(s) && isDecDigit(s[j]) {
			j++
		}
	case c == '"':
		// Сигилы: "..." b"..." rx"..." dec"..." — прочесть до закрывающей кавычки.
		k := i + 1
		for k < len(s) && s[k] != '"' {
			if s[k] == '\\' {
				k++
			}
			k++
		}
		if k < len(s) {
			k++ // закрывающая кавычка
		}
		return s[i:k]
	default:
		for _, op := range twoCharOps {
			if prefixAt(s, j, op.text) {
				return op.text
			}
		}
		return s[j : j+1]
	}
	return s[i:j]
}

// scanIdent читает [a-zA-Z0-9_]*, опциональный финальный '?'.
func scanIdent(s string, i int) int {
	j := i
	for j < len(s) && (isLower(s[j]) || isUpper(s[j]) || isDecDigit(s[j]) || s[j] == '_') {
		j++
	}
	if j < len(s) && s[j] == '?' {
		j++
	}
	return j
}

// hasSuffixQ — слово оканчивается на '?' (предикатный идентификатор: map?, and?).
func hasSuffixQ(word string) bool {
	return len(word) > 0 && word[len(word)-1] == '?'
}

// containsAny — содержит ли s хотя бы один символ из chars.
func containsAny(s, chars string) bool {
	for i := 0; i < len(s); i++ {
		for j := 0; j < len(chars); j++ {
			if s[i] == chars[j] {
				return true
			}
		}
	}
	return false
}

// scanNumber — INT (0x/0b/0o/dec) или FLOAT (с '.' или e/E) (A3.2 №8–9, §2.1).
func scanNumber(text string, i, line int) (string, int, error) {
	n := len(text)
	if i+1 < n {
		switch text[i : i+2] {
		case "0x", "0b", "0o":
			base := map[string]int{"0x": 16, "0b": 2, "0o": 8}[text[i:i+2]]
			j := i + 2
			digits := 0
			for j < n {
				c := text[j]
				if isDigitForBase(c, base) {
					digits++
					j++
					continue
				}
				if c == '_' && j+1 < n && isDigitForBase(text[j+1], base) {
					j++
					continue
				}
				break
			}
			if digits == 0 {
				return "", 0, errf(line, i+1, "digit expected after radix prefix")
			}
			if j < n && text[j] == '_' {
				return "", 0, errf(line, j+1, "underscore must be between digits")
			}
			return text[i:j], j, nil
		}
	}

	j := i
	scanDigits := func(start int) (int, error) {
		prevDigit := false
		for j < n {
			c := text[j]
			if isDecDigit(c) {
				prevDigit = true
				j++
				continue
			}
			if c == '_' && prevDigit && j+1 < n && isDecDigit(text[j+1]) {
				j++
				prevDigit = false
				continue
			}
			break
		}
		if j < n && text[j] == '_' {
			return 0, errf(line, j+1, "underscore must be between digits")
		}
		if j == start && j < n {
			return 0, errf(line, j+1, "digit expected")
		}
		return j, nil
	}
	j, err := scanDigits(i)
	if err != nil {
		return "", 0, err
	}

	// '.' — дробная часть: '1.' / '1.e9' — ошибка (§2.1); '1.5' — Float.
	if j < n && text[j] == '.' {
		if j+1 >= n || !isDecDigit(text[j+1]) {
			return "", 0, errf(line, j+1, "'1.' requires a digit after the dot")
		}
		j++
		j, err = scanDigits(j)
		if err != nil {
			return "", 0, err
		}
	}

	// экспонента 'e'/'E' [+-]? digits — Float; '1e' — ошибка (A3.3).
	if j < n && (text[j] == 'e' || text[j] == 'E') {
		k := j + 1
		if k < n && (text[k] == '+' || text[k] == '-') {
			k++
		}
		expStart := k
		for k < n && isDecDigit(text[k]) {
			k++
		}
		if k == expStart {
			return "", 0, errf(line, j+1, "exponent requires digits")
		}
		if k < n && text[k] == '_' {
			return "", 0, errf(line, k+1, "underscore must be between digits")
		}
		j = k
	}

	return text[i:j], j, nil
}

// scanString читает "..." с escape и интерполяцией \(...) (A4.1).
// Возвращает тело (без кавычек) и индекс за закрывающей кавычкой.
func scanString(text string, i, line int) (string, int, error) {
	n := len(text)
	bodyStart := i + 1
	j := bodyStart
	for j < n {
		c := text[j]
		switch c {
		case '"':
			return text[bodyStart:j], j + 1, nil
		case '\n':
			return "", 0, errf(line, j+1, "unclosed string literal")
		case '\\':
			if j+1 >= n {
				return "", 0, errf(line, j+1, "trailing backslash in string")
			}
			esc := text[j+1]
			switch esc {
			case 'n', 't', 'r', '0', '\\', '"':
				j += 2
			case '(':
				// интерполяция \(...) — до соответствующей ')' с учётом
				// вложенных () [] {} и вложенных строк (§2.2, П-003).
				end, err := scanInterpolation(text, j+1, line)
				if err != nil {
					return "", 0, err
				}
				j = end
			case 'u':
				end, err := scanUnicodeEscape(text, j, line)
				if err != nil {
					return "", 0, err
				}
				j = end
			default:
				return "", 0, errf(line, j+1, "invalid escape '\\%c' in string", esc)
			}
		default:
			j++
		}
	}
	return "", 0, errf(line, i+1, "unclosed string literal")
}

// scanInterpolation от \( до соответствующей ')' (П-003: незакрытая — ошибка).
func scanInterpolation(text string, open int, line int) (int, error) {
	depth := 1
	i := open + 1 // сам '(' уже учтён в depth
	for i < len(text) {
		switch text[i] {
		case '"':
			_, end, err := scanString(text, i, line)
			if err != nil {
				// Вложенная строка не закрылась: эта кавычка — закрывающая
				// кавычка внешней строки → интерполяция без ')' (П-003).
				return 0, errf(line, open-1, "unclosed interpolation '\\(' (П-003)")
			}
			i = end
			continue
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
			if depth == 0 {
				return i + 1, nil
			}
		}
		i++
	}
	return 0, errf(line, open-1, "unclosed interpolation '\\(' (П-003)")
}

// scanBytes читает b"..." — escape без интерполяции (A4.2).
func scanBytes(text string, i, line int) (string, int, error) {
	n := len(text)
	bodyStart := i + 2
	j := bodyStart
	for j < n {
		c := text[j]
		switch c {
		case '"':
			return text[bodyStart:j], j + 1, nil
		case '\n':
			return "", 0, errf(line, j+1, "unclosed bytes literal")
		case '\\':
			if j+1 >= n {
				return "", 0, errf(line, j+1, "trailing backslash in bytes")
			}
			esc := text[j+1]
			switch esc {
			case 'n', 't', 'r', '0', '\\', '"':
				j += 2
			case 'x':
				if j+3 >= n || !isHexDigit(text[j+2]) || !isHexDigit(text[j+3]) {
					return "", 0, errf(line, j+1, "'\\xHH' requires two hex digits")
				}
				j += 4
			case '(', 'u':
				return "", 0, errf(line, j+1, "escape '\\%c' forbidden in bytes literal", esc)
			default:
				return "", 0, errf(line, j+1, "invalid escape '\\%c' in bytes", esc)
			}
		default:
			j++
		}
	}
	return "", 0, errf(line, i+1, "unclosed bytes literal")
}

// scanRegex читает rx"..." — \" и \\ — литералы, остальные \X сохраняются (A4.3).
func scanRegex(text string, i, line int) (string, int, error) {
	n := len(text)
	bodyStart := i + 3
	j := bodyStart
	for j < n {
		c := text[j]
		switch c {
		case '"':
			return text[bodyStart:j], j + 1, nil
		case '\n':
			return "", 0, errf(line, j+1, "unclosed regex literal")
		case '\\':
			if j+1 >= n {
				return "", 0, errf(line, j+1, "trailing backslash in regex")
			}
			j += 2
		default:
			j++
		}
	}
	return "", 0, errf(line, i+1, "unclosed regex literal")
}

// scanDecimal читает dec"..." и валидирует тело (A4.4, П-004).
func scanDecimal(text string, i, line int) (string, int, error) {
	n := len(text)
	j := i + 4 // после dec"
	bodyStart := j
	for j < n && text[j] != '"' {
		if text[j] == '\n' {
			return "", 0, errf(line, j+1, "unclosed decimal literal")
		}
		j++
	}
	if j >= n {
		return "", 0, errf(line, i+1, "unclosed decimal literal")
	}
	body := text[bodyStart:j]
	if err := validateDecimalBody(body, line, bodyStart+1); err != nil {
		return "", 0, err
	}
	return body, j + 1, nil
}

// validateDecimalBody удалена → escape.go.

func isLower(c byte) bool { return c >= 'a' && c <= 'z' }
func isUpper(c byte) bool { return c >= 'A' && c <= 'Z' }
