// Package term — редактор строки консоли REPL (§11.4): редактирование
// многострочного ввода, автоотступ, bracketed paste, история и
// Ctrl-R. Редактор работает на io.Reader/io.Writer и о языке не знает:
// полноту ввода, отступ, раскраску и подсказку он получает хуками.
// Raw mode настоящего терминала — Terminal.
package term

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/it1ro/brig-lang/internal/termio"
)

// Editor читает порции ввода с приглашением. Поток байтов — клавиши
// терминала в raw mode, вывод — ANSI-последовательности.
type Editor struct {
	// NeedMore — ввод src не завершён: Enter в конце буфера переводит
	// строку. nil — Enter в конце буфера всегда завершает ввод.
	NeedMore func(src string) bool
	// Indent — отступ новой строки после текста src до курсора. nil —
	// новая строка без отступа.
	Indent func(src string) string
	// IndentWidth — ширина уровня отступа: Backspace в конце отступа
	// снимает уровень. 0 — Backspace удаляет один пробел.
	IndentWidth int
	// Highlight раскрашивает буфер. cursor — индекс руны, где стоит
	// курсор. Результат сохраняет число строк: ANSI-коды места не
	// занимают, направляющая отступа может заменить пробел на символ
	// той же ширины. nil — простой текст.
	Highlight func(src string, cursor int) string
	// Hint — суффикс истории после курсора. Редактор зовёт его только в
	// конце строки курсора. На экране — первая строка, серым и только
	// при Color; → и End вставляют весь суффикс и без цвета тоже.
	// nil — без хвоста.
	Hint func(src string, pos int) string
	// Complete — кандидаты Tab. Span [From, To) редактор заменяет сам,
	// не разбирая, где в тексте имя.
	Complete func(src string, pos int) Completion
	// Signature — строка под вводом. text без SGR; argStart и argEnd —
	// руны метки текущего аргумента, -1 — метки нет.
	Signature func(src string, pos int) (text string, argStart, argEnd int)
	// Color — рисовать серый хвост и подчёркивать аргумент. На клавиши
	// не влияет: без цвета хвост просто не виден, а аргумент в скобках.
	Color bool
	// History — вводы для ↑/↓ и Ctrl-R; nil — без истории. Добавляет
	// в неё вызывающий.
	History *History
	// Width — ширина терминала в колонках; nil или не больше 0 — 80.
	Width func() int

	in  *bufio.Reader
	out io.Writer

	buf          buffer
	prompt, cont string
	crow         int // строка экрана с курсором от начала ввода
	hist         int // запись истории в буфере; History.Len() — черновик
	draft        string
	search       *search
	noHint       bool
	menu         []string
	ghost        string
}

// search — состояние Ctrl-R.
type search struct {
	query  []rune
	match  int // запись истории в буфере; -1 — совпадения ещё не было
	failed bool
	saved  buffer
}

// NewEditor создаёт редактор, читающий клавиши из in и рисующий в out.
func NewEditor(in io.Reader, out io.Writer) *Editor {
	return &Editor{in: bufio.NewReader(in), out: out}
}

// ReadInput выводит prompt (продолжения ввода — cont) и возвращает
// порцию ввода с завершающим '\n'. Ctrl-D на пустом буфере и конец
// потока — io.EOF; Ctrl-C очищает буфер и начинает ввод заново.
func (e *Editor) ReadInput(prompt, cont string) (string, error) {
	e.prompt, e.cont = prompt, cont
	e.reset()
	for {
		k, err := termio.ReadKey(e.in)
		if err != nil {
			e.finish("")
			return "", err
		}
		src, done, err := e.handle(k)
		if done {
			return src, err
		}
		e.render()
	}
}

// reset начинает новый ввод.
func (e *Editor) reset() {
	e.buf = buffer{}
	e.crow = 0
	e.hist = e.historyLen()
	e.draft = ""
	e.search = nil
	e.menu = nil
	e.ghost = ""
	e.render()
}

// handle применяет клавишу. done — ввод закончен: src и err — итог
// ReadInput.
func (e *Editor) handle(k termio.Key) (src string, done bool, err error) {
	if e.search != nil && e.searchKey(k) {
		return "", false, nil
	}
	if k.Code != termio.KeyTab {
		e.menu = nil
	}
	b := &e.buf
	switch k.Code {
	case termio.KeyRune:
		b.insert(string(k.Rune))
	case termio.KeyPaste:
		b.insert(k.Text)
	case termio.KeyEnter:
		return e.enter()
	case termio.KeyBackspace:
		if _, col := b.lineCol(); e.IndentWidth > 0 && b.onlySpacesBefore() {
			b.del(b.pos-(col-1)%e.IndentWidth-1, b.pos)
		} else {
			b.backspace()
		}
	case termio.KeyDelete:
		b.delete()
	case termio.KeyEOF:
		if len(b.r) == 0 {
			e.finish("")
			return "", true, io.EOF
		}
		b.delete()
	case termio.KeyInterrupt:
		e.finish("^C")
		e.reset()
	case termio.KeyLeft:
		b.left()
	case termio.KeyRight:
		if !e.acceptGhost() {
			b.right()
		}
	case termio.KeyHome:
		b.home()
	case termio.KeyEnd:
		if !e.acceptGhost() {
			b.end()
		}
	case termio.KeyTab:
		e.tab()
	case termio.KeyWordLeft:
		b.wordLeft()
	case termio.KeyWordRight:
		b.wordRight()
	case termio.KeyUp:
		if b.firstLine() {
			e.historyMove(-1)
		} else {
			b.up()
		}
	case termio.KeyDown:
		if b.lastLine() {
			e.historyMove(+1)
		} else {
			b.down()
		}
	case termio.KeyKillEnd:
		b.killEnd()
	case termio.KeyKillStart:
		b.killStart()
	case termio.KeyKillWord:
		b.killWord()
	case termio.KeyClear:
		e.write("\x1b[H\x1b[2J")
		e.crow = 0
	case termio.KeySearch:
		e.ghost = ""
		e.menu = nil
		if e.History != nil {
			saved := buffer{r: append([]rune(nil), b.r...), pos: b.pos}
			e.search = &search{match: -1, saved: saved}
		}
	}
	return "", false, nil
}

// enter: в конце незавершённого ввода и внутри многострочного буфера —
// перевод строки с автоотступом, иначе — конец ввода.
func (e *Editor) enter() (string, bool, error) {
	src := e.buf.String()
	more := e.NeedMore != nil && e.NeedMore(src+"\n")
	if more || e.buf.multiline() && !e.buf.atEnd() {
		indent := ""
		if e.Indent != nil {
			indent = e.Indent(string(e.buf.r[:e.buf.pos]))
		}
		e.buf.insert("\n" + indent)
		return "", false, nil
	}
	e.finish("")
	return src + "\n", true, nil
}

// finish перерисовывает ввод без подсказки с курсором в конце, дописывает
// mark и переводит строку: следующий вывод начинается под вводом.
func (e *Editor) finish(mark string) {
	e.search = nil
	e.buf.pos = len(e.buf.r)
	e.noHint = true
	e.render()
	e.noHint = false
	e.write(mark + "\r\n")
	e.crow = 0
}

func (e *Editor) historyLen() int {
	if e.History == nil {
		return 0
	}
	return e.History.Len()
}

// historyMove листает историю на d записей; за последней — черновик,
// который редактировался до начала листания.
func (e *Editor) historyMove(d int) {
	n := e.historyLen()
	i := e.hist + d
	if i < 0 || i > n {
		return
	}
	if e.hist == n {
		e.draft = e.buf.String()
	}
	e.hist = i
	if i == n {
		e.buf.set(e.draft)
	} else {
		e.buf.set(e.History.At(i))
	}
}

// searchKey обрабатывает клавишу в режиме Ctrl-R. false — поиск принят,
// клавиша обрабатывается как обычно.
func (e *Editor) searchKey(k termio.Key) bool {
	s := e.search
	switch k.Code {
	case termio.KeyRune:
		s.query = append(s.query, k.Rune)
		from := s.match
		if from < 0 {
			from = e.historyLen() - 1
		}
		e.find(from)
	case termio.KeyBackspace:
		if len(s.query) > 0 {
			s.query = s.query[:len(s.query)-1]
		}
		e.find(e.historyLen() - 1)
	case termio.KeySearch:
		if s.match > 0 {
			e.find(s.match - 1)
		} else if s.match == 0 {
			s.failed = true
		}
	case termio.KeyCancel:
		e.buf = s.saved
		e.search = nil
	case termio.KeyEnter:
		e.search = nil
		e.buf.pos = len(e.buf.r)
		return false
	default:
		e.search = nil
		return false
	}
	return true
}

// find ищет запрос Ctrl-R от записи from к старым; совпадение — в буфер,
// курсор — на его начало.
func (e *Editor) find(from int) {
	s := e.search
	if len(s.query) == 0 {
		s.failed = false
		return
	}
	i, at := e.History.search(string(s.query), from)
	if i < 0 {
		s.failed = true
		return
	}
	s.failed, s.match, e.hist = false, i, i
	e.buf.set(e.History.At(i))
	e.buf.pos = at
}

func (e *Editor) write(s string) {
	_, _ = io.WriteString(e.out, s)
}

func (e *Editor) width() int {
	if e.Width != nil {
		if w := e.Width(); w > 0 {
			return w
		}
	}
	return 80
}

// render перерисовывает ввод с его первой строки экрана. Строка текста
// шириной n колонок (с приглашением) занимает n/w+1 строк экрана: если
// она кончается ровно на краю, курсор переносится на новую строку
// (` \b`), иначе терминал оставил бы его в «отложенном переносе».
func (e *Editor) render() {
	var b strings.Builder
	b.WriteString("\r")
	if e.crow > 0 {
		fmt.Fprintf(&b, "\x1b[%dA", e.crow)
	}
	b.WriteString("\x1b[J")

	w := e.width()
	src := e.buf.String()
	lines := strings.Split(src, "\n")
	shown := lines
	if e.Highlight != nil {
		if h := strings.Split(e.Highlight(src, e.buf.pos), "\n"); len(h) == len(lines) {
			shown = h
		}
	}
	cl, cc := e.buf.lineCol()
	hint := ""
	e.ghost = ""
	if e.Hint != nil && !e.noHint && e.search == nil && e.buf.pos == e.buf.lineEnd() {
		e.ghost = e.Hint(src, e.buf.pos)
		if e.Color {
			hint, _, _ = strings.Cut(e.ghost, "\n")
		}
	}

	rows, crow, ccol := 0, 0, 0
	for i, line := range lines {
		p := e.cont
		if i == 0 {
			p = e.firstPrompt()
		}
		b.WriteString(p)
		b.WriteString(termio.Visible(shown[i]))
		n := termio.Cells(p) + termio.Cells(line)
		if i == cl {
			x := termio.Cells(p) + termio.Cells(string([]rune(line)[:cc]))
			crow, ccol = rows+x/w, x%w
			if hint != "" {
				b.WriteString("\x1b[90m" + termio.Visible(hint) + "\x1b[0m")
				n += termio.Cells(hint)
			}
		}
		if n > 0 && n%w == 0 {
			b.WriteString(" \b\x1b[K")
		}
		rows += n/w + 1
		if i < len(lines)-1 {
			b.WriteString("\r\n")
		}
	}
	footRows := 0
	if foot := e.footer(src, w); len(foot) > 0 {
		b.WriteString("\r\n")
		for i, line := range foot {
			b.WriteString(line)
			n := termio.Cells(line)
			if n > 0 && n%w == 0 {
				b.WriteString(" \b\x1b[K")
			}
			footRows += n/w + 1
			if i < len(foot)-1 {
				b.WriteString("\r\n")
			}
		}
	}
	b.WriteString("\r")
	if up := rows - 1 - crow + footRows; up > 0 {
		fmt.Fprintf(&b, "\x1b[%dA", up)
	}
	if ccol > 0 {
		fmt.Fprintf(&b, "\x1b[%dC", ccol)
	}
	e.crow = crow
	e.write(b.String())
}

func (e *Editor) firstPrompt() string {
	s := e.search
	switch {
	case s == nil:
		return e.prompt
	case s.failed:
		return fmt.Sprintf("(failed i-search)`%s': ", string(s.query))
	}
	return fmt.Sprintf("(i-search)`%s': ", string(s.query))
}
