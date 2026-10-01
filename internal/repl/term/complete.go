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

func (e *Editor) tab() {
	if e.Complete == nil {
		e.menu = nil
		return
	}
	c := e.Complete(e.buf.String(), e.buf.pos)
	if len(c.Candidates) == 0 || c.From < 0 || c.To > len(e.buf.r) || c.From > c.To {
		e.menu = nil
		return
	}
	span := string(e.buf.r[c.From:c.To])
	if len(c.Candidates) == 1 {
		e.menu = nil
		if c.Candidates[0].Insert != span {
			e.replaceSpan(c.From, c.To, c.Candidates[0].Insert)
		}
		return
	}
	common := commonPrefix(inserts(c.Candidates))
	if strings.HasPrefix(common, span) && common != span {
		e.menu = nil
		e.replaceSpan(c.From, c.To, common)
		return
	}
	e.menu = make([]string, len(c.Candidates))
	for i, cand := range c.Candidates {
		if cand.Display != "" {
			e.menu[i] = cand.Display
		} else {
			e.menu[i] = cand.Insert
		}
	}
}

func (e *Editor) replaceSpan(from, to int, text string) {
	e.buf.del(from, to)
	e.buf.insert(text)
}

func (e *Editor) acceptGhost() bool {
	if e.search != nil || e.ghost == "" || e.buf.pos != e.buf.lineEnd() {
		return false
	}
	e.buf.insert(e.ghost)
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
	if len(e.menu) > 0 {
		lines = append(lines, menuLines(e.menu, w)...)
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

func menuLines(items []string, width int) []string {
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
		lines[i/cols] += cell
	}
	if extra > 0 {
		lines = append(lines, fmt.Sprintf("… and %d more", extra))
	}
	return lines
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
