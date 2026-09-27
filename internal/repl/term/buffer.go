package term

import (
	"strings"
	"unicode"
)

// buffer — текст ввода и курсор. Многострочный ввод — один буфер:
// строки разделены '\n', pos — индекс руны перед курсором.
type buffer struct {
	r   []rune
	pos int
}

func (b *buffer) String() string { return string(b.r) }

func (b *buffer) set(s string) {
	b.r = []rune(s)
	b.pos = len(b.r)
}

func (b *buffer) insert(s string) {
	rs := []rune(s)
	b.r = append(b.r[:b.pos], append(rs, b.r[b.pos:]...)...)
	b.pos += len(rs)
}

// del удаляет руны [from, to) и ставит курсор в from.
func (b *buffer) del(from, to int) {
	b.r = append(b.r[:from], b.r[to:]...)
	b.pos = from
}

// lineStart и lineEnd — границы строки, в которой стоит курсор.
func (b *buffer) lineStart() int {
	i := b.pos
	for i > 0 && b.r[i-1] != '\n' {
		i--
	}
	return i
}

func (b *buffer) lineEnd() int {
	i := b.pos
	for i < len(b.r) && b.r[i] != '\n' {
		i++
	}
	return i
}

func (b *buffer) firstLine() bool { return b.lineStart() == 0 }
func (b *buffer) lastLine() bool  { return b.lineEnd() == len(b.r) }
func (b *buffer) atEnd() bool     { return b.pos == len(b.r) }
func (b *buffer) multiline() bool { return strings.ContainsRune(string(b.r), '\n') }

// lineCol — номер строки курсора и его колонка в рунах.
func (b *buffer) lineCol() (line, col int) {
	for _, c := range b.r[:b.pos] {
		if c == '\n' {
			line++
		}
	}
	return line, b.pos - b.lineStart()
}

func (b *buffer) left() {
	if b.pos > 0 {
		b.pos--
	}
}

func (b *buffer) right() {
	if b.pos < len(b.r) {
		b.pos++
	}
}

func (b *buffer) home() { b.pos = b.lineStart() }
func (b *buffer) end()  { b.pos = b.lineEnd() }

// up и down переводят курсор на соседнюю строку буфера в ту же колонку
// (или в конец более короткой строки).
func (b *buffer) up() {
	col := b.pos - b.lineStart()
	b.pos = b.lineStart() - 1
	b.pos = min(b.lineStart()+col, b.pos)
}

func (b *buffer) down() {
	col := b.pos - b.lineStart()
	b.pos = b.lineEnd() + 1
	b.pos = min(b.pos+col, b.lineEnd())
}

func (b *buffer) backspace() {
	if b.pos > 0 {
		b.del(b.pos-1, b.pos)
	}
}

func (b *buffer) delete() {
	if b.pos < len(b.r) {
		b.del(b.pos, b.pos+1)
	}
}

// killEnd удаляет текст до конца строки; в конце строки — её перевод.
func (b *buffer) killEnd() {
	if e := b.lineEnd(); e > b.pos {
		b.del(b.pos, e)
	} else {
		b.delete()
	}
}

// killStart удаляет текст от начала строки до курсора.
func (b *buffer) killStart() { b.del(b.lineStart(), b.pos) }

// killWord удаляет слово перед курсором вместе с пробелами за ним
// (граница слова — пробельный символ, как unix-word-rubout).
func (b *buffer) killWord() {
	i := b.pos
	for i > 0 && unicode.IsSpace(b.r[i-1]) {
		i--
	}
	for i > 0 && !unicode.IsSpace(b.r[i-1]) {
		i--
	}
	b.del(i, b.pos)
}

// wordLeft и wordRight — к началу/концу слова из букв и цифр.
func (b *buffer) wordLeft() {
	for b.pos > 0 && !isWord(b.r[b.pos-1]) {
		b.pos--
	}
	for b.pos > 0 && isWord(b.r[b.pos-1]) {
		b.pos--
	}
}

func (b *buffer) wordRight() {
	for b.pos < len(b.r) && !isWord(b.r[b.pos]) {
		b.pos++
	}
	for b.pos < len(b.r) && isWord(b.r[b.pos]) {
		b.pos++
	}
}

func isWord(c rune) bool { return c == '_' || unicode.IsLetter(c) || unicode.IsDigit(c) }

// onlySpacesBefore — перед курсором в его строке только пробелы, и их
// хотя бы один: курсор стоит в конце отступа.
func (b *buffer) onlySpacesBefore() bool {
	s := b.lineStart()
	if s == b.pos {
		return false
	}
	for _, c := range b.r[s:b.pos] {
		if c != ' ' {
			return false
		}
	}
	return true
}
