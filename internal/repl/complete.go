package repl

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/it1ro/brig-lang/internal/highlight"
	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/sema"
)

// Candidate — одно дополнение. Insert заменяет span, Display печатается в меню.
type Candidate struct {
	Insert  string
	Display string
}

// Completion — кандидаты одного и того же span. From и To — индексы рун,
// [From, To) редактор заменяет целиком и сам границу имени не ищет.
type Completion struct {
	From, To   int
	Candidates []Candidate
}

// srcTok — токен подсветки. lexer.Lex на незакрытой скобке токены не
// отдаёт, а ввод под курсором почти всегда ещё не программа: разбор берёт
// classify. Имя после точки подсветка не красит и в спаны не кладёт —
// его добираем от байта сразу за `.`.
type srcTok struct {
	start, end int
	bStart     int
	bEnd       int
	text       string
	kind       string
	depth      int
}

// Complete — кандидаты в позиции pos (индекс руны). Вне диапазона —
// пустой результат, строка по байтам не режется.
func (s *Session) Complete(src string, pos int) Completion {
	n := utf8.RuneCountInString(src)
	if pos < 0 || pos > n {
		return Completion{}
	}
	toks := srcTokens(src)
	if t, ok := activeTok(toks, pos); ok {
		switch t.kind {
		case "comment", "keyword":
			return Completion{}
		case "string":
			return s.completeLoad(toks, t, pos)
		case "ident":
			if dot, ok := prevTok(toks, t.start); ok && dot.kind == "dot" {
				return s.completeMember(toks, dot, t.text, t.start, t.end)
			}
			return s.completeBare(t.text, t.start, t.end)
		case "dot":
			return s.completeMember(toks, t, "", pos, pos)
		default:
			return Completion{}
		}
	}
	return s.completeBare("", pos, pos)
}

// Signature — строка сигнатур вызова, в котором стоит курсор.
// text без SGR. argStart и argEnd — руны внутри text; -1 — метки нет
// (вызова нет, курсор уже за `)`, или аргументов больше, чем в любой арности).
func (s *Session) Signature(src string, pos int) (text string, argStart, argEnd int) {
	n := utf8.RuneCountInString(src)
	if pos < 0 || pos > n {
		return "", -1, -1
	}
	toks := srcTokens(src)
	call, ok := innermostCall(toks, pos)
	if !ok {
		return "", -1, -1
	}
	d := s.lookupDoc(call.name)
	if d == nil || len(d.sigs) == 0 {
		return "", -1, -1
	}
	arg := argIndex(toks, call, pos)
	var b strings.Builder
	markS, markE := -1, -1
	for i, sig := range d.sigs {
		if i > 0 {
			b.WriteString("  ")
		}
		base := utf8.RuneCountInString(b.String())
		b.WriteString(sig.text)
		if markS >= 0 {
			continue
		}
		if a, z, ok := paramSpan(sig.text, arg); ok {
			markS, markE = base+a, base+z
		}
	}
	return b.String(), markS, markE
}

type callSite struct {
	name    string
	openEnd int
	close   int // руна `(`, -1 если скобка не закрыта
	depth   int
}

func innermostCall(toks []srcTok, pos int) (callSite, bool) {
	var best callSite
	found := false
	for _, c := range calls(toks) {
		if c.name == "" || pos < c.openEnd {
			continue
		}
		if c.close >= 0 && pos > c.close {
			continue
		}
		if !found || c.depth >= best.depth {
			best = c
			found = true
		}
	}
	return best, found
}

func calls(toks []srcTok) []callSite {
	var out []callSite
	for i, t := range toks {
		if t.kind != "lparen" {
			continue
		}
		c := callSite{name: callName(toks, i), openEnd: t.end, close: -1, depth: t.depth}
		for _, r := range toks[i+1:] {
			if r.kind == "rparen" && r.depth == t.depth {
				c.close = r.start
				break
			}
		}
		out = append(out, c)
	}
	return out
}

func callName(toks []srcTok, paren int) string {
	name, ok := prevTok(toks, toks[paren].start)
	if !ok || name.kind != "ident" {
		return ""
	}
	if dot, ok := prevTok(toks, name.start); ok && dot.kind == "dot" {
		if q, ok := prevTok(toks, dot.start); ok && q.kind == "ident" {
			return q.text + "." + name.text
		}
		return ""
	}
	return name.text
}

func argIndex(toks []srcTok, call callSite, pos int) int {
	n := 0
	for _, t := range toks {
		if t.kind == "comma" && t.depth == call.depth && t.start >= call.openEnd && t.start < pos {
			n++
		}
	}
	return n
}

// paramSpan — руны параметра index внутри текста сигнатуры `name(a, b)`.
func paramSpan(sig string, index int) (int, int, bool) {
	open := strings.IndexByte(sig, '(')
	if open < 0 || !strings.HasSuffix(sig, ")") {
		return 0, 0, false
	}
	inner := sig[open+1 : len(sig)-1]
	if strings.TrimSpace(inner) == "" {
		return 0, 0, false
	}
	parts := strings.Split(inner, ", ")
	if index < 0 || index >= len(parts) {
		return 0, 0, false
	}
	prefix := sig[:open+1]
	for i := 0; i < index; i++ {
		prefix += parts[i] + ", "
	}
	start := utf8.RuneCountInString(prefix)
	return start, start + utf8.RuneCountInString(parts[index]), true
}

func (s *Session) completeBare(prefix string, from, to int) Completion {
	seen := map[string]bool{}
	var out []Candidate
	add := func(c Candidate) {
		if c.Insert == "" || !strings.HasPrefix(c.Insert, prefix) || seen[c.Insert] {
			return
		}
		seen[c.Insert] = true
		out = append(out, c)
	}
	// Победитель Display: привязка, иначе хелпер, иначе прелюдия, иначе модуль.
	for _, b := range s.Bindings() {
		add(Candidate{Insert: b.Name, Display: s.bindingDisplay(b.Name, b.Value)})
	}
	for _, n := range s.extraNames() {
		add(s.docCandidate(n))
	}
	for _, n := range sema.ReplHelperNames() {
		add(s.docCandidate(n))
	}
	for _, n := range sema.PreludeNames() {
		add(s.docCandidate(n))
	}
	var mods []string
	for name, d := range s.docs {
		if d != nil && d.module {
			mods = append(mods, name)
		}
	}
	sort.Strings(mods)
	for _, name := range mods {
		add(Candidate{Insert: name, Display: name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Insert < out[j].Insert })
	return Completion{From: from, To: to, Candidates: out}
}

func (s *Session) bindingDisplay(name string, v runtime.Value) string {
	fn, _, _, _, ok := fnParts(v)
	if !ok {
		return name
	}
	if d := s.lookupDoc(fn); d != nil {
		if label := arityLabel(d); label != "" {
			return name + "/" + label
		}
	}
	return name
}

func (s *Session) docCandidate(name string) Candidate {
	d := s.lookupDoc(name)
	if d == nil {
		return Candidate{Insert: name, Display: name}
	}
	if label := arityLabel(d); label != "" {
		return Candidate{Insert: name, Display: name + "/" + label}
	}
	return Candidate{Insert: name, Display: name}
}

func arityLabel(d *helpDoc) string {
	var ls []string
	seen := map[string]bool{}
	for _, sig := range d.sigs {
		if sig.label == "" || seen[sig.label] {
			continue
		}
		seen[sig.label] = true
		ls = append(ls, sig.label)
	}
	return strings.Join(ls, ",")
}

func (s *Session) completeMember(toks []srcTok, dot srcTok, prefix string, from, to int) Completion {
	qual, ok := prevTok(toks, dot.start)
	if !ok || qual.kind != "ident" {
		return Completion{}
	}
	// Цепочка a.b. — не этот объём: квалификатор сам после точки.
	if p, ok := prevTok(toks, qual.start); ok && p.kind == "dot" {
		return Completion{}
	}
	name := qual.text
	if v, ok := s.env[name]; ok && v.Kind == runtime.KindRecord && v.Record != nil {
		var out []Candidate
		for _, f := range v.Record.Fields {
			if strings.HasPrefix(f.Name, prefix) {
				out = append(out, Candidate{Insert: f.Name, Display: f.Name})
			}
		}
		return Completion{From: from, To: to, Candidates: out}
	}
	d := s.docs[name]
	if d == nil || !d.module {
		return Completion{}
	}
	var out []Candidate
	seen := map[string]bool{}
	for _, f := range d.funs {
		member, _, ok := strings.Cut(f, "/")
		if !ok || seen[member] || !strings.HasPrefix(member, prefix) {
			continue
		}
		seen[member] = true
		disp := member
		if qd := s.docs[name+"."+member]; qd != nil {
			if label := arityLabel(qd); label != "" {
				disp = member + "/" + label
			}
		} else if _, label, ok := strings.Cut(f, "/"); ok && label != "" {
			disp = member + "/" + label
		}
		out = append(out, Candidate{Insert: member, Display: disp})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Insert < out[j].Insert })
	return Completion{From: from, To: to, Candidates: out}
}

// completeLoad дополняет путь в строке аргумента load(. Один Glob на
// вызов. Набранное экранируется, раскрывается только ведущая ~ или ~/.
func (s *Session) completeLoad(toks []srcTok, str srcTok, pos int) Completion {
	if !loadArg(toks, str) {
		return Completion{}
	}
	closed := len(str.text) >= 2 && strings.HasPrefix(str.text, `"`) && strings.HasSuffix(str.text, `"`)
	contentStart := str.start + 1
	contentEnd := str.end
	if closed {
		contentEnd = str.end - 1
	}
	if pos < contentStart || pos > contentEnd {
		return Completion{}
	}
	prefix := runeSlice(str.text, 1, 1+(pos-contentStart))
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	fsPrefix, tilde := expandHome(prefix, home)
	matches, err := filepath.Glob(escapeGlob(fsPrefix) + "*")
	if err != nil {
		return Completion{}
	}
	var out []Candidate
	seen := map[string]bool{}
	for _, m := range matches {
		shown := m
		if tilde {
			shown = restoreHome(m, home)
		}
		if !strings.HasPrefix(shown, prefix) || seen[shown] {
			continue
		}
		seen[shown] = true
		out = append(out, Candidate{Insert: shown, Display: shown})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Insert < out[j].Insert })
	return Completion{From: contentStart, To: pos, Candidates: out}
}

func loadArg(toks []srcTok, str srcTok) bool {
	paren, ok := prevTok(toks, str.start)
	if !ok || paren.kind != "lparen" {
		return false
	}
	name, ok := prevTok(toks, paren.start)
	return ok && name.kind == "ident" && name.text == "load"
}

func expandHome(prefix, home string) (string, bool) {
	if home == "" {
		return prefix, false
	}
	if prefix == "~" {
		return home, true
	}
	if strings.HasPrefix(prefix, "~/") {
		return home + prefix[1:], true
	}
	return prefix, false
}

func restoreHome(path, home string) string {
	if home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	rel, ok := strings.CutPrefix(path, home+"/")
	if !ok {
		return path
	}
	return "~/" + rel
}

func escapeGlob(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '*', '?', '[', '\\':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func (s *Session) lookupDoc(name string) *helpDoc {
	if d := s.docs[name]; d != nil && !d.module && len(d.sigs) > 0 {
		return d
	}
	bestSeq := -1
	var best *helpDoc
	for key, d := range s.docs {
		rest, ok := strings.CutPrefix(key, "__repl__")
		if !ok || d == nil || d.module || len(d.sigs) == 0 {
			continue
		}
		num, bare, ok := strings.Cut(rest, "$")
		if !ok || bare != name {
			continue
		}
		seq, err := strconv.Atoi(num)
		if err != nil || seq < bestSeq {
			continue
		}
		best, bestSeq = d, seq
	}
	return best
}

func srcTokens(src string) []srcTok {
	res := highlight.Classify(src, -1, highlight.Env{})
	var toks []srcTok
	for _, sp := range res.Spans {
		if sp.Start < 0 || sp.End > len(src) || sp.End <= sp.Start {
			continue
		}
		text := src[sp.Start:sp.End]
		if strings.TrimSpace(text) == "" {
			continue
		}
		toks = append(toks, srcTok{
			start:  runeOff(src, sp.Start),
			end:    runeOff(src, sp.End),
			bStart: sp.Start,
			bEnd:   sp.End,
			text:   text,
			kind:   tokKind(sp.Class, text),
		})
	}
	toks = append(toks, fieldNames(src, toks)...)
	sort.Slice(toks, func(i, j int) bool {
		if toks[i].start != toks[j].start {
			return toks[i].start < toks[j].start
		}
		return toks[i].end > toks[j].end
	})
	d := 0
	for i := range toks {
		switch toks[i].kind {
		case "lparen":
			d++
			toks[i].depth = d
		case "rparen":
			toks[i].depth = d
			if d > 0 {
				d--
			}
		default:
			toks[i].depth = d
		}
	}
	return toks
}

// fieldNames — идентификаторы сразу после `.`, которые classify не вернул.
func fieldNames(src string, toks []srcTok) []srcTok {
	covered := func(b int) bool {
		for _, t := range toks {
			if t.bStart <= b && b < t.bEnd {
				return true
			}
		}
		return false
	}
	var out []srcTok
	for _, t := range toks {
		if t.kind != "dot" {
			continue
		}
		b := t.bEnd
		if b >= len(src) || covered(b) {
			continue
		}
		text, end, ok := identAt(src, b)
		if !ok {
			continue
		}
		out = append(out, srcTok{
			start:  runeOff(src, b),
			end:    runeOff(src, end),
			bStart: b,
			bEnd:   end,
			text:   text,
			kind:   "ident",
		})
	}
	return out
}

func identAt(src string, i int) (string, int, bool) {
	if i >= len(src) || !identByte(src[i], true) {
		return "", 0, false
	}
	j := i + 1
	for j < len(src) && identByte(src[j], false) {
		j++
	}
	if j < len(src) && src[j] == '?' {
		j++
	}
	return src[i:j], j, true
}

func identByte(c byte, first bool) bool {
	if c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
		return true
	}
	return !first && c >= '0' && c <= '9'
}

func tokKind(class highlight.Class, text string) string {
	switch class {
	case highlight.Comment, highlight.Doc:
		return "comment"
	case highlight.String:
		return "string"
	case highlight.Keyword:
		return "keyword"
	}
	switch text {
	case ".":
		return "dot"
	case "(":
		return "lparen"
	case ")":
		return "rparen"
	case ",":
		return "comma"
	}
	if class != highlight.Number && class != highlight.Op && class != highlight.Punct && class != highlight.Atom && isBareName(text) {
		return "ident"
	}
	return "other"
}

func isBareName(s string) bool {
	body := strings.TrimSuffix(s, "?")
	if body == "" {
		return false
	}
	for i, r := range body {
		switch {
		case r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z':
		case i > 0 && r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return true
}

func activeTok(toks []srcTok, pos int) (srcTok, bool) {
	var best srcTok
	found := false
	for _, t := range toks {
		if t.start < pos && pos <= t.end {
			best = t
			found = true
		}
	}
	return best, found
}

func prevTok(toks []srcTok, start int) (srcTok, bool) {
	var prev srcTok
	ok := false
	for _, t := range toks {
		if t.end <= start {
			prev = t
			ok = true
			continue
		}
		break
	}
	return prev, ok
}

func runeOff(s string, byteOff int) int {
	if byteOff < 0 {
		return 0
	}
	if byteOff > len(s) {
		byteOff = len(s)
	}
	return utf8.RuneCountInString(s[:byteOff])
}

// runeSlice — руны [from, to) строки s. from и to — индексы рун.
func runeSlice(s string, from, to int) string {
	if from < 0 {
		from = 0
	}
	if to < from {
		return ""
	}
	b := 0
	start, end := 0, len(s)
	i := 0
	for b < len(s) && i < to {
		_, sz := utf8.DecodeRuneInString(s[b:])
		if sz < 1 {
			sz = 1
		}
		if i == from {
			start = b
		}
		b += sz
		i++
		if i == to {
			end = b
		}
	}
	if from >= i {
		return ""
	}
	if to > i {
		end = len(s)
	}
	return s[start:end]
}
