package term

import (
	"fmt"
	"strings"

	"github.com/it1ro/brig-lang/internal/termio"
)

// Candidate — что вставить и что показать в меню. Редактор языка не знает.
type Candidate struct {
	Insert  string
	Display string
}

// Completion — кандидаты одного span. From и To — индексы рун, [From, To)
// заменяется целиком.
type Completion struct {
	From, To   int
	Candidates []Candidate
}

const menuMaxRows = 8

// menuState — меню дополнения. sel — выбранный повторным Tab кандидат
// (-1 — не выбран); выбранный текст стоит в буфере на [from, to).
type menuState struct {
	cands    []Candidate
	sel      int
	from, to int
}

func (m menuState) active() bool { return len(m.cands) > 0 }

func (m menuState) items() []string {
	out := make([]string, len(m.cands))
	for i, c := range m.cands {
		out[i] = c.Display
		if out[i] == "" {
			out[i] = c.Insert
		}
	}
	return out
}

// tab — Tab: в отступе — уровень отступа; при открытом меню —
// следующий кандидат; иначе дополнение. Нет кандидатов — звонок.
func (e *Editor) tab() {
	if e.menu.active() {
		e.menuStep(+1)
		return
	}
	if e.indent() {
		return
	}
	if e.Complete == nil {
		return
	}
	c := e.Complete(e.buf.String(), e.buf.pos)
	if len(c.Candidates) == 0 || c.From < 0 || c.To > len(e.buf.r) || c.From > c.To {
		e.write("\a")
		return
	}
	span := string(e.buf.r[c.From:c.To])
	if len(c.Candidates) == 1 {
		if c.Candidates[0].Insert != span {
			e.replaceSpan(c.From, c.To, c.Candidates[0].Insert)
		}
		return
	}
	common := commonPrefix(inserts(c.Candidates))
	if strings.HasPrefix(common, span) && common != span {
		e.replaceSpan(c.From, c.To, common)
		return
	}
	e.menu = menuState{cands: c.Candidates, sel: -1, from: c.From, to: c.To}
}

// menuStep выбирает соседнего кандидата меню (Tab — вперёд, Shift-Tab —
// назад) и ставит его в буфер.
func (e *Editor) menuStep(d int) {
	m := &e.menu
	n := len(m.cands)
	switch {
	case m.sel < 0 && d > 0:
		m.sel = 0
	case m.sel < 0:
		m.sel = n - 1
	default:
		m.sel = (m.sel + d + n) % n
	}
	e.replaceSpan(m.from, m.to, m.cands[m.sel].Insert)
	m.to = e.buf.pos
}

func (e *Editor) replaceSpan(from, to int, text string) {
	e.buf.del(from, to)
	e.buf.insert(text)
}

// acceptGhost вставляет подсказку истории по одной строке: → и End в
// конце строки берут хвост до перевода строки, следующий — следующую
// строку записи.
func (e *Editor) acceptGhost() bool {
	if e.search != nil || e.ghost == "" || e.buf.pos != e.buf.lineEnd() {
		return false
	}
	g := e.ghost
	if rest, ok := strings.CutPrefix(g, "\n"); ok {
		line, _, _ := strings.Cut(rest, "\n")
		g = "\n" + line
	} else {
		g, _, _ = strings.Cut(g, "\n")
	}
	e.buf.insert(g)
	e.ghost = ""
	return true
}

func inserts(cs []Candidate) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Insert
	}
	return out
}

func commonPrefix(ss []string) string {
	if len(ss) == 0 {
		return ""
	}
	p := []rune(ss[0])
	for _, s := range ss[1:] {
		r := []rune(s)
		n := 0
		for n < len(p) && n < len(r) && p[n] == r[n] {
			n++
		}
		p = p[:n]
	}
	return string(p)
}

func (e *Editor) footer(src string, w int) []string {
	if e.noHint || e.search != nil {
		return nil
	}
	var lines []string
	if e.Signature != nil {
		text, a0, a1 := e.Signature(src, e.buf.pos)
		if text != "" {
			lines = append(lines, fitArg(text, a0, a1, w, e.Color))
		}
	}
	if e.menu.active() {
		lines = append(lines, menuLines(e.menu.items(), e.menu.sel, w, e.Color)...)
	}
	return lines
}

// fitArg обрезает строку по колонкам. Метку, которую обрезание съело,
// не рисует: с цветом это подчёркивание, без цвета — скобки.
func fitArg(text string, a0, a1, width int, color bool) string {
	if width < 1 {
		width = 80
	}
	rs := []rune(text)
	mark := a0 >= 0 && a1 > a0 && a1 <= len(rs)
	if mark {
		var painted string
		if color {
			painted = string(rs[:a0]) + "\x1b[4m" + string(rs[a0:a1]) + "\x1b[24m" + string(rs[a1:])
		} else {
			painted = string(rs[:a0]) + "[" + string(rs[a0:a1]) + "]" + string(rs[a1:])
		}
		if termio.Cells(painted) <= width {
			return painted
		}
	}
	return truncateCells(text, width)
}

// menuLines раскладывает меню по колонкам; выбранный пункт sel — в
// инверсии (без цвета — в квадратных скобках).
func menuLines(items []string, sel, width int, color bool) []string {
	if len(items) == 0 {
		return nil
	}
	if width < 1 {
		width = 80
	}
	const gap = 2
	widest := 1
	for _, it := range items {
		if n := termio.Cells(it); n > widest {
			widest = n
		}
	}
	colW := widest + gap
	cols := width / colW
	if cols < 1 {
		cols = 1
		colW = width
	}
	limit := cols * menuMaxRows
	extra := 0
	show := items
	if len(show) > limit {
		extra = len(show) - limit
		show = show[:limit]
	}
	rows := (len(show) + cols - 1) / cols
	lines := make([]string, rows)
	for i, it := range show {
		room := colW
		if cols > 1 {
			room = colW - gap
		}
		cell := it
		if termio.Cells(cell) > room {
			cell = truncateCells(cell, room)
		}
		if cols > 1 && i%cols != cols-1 {
			cell = padCells(cell, colW)
		}
		if i == sel {
			cell = markCell(cell, color)
		}
		lines[i/cols] += cell
	}
	if extra > 0 {
		lines = append(lines, fmt.Sprintf("… and %d more", extra))
	}
	return lines
}

// markCell выделяет выбранный пункт меню, не меняя его ширины: без цвета
// первый и последний пробел ячейки становятся скобками, если они есть.
func markCell(cell string, color bool) string {
	text := strings.TrimRight(cell, " ")
	pad := cell[len(text):]
	if color {
		return "\x1b[7m" + text + "\x1b[27m" + pad
	}
	if pad == "" {
		return text
	}
	return ">" + text + pad[1:]
}

func padCells(s string, width int) string {
	for termio.Cells(s) < width {
		s += " "
	}
	return s
}

func truncateCells(s string, width int) string {
	if width <= 0 || s == "" {
		return ""
	}
	if termio.Cells(s) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	var b strings.Builder
	n := 0
	for _, r := range s {
		w := termio.Cells(string(r))
		if n+w > width-1 {
			break
		}
		b.WriteRune(r)
		n += w
	}
	b.WriteString("…")
	return b.String()
}
