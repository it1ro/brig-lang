package term

import (
	"strconv"
	"strings"
)

// screen — минимальный эмулятор терминала для тестов перерисовки:
// печатные символы с отложенным переносом на правом краю, \r, \n, \b,
// CSI A/B/C/D (курсор), J/K (очистка), H, 2J; SGR и режимы игнорируются.
type screen struct {
	w          int
	lines      [][]rune
	wrap       []bool // строка продолжается на следующей (перенос на краю)
	row, col   int
	pendingEOL bool
}

func newScreen(w int) *screen { return &screen{w: w, lines: [][]rune{nil}, wrap: []bool{false}} }

// resize меняет ширину, как терминал с переносом (reflow): логические
// строки раскладываются заново, курсор остаётся на том же символе.
func (s *screen) resize(w int) {
	type logical struct {
		text []rune
		cur  int // смещение курсора в строке; -1 — курсор не здесь
	}
	var ls []logical
	cur := logical{cur: -1}
	for i, line := range s.lines {
		if i == s.row {
			cur.cur = len(cur.text) + s.col
		}
		text := line
		if s.wrap[i] {
			for len(text) < s.w {
				text = append(text, ' ')
			}
		}
		cur.text = append(cur.text, text...)
		if !s.wrap[i] {
			ls = append(ls, cur)
			cur = logical{cur: -1}
		}
	}
	s.w = w
	s.lines, s.wrap = nil, nil
	s.pendingEOL = false
	for _, l := range ls {
		first := len(s.lines)
		text := l.text
		for {
			n := min(len(text), w)
			s.lines = append(s.lines, append([]rune(nil), text[:n]...))
			s.wrap = append(s.wrap, len(text) > w)
			if len(text) <= w {
				break
			}
			text = text[w:]
		}
		if l.cur >= 0 {
			s.row = min(first+l.cur/w, len(s.lines)-1)
			s.col = l.cur - (s.row-first)*w
		}
	}
}

func (s *screen) Write(p []byte) (int, error) {
	rs := []rune(string(p))
	for i := 0; i < len(rs); i++ {
		switch c := rs[i]; {
		case c == 0x1b && i+1 < len(rs) && rs[i+1] == '[':
			j := i + 2
			for j < len(rs) && (rs[j] < 0x40 || rs[j] > 0x7e) {
				j++
			}
			s.csi(string(rs[i+2:j]), rs[j])
			i = j
		case c == '\r':
			s.col, s.pendingEOL = 0, false
		case c == '\n':
			s.down(1)
		case c == '\b':
			if s.col > 0 {
				s.col--
			}
			s.pendingEOL = false
		default:
			if s.pendingEOL {
				s.wrap[s.row] = true
				s.down(1)
				s.col = 0
			}
			line := s.lines[s.row]
			for len(line) <= s.col {
				line = append(line, ' ')
			}
			line[s.col] = c
			s.lines[s.row] = line
			if s.col == s.w-1 {
				s.pendingEOL = true
			} else {
				s.col++
			}
		}
	}
	return len(p), nil
}

func (s *screen) down(n int) {
	s.row += n
	for len(s.lines) <= s.row {
		s.lines = append(s.lines, nil)
		s.wrap = append(s.wrap, false)
	}
	s.pendingEOL = false
}

func (s *screen) csi(params string, final rune) {
	n, err := strconv.Atoi(params)
	if err != nil || n == 0 {
		n = 1
	}
	s.pendingEOL = false
	switch final {
	case 'A':
		s.row = max(0, s.row-n)
	case 'B':
		s.down(n)
	case 'C':
		s.col = min(s.w-1, s.col+n)
	case 'D':
		s.col = max(0, s.col-n)
	case 'K':
		s.truncate()
	case 'J':
		if params == "2" {
			s.lines = [][]rune{nil}
			s.wrap = []bool{false}
			s.row, s.col = 0, 0
			return
		}
		s.truncate()
		s.lines = s.lines[:s.row+1]
		s.wrap = s.wrap[:s.row+1]
	case 'H':
		s.row, s.col = 0, 0
	}
}

func (s *screen) truncate() {
	if line := s.lines[s.row]; len(line) > s.col {
		s.lines[s.row] = line[:s.col]
	}
	s.wrap[s.row] = false
}

// String — строки экрана без хвостовых пробелов; курсор помечен `█`.
func (s *screen) String() string {
	out := make([]string, len(s.lines))
	for i, line := range s.lines {
		l := append([]rune(nil), line...)
		if i == s.row {
			for len(l) <= s.col {
				l = append(l, ' ')
			}
			l = append(l[:s.col], append([]rune{'█'}, l[s.col:]...)...)
		}
		out[i] = strings.TrimRight(string(l), " ")
	}
	return strings.Join(out, "\n")
}
