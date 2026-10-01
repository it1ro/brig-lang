// Package view рисует один кадр observe. Кадр детерминирован: ровно h
// строк по w колонок, без чтения часов и терминала.
package view

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/it1ro/brig-lang/internal/actorview"
	"github.com/it1ro/brig-lang/internal/observe"
	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/termio"
	"github.com/it1ro/brig-lang/internal/vm"
)

// Крупный ящик — жирный. Порог ниже HWM (64), чтобы рост был виден раньше отказа send.
const largeMailbox = 32

const (
	sgrDim  = "2"
	sgrBold = "1"
	sgrRed  = "31"
)

// Render — кадр. color false — без SGR (NO_COLOR и не-TTY). Каждая строка
// добита пробелами до w и заканчивается ESC[K.
func Render(m observe.Model, w, h int, color bool) string {
	if w < 1 || h < 1 {
		return ""
	}
	lines := make([]string, h)
	blank := termio.Pad("", w)
	for i := range lines {
		lines[i] = blank
	}
	if w < 40 || h < 10 {
		lines[0] = termio.Pad("терминал слишком мал", w)
		return join(lines)
	}
	lines[0] = termio.Pad(header(m, w), w)
	lines[1] = strings.Repeat("─", w)
	lines[h-2] = strings.Repeat("─", w)
	lines[h-1] = termio.Pad(" ↑↓ выбор   Enter детали   s сортировка   q выход", w)

	showRed := w >= 60
	right := m.Detail && w >= 100
	listW := w
	detailW := 0
	gap := 0
	if right {
		detailW = w * 2 / 5
		if detailW < 36 {
			detailW = 36
		}
		gap = 2
		listW = w - detailW - gap
	}
	bodyH := h - 4 // строки [2, h-2)
	if bodyH < 1 {
		return join(lines)
	}
	bottomMax := h / 3
	if bottomMax < 1 {
		bottomMax = 1
	}
	if bottomMax >= bodyH {
		bottomMax = bodyH - 1
	}
	var bottom []string
	if m.Detail && !right {
		bottom = clip(detailBlock(m, w, color), bottomMax)
	} else {
		bottom = clip(crashBlock(m, w, color), bottomMax)
	}
	listH := bodyH - len(bottom)
	if listH < 1 {
		over := 1 - listH
		if over > len(bottom) {
			over = len(bottom)
		}
		bottom = bottom[:len(bottom)-over]
		listH = bodyH - len(bottom)
	}

	rows := actorview.Rows(m.Actors, m.Links, m.Sort)
	list := make([]string, listH)
	list[0] = fitWidth(colHead(listW, showRed), listW)
	for i := 1; i < listH; i++ {
		ri := i - 1
		if ri < len(rows) {
			list[i] = fitWidth(actorLine(rows[ri], m.HasPid && rows[ri].Actor.Pid == m.Pid, listW, showRed, color), listW)
		} else {
			list[i] = termio.Pad("", listW)
		}
	}
	var side []string
	if right {
		side = detailBlock(m, detailW, color)
	}
	gutter := strings.Repeat(" ", gap)
	for i := 0; i < listH; i++ {
		line := list[i]
		if right {
			extra := ""
			if i < len(side) {
				extra = side[i]
			}
			line += gutter + fitWidth(extra, detailW)
		}
		lines[2+i] = fitWidth(line, w)
	}
	for i, line := range bottom {
		lines[2+listH+i] = fitWidth(line, w)
	}
	return join(lines)
}

func header(m observe.Model, w int) string {
	left := " brig observe"
	right := summary(m)
	if m.Err != "" {
		right = "снимок недоступен: " + m.Err
	} else if m.Note != "" {
		right = m.Note
	}
	if termio.Cells(left)+1+termio.Cells(right) > w {
		return termio.Fit(strings.TrimSpace(left+"  "+right), w)
	}
	gap := w - termio.Cells(left) - termio.Cells(right)
	return left + strings.Repeat(" ", gap) + right
}

func summary(m observe.Model) string {
	n := len(m.Actors)
	return fmt.Sprintf("%d %s · sort: %s · 1s", n, actorsNoun(n), actorview.Label(m.Sort))
}

func actorsNoun(n int) string {
	n100 := n % 100
	if n100 >= 11 && n100 <= 14 {
		return "акторов"
	}
	switch n % 10 {
	case 1:
		return "актор"
	case 2, 3, 4:
		return "актора"
	default:
		return "акторов"
	}
}

func colHead(listW int, showRed bool) string {
	nameCol := nameWidth(listW, showRed)
	s := "  " + termio.Pad("PID", pidWidth) + " " + termio.Pad("NAME", nameCol) + " " + termio.Pad("STATUS", 8) + " " + termio.PadLeft("MBOX", 6)
	if showRed {
		s += " " + termio.PadLeft("REDUCTIONS", 10)
	}
	return s
}

const pidWidth = 4

func actorLine(row actorview.Row, selected bool, listW int, showRed, color bool) string {
	mark := " "
	if selected {
		mark = "▸"
	}
	prefix := treePrefix(row)
	nameCol := nameWidth(listW, showRed)
	nw := nameCol - termio.Cells(prefix)
	if nw < 1 {
		nw = 1
	}
	pid := termio.PadLeft(strconv.Itoa(row.Actor.Pid), pidWidth)
	name := termio.Pad(displayName(row.Actor), nw)
	status := paint(termio.Pad(row.Actor.Status, 8), statusSGR(row.Actor.Status), color)
	box := paint(termio.PadLeft(formatInt(int64(row.Actor.Mailbox)), 6), boxSGR(row.Actor.Mailbox), color)
	s := mark + " " + prefix + pid + " " + name + " " + status + " " + box
	if showRed {
		s += " " + termio.PadLeft(formatInt(row.Actor.Reductions), 10)
	}
	return s
}

func nameWidth(listW int, showRed bool) int {
	// "  " + pid + " " + name + " " + status8 + " " + mbox6 [+ " " + reds10]
	fixed := 2 + pidWidth + 1 + 1 + 8 + 1 + 6
	if showRed {
		fixed += 1 + 10
	}
	n := listW - fixed
	if n < 1 {
		return 1
	}
	return n
}

func treePrefix(row actorview.Row) string {
	if row.Depth == 0 {
		return ""
	}
	var b strings.Builder
	for i := 0; i < row.Depth-1; i++ {
		if i < len(row.Cont) && row.Cont[i] {
			b.WriteString("│ ")
		} else {
			b.WriteString("  ")
		}
	}
	if row.Last {
		b.WriteString("└ ")
	} else {
		b.WriteString("├ ")
	}
	return b.String()
}

func crashBlock(m observe.Model, w int, color bool) []string {
	lines := []string{termio.Pad(" crashes", w)}
	for _, c := range m.Crashes {
		name := c.Name
		if name == "" {
			name = "—"
		}
		text := fmt.Sprintf("  %s  #%d %s  %s", c.At.Format("15:04:05"), c.Pid, name, c.Reason)
		text = termio.Fit(text, w)
		if color {
			text = paint(text, sgrRed, true)
		}
		lines = append(lines, text)
	}
	return lines
}

func detailBlock(m observe.Model, w int, color bool) []string {
	title := termio.Pad(" детали", w)
	a, ok := selected(m)
	if !ok {
		return []string{title, termio.Pad(" актор не жив", w)}
	}
	fields := [][2]string{
		{"pid", strconv.Itoa(a.Pid)},
		{"name", displayName(a)},
		{"status", a.Status},
		{"mailbox", formatInt(int64(a.Mailbox))},
		{"dropped", formatInt(a.Dropped)},
		{"reductions", formatInt(a.Reductions)},
		{"alloc", formatInt(a.AllocBytes)},
		{"turn_reductions", formatInt(a.TurnReductions)},
		{"turn_alloc", formatInt(a.TurnAllocBytes)},
		{"watchers", pidList(a.Watchers)},
		{"watching", pidList(a.Watching)},
		{"initial_fn", a.InitialFn},
	}
	lines := []string{title}
	for _, f := range fields {
		val := f[1]
		if f[0] == "status" {
			val = paint(val, statusSGR(a.Status), color)
		}
		if f[0] == "mailbox" {
			val = paint(val, boxSGR(a.Mailbox), color)
		}
		lines = append(lines, termio.Fit(" "+f[0]+"  "+val, w))
	}
	return lines
}

func selected(m observe.Model) (vm.ActorSnapshot, bool) {
	if !m.HasPid {
		return vm.ActorSnapshot{}, false
	}
	for _, a := range m.Actors {
		if a.Pid == m.Pid {
			return a, true
		}
	}
	return vm.ActorSnapshot{}, false
}

func pidList(pids []int) string {
	if len(pids) == 0 {
		return "—"
	}
	n := len(pids)
	if n > 8 {
		n = 8
	}
	parts := make([]string, n)
	for i := 0; i < n; i++ {
		parts[i] = strconv.Itoa(pids[i])
	}
	s := strings.Join(parts, " ")
	if extra := len(pids) - 8; extra > 0 {
		s += fmt.Sprintf(" … +%d", extra)
	}
	return s
}

func displayName(a vm.ActorSnapshot) string {
	if !a.HasName {
		return "—"
	}
	switch a.Name.Kind {
	case runtime.KindAtom:
		return a.Name.Atom
	case runtime.KindStr:
		return a.Name.Str
	default:
		return a.Name.Inspect()
	}
}

func statusSGR(status string) string {
	switch status {
	case "recv", "waiting":
		return sgrDim
	default:
		return ""
	}
}

func boxSGR(n int) string {
	if n >= largeMailbox {
		return sgrBold
	}
	return ""
}

func paint(s, code string, on bool) string {
	if !on || code == "" || s == "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func formatInt(n int64) string {
	sign := ""
	if n < 0 {
		sign = "-"
		n = -n
	}
	s := strconv.FormatInt(n, 10)
	var b strings.Builder
	b.WriteString(sign)
	for i := 0; i < len(s); i++ {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(' ')
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func clip(lines []string, n int) []string {
	if n < 0 {
		n = 0
	}
	if len(lines) > n {
		return lines[:n]
	}
	return lines
}

func fitWidth(s string, w int) string {
	if termio.Cells(s) == w {
		return s
	}
	if termio.Cells(s) > w {
		return termio.Fit(s, w)
	}
	return s + strings.Repeat(" ", w-termio.Cells(s))
}

func join(lines []string) string {
	var b strings.Builder
	for i, line := range lines {
		b.WriteString(line)
		b.WriteString("\x1b[K")
		if i+1 < len(lines) {
			b.WriteByte('\n')
		}
	}
	return b.String()
}
