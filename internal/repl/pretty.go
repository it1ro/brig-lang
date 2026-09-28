package repl

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/it1ro/brig-lang/internal/highlight"
	"github.com/it1ro/brig-lang/internal/runtime"
)

// Limits — потолки pretty-printer. Ноль в поле значит значение по
// умолчанию: 50 элементов коллекции, глубина 8, 2 000 кодпоинтов
// строки. Отрицательное поле снимает этот потолок.
type Limits struct {
	Elems int
	Depth int
	Runes int
}

// NoLimits снимает потолки: вывод можно разобрать обратно в значение.
func NoLimits() Limits { return Limits{Elems: -1, Depth: -1, Runes: -1} }

func (l Limits) elems() int { return normLimit(l.Elems, 50) }
func (l Limits) depth() int { return normLimit(l.Depth, 8) }
func (l Limits) runes() int { return normLimit(l.Runes, 2000) }

func normLimit(n, def int) int {
	if n == 0 {
		return def
	}
	return n
}

// Print — параметры печати значения.
// Width <= 0 не ограничивает строку: значение, которое помещается,
// печатается как Inspect. Pal с выключенным цветом (NO_COLOR, не-TTY)
// не вставляет escape-коды.
type Print struct {
	Width  int
	Limits Limits
	Pal    highlight.Palette
	Env    highlight.Env
}

// Format печатает значение. Пустой Print — лимиты по умолчанию,
// без ограничения ширины и без цвета.
func Format(v runtime.Value, opt Print) string {
	text, _ := FormatAnswer("", v, opt)
	return text
}

// FormatAnswer печатает итог ввода: связывание — `name = <значение>`,
// выражение — `<значение>`. `()` не печатается (ok == false).
func FormatAnswer(name string, v runtime.Value, opt Print) (string, bool) {
	if v.Kind == runtime.KindUnit {
		return "", false
	}
	prefix := ""
	indent := 0
	if name != "" {
		prefix = name + " = "
		indent = displayWidth(prefix)
	}
	body := render(v, false, 1, indent, opt.Width, opt.Limits)
	if prefix != "" {
		body = joinPrefix(prefix, body)
	}
	return colorize(body, opt), true
}

func render(v runtime.Value, nested bool, depth, indent, width int, lim Limits) string {
	if lim.depth() >= 0 && depth > lim.depth() {
		return "…"
	}
	flat := flatForm(v, nested, depth, lim)
	if !breakable(v) || width <= 0 || indent+displayWidth(flat) <= width {
		return flat
	}
	return brokenForm(v, depth, indent, width, lim)
}

func breakable(v runtime.Value) bool {
	switch v.Kind {
	case runtime.KindList, runtime.KindVector, runtime.KindMap, runtime.KindSet, runtime.KindTuple, runtime.KindRecord:
		return true
	case runtime.KindVariant:
		return v.Variant != nil && len(v.Variant.Args) > 0
	default:
		return false
	}
}

func flatForm(v runtime.Value, nested bool, depth int, lim Limits) string {
	if lim.depth() >= 0 && depth > lim.depth() {
		return "…"
	}
	if !breakable(v) {
		return flatLeaf(v, nested, lim)
	}
	open, end, items, spaced := shape(v, lim)
	parts := make([]string, len(items))
	for i, it := range items {
		parts[i] = flatItem(it, depth, lim)
	}
	if len(parts) == 0 {
		return open + end
	}
	inner := strings.Join(parts, ", ")
	if spaced {
		return open + " " + inner + " " + end
	}
	return open + inner + end
}

func flatLeaf(v runtime.Value, nested bool, lim Limits) string {
	if v.Kind == runtime.KindStr {
		return flatStr(v.Str, nested, lim)
	}
	return v.Inspect()
}

func flatStr(s string, nested bool, lim Limits) string {
	rs := []rune(s)
	limit := lim.runes()
	if limit < 0 || len(rs) <= limit {
		if nested {
			return strconv.Quote(s)
		}
		return s
	}
	return strconv.Quote(string(rs[:limit])) + fmt.Sprintf("… %d more", len(rs)-limit)
}

func brokenForm(v runtime.Value, depth, indent, width int, lim Limits) string {
	open, end, items, _ := shape(v, lim)
	if len(items) == 0 {
		return open + end
	}
	var b strings.Builder
	b.WriteString(open)
	b.WriteByte('\n')
	forceComma := v.Kind == runtime.KindTuple && len(items) == 1
	for i, it := range items {
		b.WriteString(indentLines(brokenItem(it, depth, indent, width, lim), 2))
		if i+1 < len(items) || forceComma {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	b.WriteString(end)
	return b.String()
}

type item struct {
	val   runtime.Value
	key   runtime.Value
	field string
	pair  bool
	more  int
}

func shape(v runtime.Value, lim Limits) (open, end string, items []item, spaced bool) {
	switch v.Kind {
	case runtime.KindList:
		return "[", "]", valueItems(v.List, lim), false
	case runtime.KindVector:
		return "%[", "]", valueItems(v.Vector, lim), false
	case runtime.KindSet:
		return "set(", ")", valueItems(v.Set, lim), false
	case runtime.KindTuple:
		return "(", ")", valueItems(v.Tuple, lim), false
	case runtime.KindMap:
		entries := sortedMap(v.Map)
		shown, hidden := clip(len(entries), lim.elems())
		items = make([]item, 0, shown+1)
		for i := 0; i < shown; i++ {
			items = append(items, item{pair: true, key: entries[i].Key, val: entries[i].Val})
		}
		if hidden > 0 {
			items = append(items, item{more: hidden})
		}
		return "%{", "}", items, false
	case runtime.KindRecord:
		if v.Record == nil {
			return "{", "}", nil, true
		}
		shown, hidden := clip(len(v.Record.Fields), lim.elems())
		items = make([]item, 0, shown+1)
		for i := 0; i < shown; i++ {
			f := v.Record.Fields[i]
			items = append(items, item{field: f.Name, val: f.Val})
		}
		if hidden > 0 {
			items = append(items, item{more: hidden})
		}
		return v.Record.Type + "{", "}", items, true
	case runtime.KindVariant:
		return v.Variant.Tag + "(", ")", valueItems(v.Variant.Args, lim), false
	default:
		return "", "", nil, false
	}
}

func valueItems(vs []runtime.Value, lim Limits) []item {
	shown, hidden := clip(len(vs), lim.elems())
	items := make([]item, 0, shown+1)
	for i := 0; i < shown; i++ {
		items = append(items, item{val: vs[i]})
	}
	if hidden > 0 {
		items = append(items, item{more: hidden})
	}
	return items
}

func clip(n, limit int) (shown, hidden int) {
	if limit < 0 || n <= limit {
		return n, 0
	}
	return limit, n - limit
}

func flatItem(it item, depth int, lim Limits) string {
	if it.more > 0 {
		return fmt.Sprintf("… %d more", it.more)
	}
	if it.pair {
		return flatForm(it.key, true, depth+1, lim) + " => " + flatForm(it.val, true, depth+1, lim)
	}
	if it.field != "" {
		return it.field + ": " + flatForm(it.val, true, depth+1, lim)
	}
	return flatForm(it.val, true, depth+1, lim)
}

func brokenItem(it item, depth, indent, width int, lim Limits) string {
	if it.more > 0 {
		return fmt.Sprintf("… %d more", it.more)
	}
	child := indent + 2
	if it.pair {
		key := render(it.key, true, depth+1, child, width, lim)
		pref := key + " => "
		valCol := child + displayWidth(firstLine(pref))
		val := render(it.val, true, depth+1, valCol, width, lim)
		return joinPrefix(pref, val)
	}
	if it.field != "" {
		pref := it.field + ": "
		val := render(it.val, true, depth+1, child+displayWidth(pref), width, lim)
		return joinPrefix(pref, val)
	}
	return render(it.val, true, depth+1, child, width, lim)
}

func sortedMap(es []runtime.MapEntry) []runtime.MapEntry {
	out := append([]runtime.MapEntry(nil), es...)
	sort.SliceStable(out, func(i, j int) bool {
		c, err := runtime.Compare(out[i].Key, out[j].Key)
		return err == nil && c < 0
	})
	return out
}

func joinPrefix(prefix, val string) string {
	if !strings.Contains(val, "\n") {
		return prefix + val
	}
	pad := strings.Repeat(" ", displayWidth(prefix))
	lines := strings.Split(val, "\n")
	for i := 1; i < len(lines); i++ {
		lines[i] = pad + lines[i]
	}
	return prefix + strings.Join(lines, "\n")
}

func indentLines(s string, n int) string {
	pad := strings.Repeat(" ", n)
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = pad + lines[i]
	}
	return strings.Join(lines, "\n")
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func displayWidth(s string) int { return utf8.RuneCountInString(s) }

func colorize(text string, opt Print) string {
	if text == "" {
		return ""
	}
	env := opt.Env
	if env.Prelude == nil && env.Modules == nil && env.Bindings == nil {
		env = highlight.REPLEnv()
	}
	res := highlight.Classify(text, -1, env)
	res.Guides = nil
	return opt.Pal.Paint(text, res)
}
