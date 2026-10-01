package highlight

import (
	"strings"
	"unicode/utf8"
)

// brackets помечает лишние закрывающие скобки и скобки чужого вида как
// error и выделяет пару скобки под курсором. Незакрытая открывающая —
// неполный ввод, не ошибка: консоль ждёт продолжения.
func brackets(src string, toks []token, ann []ann, cursor int) {
	type br struct{ kind, idx int }
	var st []br
	match := map[int]int{}
	bad := map[int]bool{}
	for i, t := range toks {
		kind, open, ok := bracketKind(t.text(src))
		if !ok {
			continue
		}
		if open {
			st = append(st, br{kind, i})
			continue
		}
		if n := len(st); n > 0 && st[n-1].kind == kind {
			j := st[n-1].idx
			st = st[:n-1]
			match[i] = j
			match[j] = i
		} else {
			bad[i] = true
		}
	}
	for i := range bad {
		ann[i].class = Error
		ann[i].set = true
		ann[i].match = false
	}
	at := bracketAt(src, toks, cursor)
	if j, ok := match[at]; ok {
		ann[at].match = true
		ann[j].match = true
	}
}

func bracketKind(lit string) (kind int, open, ok bool) {
	switch lit {
	case "(", "\\(":
		return 1, true, true
	case ")":
		return 1, false, true
	case "[", "%[":
		return 2, true, true
	case "]":
		return 2, false, true
	case "{", "%{":
		return 3, true, true
	case "}":
		return 3, false, true
	default:
		return 0, false, false
	}
}

func bracketAt(src string, toks []token, cursor int) int {
	if cursor < 0 {
		return -1
	}
	b := runeToByte(src, cursor)
	contain, edge := -1, -1
	for i, t := range toks {
		if _, _, ok := bracketKind(t.text(src)); !ok {
			continue
		}
		if t.start <= b && b < t.end {
			contain = i
		}
		if t.end == b {
			edge = i
		}
	}
	if contain >= 0 {
		return contain
	}
	return edge
}

func runeToByte(s string, n int) int {
	i := 0
	for n > 0 && i < len(s) {
		_, sz := utf8.DecodeRuneInString(s[i:])
		if sz < 1 {
			sz = 1
		}
		i += sz
		n--
	}
	return i
}

func indents(src string, toks []token) (spans []Span, guides []int) {
	lines := splitLines(src)
	depth := lineDepths(src, lines, toks)
	stack := []int{0}
	stmt := 0
	for i, ln := range lines {
		ind, tab := leading(ln.text)
		if tab {
			end := ln.start + ind + 1
			if end > ln.end {
				end = ln.end
			}
			spans = append(spans, Span{Start: ln.start, End: end, Class: Error})
			continue
		}
		if depth[i] > 0 || blankOrComment(ln.text) {
			// Пустая последняя строка закрывает блок: направляющая на ней
			// осталась бы в scrollback после Enter.
			if i == len(lines)-1 && i > 0 && strings.TrimSpace(ln.text) == "" {
				continue
			}
			guides = append(guides, guideAt(src, stack, ln, ind)...)
			continue
		}
		if continues(src, toks, ln) {
			if ind <= stmt {
				spans = append(spans, badIndent(toks, ln, ind)...)
			} else {
				guides = append(guides, guideAt(src, stack, ln, ind)...)
			}
			continue
		}
		bad := false
		top := stack[len(stack)-1]
		switch {
		case ind > top:
			stack = append(stack, ind)
		case ind < top:
			on := false
			for _, s := range stack {
				if s == ind {
					on = true
					break
				}
			}
			if !on {
				bad = true
				break
			}
			for len(stack) > 1 && stack[len(stack)-1] > ind {
				stack = stack[:len(stack)-1]
			}
		}
		if bad {
			spans = append(spans, badIndent(toks, ln, ind)...)
			continue
		}
		stmt = ind
		guides = append(guides, guideAt(src, stack, ln, ind)...)
	}
	return spans, guides
}

func badIndent(toks []token, ln line, ind int) []Span {
	if ind > 0 {
		end := ln.start + ind
		if end > ln.end {
			end = ln.end
		}
		return []Span{{Start: ln.start, End: end, Class: Error}}
	}
	if t := firstTok(toks, ln); t >= 0 {
		return []Span{{Start: toks[t].start, End: toks[t].end, Class: Error}}
	}
	return nil
}

type line struct {
	start, end int
	text       string
}

func splitLines(src string) []line {
	var ls []line
	start := 0
	for i := 0; i < len(src); i++ {
		if src[i] == '\n' {
			ls = append(ls, line{start, i, src[start:i]})
			start = i + 1
		}
	}
	return append(ls, line{start, len(src), src[start:]})
}

func lineDepths(src string, lines []line, toks []token) []int {
	d := make([]int, len(lines))
	depth, ti := 0, 0
	for i, ln := range lines {
		if depth < 0 {
			depth = 0
		}
		d[i] = depth
		for ti < len(toks) && toks[ti].start < ln.end {
			depth += toks[ti].delta(src)
			ti++
		}
	}
	return d
}

func leading(text string) (n int, tab bool) {
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case ' ':
			n++
		case '\t':
			return i, true
		default:
			return n, false
		}
	}
	return n, false
}

func blankOrComment(text string) bool {
	i := 0
	for i < len(text) && text[i] == ' ' {
		i++
	}
	return i >= len(text) || text[i] == '#'
}

func continues(src string, toks []token, ln line) bool {
	t := firstTok(toks, ln)
	if t < 0 {
		return false
	}
	return isCont(toks[t].text(src))
}

func firstTok(toks []token, ln line) int {
	for i, t := range toks {
		if t.start < ln.start {
			continue
		}
		if t.start >= ln.end {
			return -1
		}
		if t.kind == tComment || t.kind == tDoc {
			return -1
		}
		return i
	}
	return -1
}

func isCont(lit string) bool {
	switch lit {
	case "|>", "+", "-", "*", "/", "**", "==", "!=", "<", ">", "<=", ">=", "..",
		"and", "or", "div", "rem", "to":
		return true
	default:
		return false
	}
}

func guideAt(src string, stack []int, ln line, ind int) []int {
	open := false
	for _, s := range stack {
		if s > 0 {
			open = true
			break
		}
	}
	if !open {
		return nil
	}
	var gs []int
	for _, s := range stack {
		if s < ind {
			off := ln.start + s
			if off < ln.end && off < len(src) && src[off] == ' ' {
				gs = append(gs, off)
			}
		}
	}
	return gs
}
