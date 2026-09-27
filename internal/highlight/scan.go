package highlight

import "unicode/utf8"

type tokKind int

const (
	tLower tokKind = iota
	tUpper
	tKeyword
	tAtom
	tString
	tBytes
	tRegex
	tNumber
	tComment
	tDoc
	tOp
	tPunct
	tInterp
	tWild
	tError
)

type token struct {
	kind       tokKind
	start, end int
}

func (t token) text(src string) string {
	if t.start < 0 || t.end > len(src) || t.end < t.start {
		return ""
	}
	return src[t.start:t.end]
}

func (t token) delta(src string) int {
	switch t.text(src) {
	case "(", "[", "{", "%[", "%{", "\\(":
		return 1
	case ")", "]", "}":
		return -1
	default:
		return 0
	}
}

func (t token) base() (Class, bool) {
	switch t.kind {
	case tKeyword:
		return Keyword, true
	case tAtom:
		return Atom, true
	case tString:
		return String, true
	case tBytes:
		return Bytes, true
	case tRegex:
		return Regex, true
	case tNumber:
		return Number, true
	case tComment:
		return Comment, true
	case tDoc:
		return Doc, true
	case tOp:
		return Op, true
	case tPunct:
		return Punct, true
	case tInterp:
		return Interp, true
	case tError:
		return Error, true
	default:
		return "", false
	}
}

type scanner struct {
	src  string
	toks []token
}

func scanSrc(src string) []token {
	s := &scanner{src: src}
	s.scan(0, false)
	return s.toks
}

func (s *scanner) emit(k tokKind, start, end int) {
	if start < 0 {
		start = 0
	}
	if end > len(s.src) {
		end = len(s.src)
	}
	if end <= start {
		return
	}
	s.toks = append(s.toks, token{kind: k, start: start, end: end})
}

func (s *scanner) has(i int, p string) bool {
	return i+len(p) <= len(s.src) && s.src[i:i+len(p)] == p
}

// scan читает [i, конец). stop — остановиться на ')' при глубине 0
// (закрытие интерполяции). Глубина локальна: вложенный вызов из
// строки не портит внешнюю.
func (s *scanner) scan(i int, stop bool) int {
	depth := 0
	for i < len(s.src) {
		if stop && s.src[i] == ')' && depth == 0 {
			return i
		}
		c := s.src[i]
		switch c {
		case ' ', '\t', '\r':
			i++
			continue
		case '\n':
			i++
			continue
		case '#':
			i = s.comment(i)
			continue
		case '"':
			i = s.str(i)
			continue
		}
		switch {
		case s.has(i, `dec"`):
			i = s.raw(i, 4, tNumber)
		case s.has(i, `rx"`):
			i = s.raw(i, 3, tRegex)
		case s.has(i, `b"`):
			i = s.raw(i, 2, tBytes)
		case c == ':':
			i = s.colon(i)
		case isDigit(c):
			i = s.number(i)
		case isLower(c) || isUpper(c) || c == '_':
			i = s.ident(i)
		case c == '%':
			if i+1 < len(s.src) && (s.src[i+1] == '[' || s.src[i+1] == '{') {
				depth++
				s.emit(tPunct, i, i+2)
				i += 2
			} else {
				s.emit(tError, i, i+1)
				i++
			}
		case c == '(' || c == '[' || c == '{':
			depth++
			s.emit(tPunct, i, i+1)
			i++
		case c == ')' || c == ']' || c == '}':
			if depth > 0 {
				depth--
			}
			s.emit(tPunct, i, i+1)
			i++
		case c == ',' || c == ';':
			s.emit(tPunct, i, i+1)
			i++
		default:
			if n := s.operator(i); n > 0 {
				i += n
				continue
			}
			_, sz := utf8.DecodeRuneInString(s.src[i:])
			if sz < 1 {
				sz = 1
			}
			s.emit(tError, i, i+sz)
			i += sz
		}
	}
	return i
}

func (s *scanner) comment(i int) int {
	kind := tComment
	if docAt(s.src, i) {
		kind = tDoc
	}
	start := i
	for i < len(s.src) && s.src[i] != '\n' {
		i++
	}
	s.emit(kind, start, i)
	return i
}

func docAt(src string, hash int) bool {
	k := hash
	for k > 0 && src[k-1] != '\n' {
		k--
	}
	for k < hash && src[k] == ' ' {
		k++
	}
	if k != hash || hash+2 > len(src) || src[hash:hash+2] != "##" {
		return false
	}
	return hash+2 >= len(src) || src[hash+2] != '#'
}

func (s *scanner) str(i int) int {
	start := i
	i++
	for i < len(s.src) {
		switch s.src[i] {
		case '"':
			s.emit(tString, start, i+1)
			return i + 1
		case '\n':
			s.emit(tString, start, i)
			return i
		case '\\':
			if i+1 < len(s.src) && s.src[i+1] == '(' {
				s.emit(tString, start, i)
				s.emit(tInterp, i, i+2)
				i = s.scan(i+2, true)
				if i < len(s.src) && s.src[i] == ')' {
					s.emit(tInterp, i, i+1)
					i++
				}
				start = i
				continue
			}
			if i+1 < len(s.src) {
				i += 2
				continue
			}
			i++
		default:
			i++
		}
	}
	s.emit(tString, start, i)
	return i
}

func (s *scanner) raw(i, prefix int, k tokKind) int {
	start := i
	i += prefix
	for i < len(s.src) {
		switch s.src[i] {
		case '"':
			s.emit(k, start, i+1)
			return i + 1
		case '\n':
			s.emit(k, start, i)
			return i
		case '\\':
			if i+1 < len(s.src) {
				i += 2
			} else {
				i++
			}
		default:
			i++
		}
	}
	s.emit(k, start, i)
	return i
}

func (s *scanner) colon(i int) int {
	if !s.afterValue() && i+1 < len(s.src) && (isLower(s.src[i+1]) || s.src[i+1] == '_') {
		j := i + 1
		for j < len(s.src) && isIdent(s.src[j]) {
			j++
		}
		s.emit(tAtom, i, j)
		return j
	}
	s.emit(tPunct, i, i+1)
	return i + 1
}

func (s *scanner) afterValue() bool {
	if len(s.toks) == 0 {
		return false
	}
	t := s.toks[len(s.toks)-1]
	if t.kind == tLower || t.kind == tUpper {
		return true
	}
	if t.kind != tPunct || t.end <= t.start {
		return false
	}
	c := s.src[t.end-1]
	return c == ')' || c == ']' || c == '}'
}

func (s *scanner) number(i int) int {
	start := i
	if s.src[i] == '0' && i+1 < len(s.src) {
		switch s.src[i+1] {
		case 'x', 'X', 'b', 'B', 'o', 'O':
			i += 2
			for i < len(s.src) && (isHex(s.src[i]) || s.src[i] == '_') {
				i++
			}
			s.emit(tNumber, start, i)
			return i
		}
	}
	for i < len(s.src) && (isDigit(s.src[i]) || s.src[i] == '_') {
		i++
	}
	if i+1 < len(s.src) && s.src[i] == '.' && isDigit(s.src[i+1]) {
		i++
		for i < len(s.src) && (isDigit(s.src[i]) || s.src[i] == '_') {
			i++
		}
	}
	if i < len(s.src) && (s.src[i] == 'e' || s.src[i] == 'E') {
		j := i + 1
		if j < len(s.src) && (s.src[j] == '+' || s.src[j] == '-') {
			j++
		}
		if j < len(s.src) && isDigit(s.src[j]) {
			i = j
			for i < len(s.src) && (isDigit(s.src[i]) || s.src[i] == '_') {
				i++
			}
		}
	}
	s.emit(tNumber, start, i)
	return i
}

func (s *scanner) ident(i int) int {
	start := i
	i++
	for i < len(s.src) && isIdent(s.src[i]) {
		i++
	}
	if i < len(s.src) && s.src[i] == '?' {
		i++
	}
	word := s.src[start:i]
	kind := tLower
	switch {
	case word == "_":
		kind = tWild
	case isUpper(s.src[start]):
		kind = tUpper
	case len(word) > 1 && word[0] == '_' && (isUpper(word[1]) || isDigit(word[1])):
		kind = tError
	case !hasQ(word) && keywords[word]:
		kind = tKeyword
	}
	s.emit(kind, start, i)
	return i
}

func hasQ(w string) bool { return len(w) > 0 && w[len(w)-1] == '?' }

var twoOps = []string{"|>", "**", "==", "!=", "<=", ">=", "..", "->", "<-", "=>"}

func (s *scanner) operator(i int) int {
	if i+1 < len(s.src) {
		pair := s.src[i : i+2]
		for _, op := range twoOps {
			if pair == op {
				s.emit(tOp, i, i+2)
				return 2
			}
		}
	}
	switch s.src[i] {
	case '+', '-', '*', '/', '<', '>', '=', '.':
		s.emit(tOp, i, i+1)
		return 1
	default:
		return 0
	}
}

var keywords = map[string]bool{
	"fn": true, "match": true, "recv": true, "with": true,
	"else": true, "if": true, "then": true, "after": true,
	"when": true, "alias": true, "import": true,
	"module": true, "type": true, "ensure": true,
	"trap": true, "and": true, "or": true, "not": true,
	"div": true, "rem": true, "to": true, "true": true,
	"false": true, "as": true, "pub": true, "quote": true,
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func isHex(c byte) bool {
	return isDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}
func isLower(c byte) bool { return c >= 'a' && c <= 'z' }
func isUpper(c byte) bool { return c >= 'A' && c <= 'Z' }
func isIdent(c byte) bool {
	return isLower(c) || isUpper(c) || isDigit(c) || c == '_'
}
