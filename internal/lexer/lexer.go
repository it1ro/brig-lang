package lexer

import (
	"fmt"
	"strings"
	"unicode/utf8"
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
func Lex(src string) ([]Token, error) {
	toks, err := newLexer(src).run()
	if !isASCII(src) {
		toks, err = codePointCols(src, toks, err)
	}
	return toks, err
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// codePointCols переводит байтовые колонки, посчитанные лексером, в
// колонки по Unicode code points (§E.2).
func codePointCols(src string, toks []Token, err error) ([]Token, error) {
	lines := strings.Split(src, "\n")
	conv := func(line, col int) int {
		if line < 1 || line > len(lines) || col < 2 {
			return col
		}
		text := lines[line-1]
		b := col - 1
		if b > len(text) {
			return col
		}
		return utf8.RuneCountInString(text[:b]) + 1
	}
	for i := range toks {
		toks[i].Col = conv(toks[i].Line, toks[i].Col)
	}
	if le, ok := err.(*Error); ok {
		le.Col = conv(le.Line, le.Col)
	}
	return toks, err
}

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

	// A5.4 offside-мини-блоки внутри скобок.
	blockDepth int
}

func newLexer(src string) *lexer {
	return &lexer{
		src:         src,
		indentStack: []int{0},
		firstLine:   true,
		line:        1,
		blockDepth:  0,
	}
}

func (l *lexer) run() ([]Token, error) {
	for {
		pl, ok := l.nextPhysLine()
		if !ok {
			break
		}
		if !pl.hasTok {
			continue
		}
		if err := l.processLine(pl); err != nil {
			return nil, err
		}
	}

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
	// A5.4: offside-мини-блоки внутри скобок.
	if l.blockDepth > 0 {
		if !l.firstLine {
			l.emit(NEWLINE, "\n")
		}
		indent := countLeadingSpaces(pl.text)
		if indent <= l.top() {
			l.blockDepth = 0
		}
		return l.lexLine(pl)
	}

	// parenDepth > 0 без активного mini-block: offside отключён, но
	// NEWLINE может эмитироваться как разделитель элементов (A5.4 §D.5).
	if l.parenDepth > 0 {
		ft := firstToken(pl.text)
		if isBlockOpener(ft) && l.blockDepth == 0 {
			l.parenDepth = 0
			l.blockDepth = 1
			l.stmtIndent = 0
			return l.lexLine(pl)
		}
		if !l.firstLine && shouldEmitBracketNewline(l.lastTokenLit(), ft) {
			l.emit(NEWLINE, "\n")
		}
		l.firstLine = false
		return l.lexLine(pl)
	}

	indent := countLeadingSpaces(pl.text)
	ft := firstToken(pl.text)
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

// lastTokenLit возвращает Lit последнего эмитированного токена
// (или "" если токенов ещё нет). Нужен для решения об эмиссии
// NEWLINE-разделителя внутри скобок (A5.4 §D.5).
func (l *lexer) lastTokenLit() string {
	if len(l.tokens) == 0 {
		return ""
	}
	return l.tokens[len(l.tokens)-1].Lit
}

// shouldEmitBracketNewline решает, нужен ли NEWLINE между элементами
// внутри бракетного литерала (A5.4 §D.5). NEWLINE эмитируется, если:
//   - предыдущий токен может завершать элемент (не открывающая скобка,
//     не запятая);
//   - следующий токен может начинать элемент (не закрывающая скобка,
//     не запятая).
func shouldEmitBracketNewline(prev, next string) bool {
	if prev == "" || next == "" {
		return false
	}
	switch prev {
	case "(", "[", "{", "%[", "%{", ",":
		return false
	}
	switch next {
	case ")", "]", "}", ",":
		return false
	}
	return true
}

// physLine — одна физическая строка.
type physLine struct {
	text   string
	line   int
	hasTok bool
}

// nextPhysLine читает следующую физическую строку; ok=false при EOF.
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
			end = l.pos - 1
		}
		text := l.src[start:end]
		l.line++
		if !blankOrComment(text) {
			return physLine{text: text, line: l.line - 1, hasTok: true}, true
		}
	}
	return physLine{}, false
}

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
			return errf(pl.line, i+1, "tab character is forbidden")
		case '#':
			return nil
		}

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

		if c == ':' {
			// §1.5 / B.2: после LOWER_IDENT/UPPER_IDENT/)/]/} всегда COLON;
			// иначе ':' + [a-z_] → ATOM.
			if !l.colonAfterValue() && i+1 < n && (isLower(text[i+1]) || text[i+1] == '_') {
				j := scanIdent(text, i+1)
				l.addToken(Token{Type: ATOM, Lit: text[i:j]}, pl, i)
				i = j
				continue
			}
			l.addToken(Token{Type: COLON, Lit: ":"}, pl, i)
			i++
			continue
		}

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

		if isUpper(c) {
			j := scanIdent(text, i)
			l.addToken(Token{Type: UPPER_IDENT, Lit: text[i:j]}, pl, i)
			i = j
			continue
		}

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
			if i+1 < n && (isLower(text[i+1]) || isUpper(text[i+1]) || isDecDigit(text[i+1])) {
				return errf(pl.line, i+1, "identifier must not start with '_'")
			}
			l.addToken(Token{Type: WILDCARD, Lit: "_"}, pl, i)
			i++
			continue
		}

		if c == '%' {
			if i+1 < n && text[i+1] == '[' {
				l.parenDepth++ // FIX: %[ теперь считается скобкой
				l.addToken(Token{Type: VEC_OPEN, Lit: "%["}, pl, i)
				i += 2
				continue
			}
			if i+1 < n && text[i+1] == '{' {
				l.parenDepth++ // FIX: %{ теперь считается скобкой
				l.addToken(Token{Type: MAP_OPEN, Lit: "%{"}, pl, i)
				i += 2
				continue
			}
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

func isBlockOpener(tok string) bool {
	switch tok {
	case "fn", "match", "recv", "with", "trap", "if":
		return true
	}
	return false
}

func (l *lexer) addToken(t Token, pl physLine, i int) {
	t.Line = pl.line
	t.Col = i + 1
	l.tokens = append(l.tokens, t)
}

// colonAfterValue — предыдущий токен заставляет ':' эмититься как COLON
// (§1.5): LOWER_IDENT, UPPER_IDENT, ')', ']', '}'.
func (l *lexer) colonAfterValue() bool {
	if len(l.tokens) == 0 {
		return false
	}
	switch l.tokens[len(l.tokens)-1].Type {
	case LOWER_IDENT, UPPER_IDENT, RPAREN, RBRACKET, RBRACE:
		return true
	}
	return false
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
		if j < len(s) && s[j] == '?' {
			j++
		}
	case isDecDigit(c):
		for j < len(s) && isDecDigit(s[j]) {
			j++
		}
	case c == '"':
		k := i + 1
		for k < len(s) && s[k] != '"' {
			if s[k] == '\\' && k+1 < len(s) {
				k += 2
				continue
			}
			k++
		}
		if k < len(s) {
			k++
		}
		return s[i:k]
	default:
		for _, op := range twoCharOps {
			if prefixAt(s, j, op.text) {
				return op.text
			}
		}
		if i < len(s) {
			return s[i : i+1]
		}
		return ""
	}
	return s[i:j]
}

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

func hasSuffixQ(word string) bool {
	return len(word) > 0 && word[len(word)-1] == '?'
}

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
				// '_' только между цифрами — не сразу после x/b/o (§3.1).
				if c == '_' && digits > 0 && j+1 < n && isDigitForBase(text[j+1], base) {
					j++
					continue
				}
				break
			}
			if digits == 0 {
				if j < n && text[j] == '_' {
					return "", 0, errf(line, j+1, "underscore must be between digits")
				}
				return "", 0, errf(line, i+1, "digit expected after radix prefix")
			}
			if j < n && text[j] == '_' {
				return "", 0, errf(line, j+1, "underscore must be between digits")
			}
			// Невалидная цифра сразу после литерала (0b102) — ошибка, а не
			// «отрезать» префикс и оставить хвост отдельным токеном.
			if j < n && (isDecDigit(text[j]) || isLower(text[j]) || isUpper(text[j])) {
				return "", 0, errf(line, j+1, "invalid digit in base-%d literal", base)
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

func scanInterpolation(text string, open int, line int) (int, error) {
	depth := 1
	i := open + 1
	for i < len(text) {
		switch text[i] {
		case '"':
			_, end, err := scanString(text, i, line)
			if err != nil {
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

func scanDecimal(text string, i, line int) (string, int, error) {
	n := len(text)
	j := i + 4
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

func isLower(c byte) bool { return c >= 'a' && c <= 'z' }
func isUpper(c byte) bool { return c >= 'A' && c <= 'Z' }
