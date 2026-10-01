package term

import (
	"strings"
	"unicode"

	"github.com/it1ro/brig-lang/internal/termio"
)

// killRingSize — сколько удалённых фрагментов помнит Ctrl-Y / Alt-Y.
const killRingSize = 16

// yankState — последняя вставка Ctrl-Y: Alt-Y заменяет [from, to)
// предыдущей записью kill ring.
type yankState struct {
	from, to int
	idx      int // индекс вставленной записи в kills
}

// lastArgState — последняя вставка Alt-.: повторный Alt-. заменяет
// [from, to) последним аргументом более старой записи истории.
type lastArgState struct {
	from, to int
	hist     int
}

// kill удаляет [from, to) в kill ring. Подряд идущие удаления
// склеиваются в одну запись: back — удаление назад (Ctrl-W, Ctrl-U),
// его текст встаёт перед прежним.
func (e *Editor) kill(from, to int, back bool) {
	if from >= to {
		return
	}
	text := string(e.buf.r[from:to])
	e.buf.del(from, to)
	if isKill(e.last) && len(e.kills) > 0 {
		n := len(e.kills) - 1
		if back {
			e.kills[n] = text + e.kills[n]
		} else {
			e.kills[n] += text
		}
		return
	}
	e.kills = append(e.kills, text)
	if len(e.kills) > killRingSize {
		e.kills = e.kills[len(e.kills)-killRingSize:]
	}
}

func isKill(k termio.KeyCode) bool {
	switch k {
	case termio.KeyKillEnd, termio.KeyKillStart, termio.KeyKillWord,
		termio.KeyKillWordAlnum, termio.KeyKillWordRight:
		return true
	}
	return false
}

// yankTop вставляет последнюю запись kill ring (Ctrl-Y).
func (e *Editor) yankTop() {
	if len(e.kills) == 0 {
		return
	}
	e.yankAt(len(e.kills) - 1)
}

// yankPop сразу после Ctrl-Y или Alt-Y заменяет вставку предыдущей
// записью kill ring (Alt-Y).
func (e *Editor) yankPop() {
	if len(e.kills) == 0 || (e.last != termio.KeyYank && e.last != termio.KeyYankPop) {
		return
	}
	e.buf.del(e.yank.from, e.yank.to)
	e.yankAt((e.yank.idx - 1 + len(e.kills)) % len(e.kills))
}

func (e *Editor) yankAt(i int) {
	from := e.buf.pos
	e.buf.insert(e.kills[i])
	e.yank = yankState{from: from, to: e.buf.pos, idx: i}
}

// undoStep возвращает буфер к состоянию до последней правки (Ctrl-_).
func (e *Editor) undoStep() {
	n := len(e.undo)
	if n == 0 {
		return
	}
	e.buf = e.undo[n-1]
	e.undo = e.undo[:n-1]
}

// insertLastArg вставляет последний аргумент предыдущего ввода (Alt-.);
// повторный Alt-. берёт его из более старого ввода.
func (e *Editor) insertLastArg() {
	h := e.historyLen()
	if e.last == termio.KeyLastArg {
		e.buf.del(e.lastArg.from, e.lastArg.to)
		h = e.lastArg.hist
	}
	for i := h - 1; i >= 0; i-- {
		arg := lastArg(e.History.At(i))
		if arg == "" {
			continue
		}
		from := e.buf.pos
		e.buf.insert(arg)
		e.lastArg = lastArgState{from: from, to: e.buf.pos, hist: i}
		return
	}
	e.lastArg = lastArgState{from: e.buf.pos, to: e.buf.pos, hist: 0}
}

// lastArg — последний аргумент ввода: последнее слово, без скобок и
// запятых вокруг (`f(a, xs)` → `xs`).
func lastArg(entry string) string {
	fields := strings.FieldsFunc(entry, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune("(),[]{}", r)
	})
	if len(fields) == 0 {
		return ""
	}
	return fields[len(fields)-1]
}

// external правит ввод в $EDITOR (Ctrl-X Ctrl-E). Экран после внешнего
// редактора рисуется заново с текущей строки.
func (e *Editor) external() {
	if e.External == nil {
		return
	}
	text, err := e.External(e.buf.String())
	e.crow = 0
	if err != nil {
		return
	}
	e.buf.set(strings.TrimRight(text, "\n"))
}

// indent — Tab в отступе: перед курсором в строке только пробелы, Tab
// дописывает пробелы до следующего уровня. Пустая первая строка — не
// отступ: там Tab показывает все имена.
func (e *Editor) indent() bool {
	b := &e.buf
	start := b.lineStart()
	if start == 0 && b.pos == 0 {
		return false
	}
	for _, c := range b.r[start:b.pos] {
		if c != ' ' {
			return false
		}
	}
	w := e.IndentWidth
	if w <= 0 {
		w = 4
	}
	col := b.pos - b.lineStart()
	b.insert(strings.Repeat(" ", w-col%w))
	return true
}

// dedent снимает один уровень отступа строки курсора (Shift-Tab).
func (e *Editor) dedent() {
	b := &e.buf
	start := b.lineStart()
	n := 0
	for start+n < len(b.r) && b.r[start+n] == ' ' {
		n++
	}
	if n == 0 {
		return
	}
	w := e.IndentWidth
	if w <= 0 {
		w = 4
	}
	drop := n % w
	if drop == 0 {
		drop = w
	}
	pos := b.pos
	b.del(start, start+drop)
	b.pos = max(start, pos-drop)
}

// ghostLine — что подсказка истории показывает на экране: первая строка
// хвоста и `…`, если в записи есть ещё строки.
func ghostLine(ghost string) string {
	line, rest, more := strings.Cut(ghost, "\n")
	if more && strings.TrimSpace(rest) != "" || more && line == "" {
		return line + " …"
	}
	return line
}

// searchShown — буфер Ctrl-R с выделенным совпадением: с цветом —
// инверсия, без цвета — текст как есть.
func (e *Editor) searchShown(src string) string {
	s := e.search
	rs := []rune(src)
	from := e.buf.pos
	to := from + len(s.query)
	if !e.Color || from < 0 || to > len(rs) {
		return src
	}
	var b strings.Builder
	b.WriteString(string(rs[:from]))
	for _, line := range strings.SplitAfter(string(rs[from:to]), "\n") {
		text := strings.TrimSuffix(line, "\n")
		if text != "" {
			b.WriteString("\x1b[7m" + text + "\x1b[27m")
		}
		if len(text) < len(line) {
			b.WriteByte('\n')
		}
	}
	b.WriteString(string(rs[to:]))
	return b.String()
}

// wordStartSpace — начало слова до пробела перед курсором вместе с
// пробелами за ним (Ctrl-W, unix-word-rubout).
func (b *buffer) wordStartSpace() int {
	i := b.pos
	for i > 0 && unicode.IsSpace(b.r[i-1]) {
		i--
	}
	for i > 0 && !unicode.IsSpace(b.r[i-1]) {
		i--
	}
	return i
}

// wordStartAlnum — начало слова из букв и цифр перед курсором
// (Alt-Backspace, backward-kill-word).
func (b *buffer) wordStartAlnum() int {
	i := b.pos
	for i > 0 && !isWord(b.r[i-1]) {
		i--
	}
	for i > 0 && isWord(b.r[i-1]) {
		i--
	}
	return i
}

// wordEndAlnum — конец слова из букв и цифр после курсора (Alt-D).
func (b *buffer) wordEndAlnum() int {
	i := b.pos
	for i < len(b.r) && !isWord(b.r[i]) {
		i++
	}
	for i < len(b.r) && isWord(b.r[i]) {
		i++
	}
	return i
}

// transpose меняет местами символ перед курсором и под ним (Ctrl-T); в
// конце строки — два последних символа.
func (b *buffer) transpose() {
	if b.pos == 0 || len(b.r) < 2 {
		return
	}
	i := b.pos
	if i == len(b.r) || b.r[i] == '\n' {
		i--
	}
	if i == 0 || b.r[i] == '\n' || b.r[i-1] == '\n' {
		return
	}
	b.r[i-1], b.r[i] = b.r[i], b.r[i-1]
	b.pos = i + 1
}
