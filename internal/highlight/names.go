package highlight

import (
	"strings"

	"github.com/it1ro/brig-lang/internal/sema"
)

type vis struct {
	name string
	from int
}

type modVis struct {
	name    string
	from    int
	members map[string]int
}

type ann struct {
	class Class
	set   bool
	match bool
}

type resolver struct {
	src   string
	toks  []token
	env   Env
	ann   []ann
	binds []vis
	mods  []modVis
	types []vis
	mod   string
}

func resolve(src string, toks []token, env Env) []ann {
	r := &resolver{
		src:  src,
		toks: toks,
		env:  env,
		ann:  make([]ann, len(toks)),
	}
	r.collect()
	r.modules()
	r.plain()
	r.names()
	return r.ann
}

func (r *resolver) text(i int) string { return r.toks[i].text(r.src) }

func (r *resolver) force(i int, c Class) {
	if i < 0 || i >= len(r.ann) || r.ann[i].set {
		return
	}
	r.ann[i] = ann{class: c, set: true}
}

func (r *resolver) next(i int) int {
	i++
	for i < len(r.toks) && (r.toks[i].kind == tComment || r.toks[i].kind == tDoc) {
		i++
	}
	return i
}

func (r *resolver) collect() {
	for i := 0; i < len(r.toks); i++ {
		if r.toks[i].kind != tKeyword && r.toks[i].kind != tOp {
			continue
		}
		switch r.text(i) {
		case "fn":
			r.fn(i)
		case "import":
			r.importAt(r.next(i))
		case "module":
			r.moduleKw(i)
		case "alias":
			r.aliasAt(i)
		case "type":
			r.typeKw(i)
		case "=":
			r.markPattern(i, r.assignFrom(i))
		case "->":
			r.markPattern(i, -1)
		case "<-":
			// Имя паттерна видно со следующей строки: правая часть `<-`
			// его не видит, следующие клаузы `with` — видят.
			r.markPattern(i, r.nextLine(i))
		}
	}
}

// assignFrom — позиция, с которой имя слева от `=` видно. Правая
// часть того же стейтмента его не видит: `x = x` справа — прежнее имя.
func (r *resolver) assignFrom(eq int) int {
	pos := r.toks[eq].end
	depth := 0
	ti := eq + 1
	for pos <= len(r.src) {
		rel := strings.IndexByte(r.src[pos:], '\n')
		if rel < 0 {
			return len(r.src)
		}
		lineEnd := pos + rel
		for ti < len(r.toks) && r.toks[ti].start < lineEnd {
			depth += r.toks[ti].delta(r.src)
			ti++
		}
		next := lineEnd + 1
		if depth <= 0 && !r.lineCont(next) {
			return next
		}
		pos = next
	}
	return len(r.src)
}

func (r *resolver) nextLine(i int) int {
	rel := strings.IndexByte(r.src[r.toks[i].end:], '\n')
	if rel < 0 {
		return len(r.src)
	}
	return r.toks[i].end + rel + 1
}

func (r *resolver) lineCont(lineStart int) bool {
	if lineStart >= len(r.src) {
		return false
	}
	lim := len(r.src)
	if rel := strings.IndexByte(r.src[lineStart:], '\n'); rel >= 0 {
		lim = lineStart + rel
	}
	for _, t := range r.toks {
		if t.start < lineStart {
			continue
		}
		if t.start >= lim {
			return false
		}
		if t.kind == tComment || t.kind == tDoc {
			return false
		}
		return isCont(t.text(r.src))
	}
	return false
}

func (r *resolver) fn(i int) {
	k := r.next(i)
	if k < len(r.toks) && r.toks[k].kind == tLower {
		r.defBinding(k, r.toks[k].end)
		if r.mod != "" && r.topFn(i) {
			r.addMember(r.mod, r.text(k), r.toks[k].end)
		}
		k = r.next(k)
	}
	if k < len(r.toks) && r.text(k) == "(" {
		r.markParams(k)
	}
}

func (r *resolver) topFn(i int) bool {
	if r.col0(i) {
		return true
	}
	p := i - 1
	for p >= 0 && (r.toks[p].kind == tComment || r.toks[p].kind == tDoc) {
		p--
	}
	return p >= 0 && r.text(p) == "pub" && r.col0(p)
}

func (r *resolver) col0(i int) bool {
	k := r.toks[i].start
	for k > 0 && r.src[k-1] != '\n' {
		if r.src[k-1] != ' ' && r.src[k-1] != '\t' {
			return false
		}
		k--
	}
	return r.toks[i].start == k
}

func (r *resolver) markParams(open int) {
	depth := 0
	for k := open; k < len(r.toks); k++ {
		depth += r.toks[k].delta(r.src)
		if k != open && depth == 0 {
			return
		}
		if depth >= 1 && r.toks[k].kind == tLower && !r.colonNext(k) {
			r.defBinding(k, r.toks[k].end)
		}
	}
}

func (r *resolver) colonNext(i int) bool {
	k := r.next(i)
	return k < len(r.toks) && r.text(k) == ":"
}

func (r *resolver) defBinding(i int, from int) {
	if i < 0 || i >= len(r.ann) || r.ann[i].set {
		return
	}
	r.binds = append(r.binds, vis{name: r.text(i), from: from})
	r.force(i, Binding)
}

func (r *resolver) moduleKw(i int) {
	k := r.next(i)
	if k >= len(r.toks) || r.toks[k].kind != tUpper {
		return
	}
	r.addMod(k)
	r.mod = r.text(k)
}

func (r *resolver) importAt(k int) {
	for k < len(r.toks) && r.toks[k].kind == tUpper {
		r.addMod(k)
		d := r.next(k)
		if d >= len(r.toks) || r.text(d) != "." {
			return
		}
		u := r.next(d)
		if u >= len(r.toks) || r.toks[u].kind != tUpper {
			return
		}
		k = u
	}
}

func (r *resolver) aliasAt(i int) {
	k := r.next(i)
	if k < len(r.toks) && r.toks[k].kind == tUpper {
		r.importAt(k)
	}
	for k < len(r.toks) && r.toks[k].kind != tKeyword {
		k = r.next(k)
	}
	if k < len(r.toks) && r.text(k) == "as" {
		n := r.next(k)
		if n < len(r.toks) && r.toks[n].kind == tUpper {
			r.addMod(n)
		}
	}
}

func (r *resolver) typeKw(i int) {
	k := r.next(i)
	if k >= len(r.toks) || r.toks[k].kind != tUpper {
		return
	}
	r.types = append(r.types, vis{name: r.text(k), from: r.toks[k].end})
	r.force(k, Type)
}

func (r *resolver) addMod(i int) {
	r.mods = append(r.mods, modVis{
		name:    r.text(i),
		from:    r.toks[i].end,
		members: map[string]int{},
	})
	r.force(i, Module)
}

func (r *resolver) addMember(mod, name string, from int) {
	for i := len(r.mods) - 1; i >= 0; i-- {
		if r.mods[i].name == mod {
			r.mods[i].members[name] = from
			return
		}
	}
}

// markPattern помечает имена паттерна перед `=` или `->`.
// from < 0 — имя видно сразу после себя; иначе с позиции from.
func (r *resolver) markPattern(idx, from int) {
	start, depth := idx, 0
	line := lineAt(r.src, r.toks[idx].start)
	for j := idx - 1; j >= 0; j-- {
		if depth == 0 && lineAt(r.src, r.toks[j].start) < line {
			break
		}
		start = j
		depth -= r.toks[j].delta(r.src)
		if depth < 0 {
			start = j + 1
			break
		}
	}
	for k := start; k < idx; k++ {
		if r.toks[k].kind == tLower && !r.colonNext(k) {
			visFrom := r.toks[k].end
			if from >= 0 {
				visFrom = from
			}
			r.defBinding(k, visFrom)
		}
	}
}

func (r *resolver) modules() {
	for i := 0; i < len(r.toks); i++ {
		if r.ann[i].set || r.toks[i].kind != tUpper {
			continue
		}
		d := r.next(i)
		m := r.next(d)
		if d >= len(r.toks) || r.text(d) != "." || m >= len(r.toks) {
			continue
		}
		if r.toks[m].kind != tLower && r.toks[m].kind != tUpper {
			continue
		}
		mod, fn := r.text(i), r.text(m)
		knownMod, knownFn := r.lookupMod(mod, fn, r.toks[m].start)
		if !knownMod {
			r.force(i, Unknown)
			r.force(m, Unknown)
			continue
		}
		r.force(i, Module)
		if r.toks[m].kind == tUpper && (!knownFn || !builtinMod(mod)) {
			r.force(m, Module)
			continue
		}
		if knownFn {
			if builtinMod(mod) {
				r.force(m, Prelude)
			} else {
				r.force(m, Binding)
			}
		} else {
			r.force(m, Unknown)
		}
	}
}

// builtinMod — модуль из sema.BuiltinModules или Repl: его функции
// красятся как прелюдия, функции модулей пользователя — как привязки.
func builtinMod(name string) bool {
	if name == "Repl" {
		return true
	}
	_, ok := builtinMods[name]
	return ok
}

var builtinMods = sema.BuiltinModules()

func (r *resolver) lookupMod(mod, fn string, pos int) (knownMod, knownFn bool) {
	if members, ok := r.env.Modules[mod]; ok {
		return true, members[fn]
	}
	for _, m := range r.mods {
		if m.name != mod || m.from > pos {
			continue
		}
		knownMod = true
		if from, ok := m.members[fn]; ok && from <= pos {
			knownFn = true
		}
	}
	return knownMod, knownFn
}

func (r *resolver) plain() {
	for i := 0; i < len(r.toks); i++ {
		if r.ann[i].set {
			continue
		}
		if r.toks[i].kind != tLower && r.toks[i].kind != tUpper {
			continue
		}
		if r.colonNext(i) || r.field(i) {
			r.ann[i].set = true
		}
	}
}

func (r *resolver) field(i int) bool {
	if i == 0 {
		return false
	}
	d := i - 1
	for d >= 0 && (r.toks[d].kind == tComment || r.toks[d].kind == tDoc) {
		d--
	}
	if d < 0 || r.text(d) != "." {
		return false
	}
	return !r.ann[i].set
}

func (r *resolver) names() {
	for i := 0; i < len(r.toks); i++ {
		if r.ann[i].set {
			continue
		}
		switch r.toks[i].kind {
		case tLower:
			r.ann[i] = ann{class: r.lower(r.text(i), r.toks[i].start), set: true}
		case tUpper:
			r.ann[i] = ann{class: r.upper(r.text(i), r.toks[i].start), set: true}
		}
	}
}

func (r *resolver) lower(name string, pos int) Class {
	if r.bound(name, pos) {
		return Binding
	}
	if r.env.Helpers[name] {
		return Helper
	}
	if r.env.Prelude[name] {
		return Prelude
	}
	return Unknown
}

func (r *resolver) upper(name string, pos int) Class {
	if r.bound(name, pos) {
		return Binding
	}
	if r.env.Prelude[name] {
		return Prelude
	}
	if r.typed(name, pos) {
		return Type
	}
	if known, _ := r.lookupMod(name, "", pos); known {
		return Module
	}
	return Unknown
}

func (r *resolver) bound(name string, pos int) bool {
	if r.env.Bindings[name] {
		return true
	}
	for _, b := range r.binds {
		if b.name == name && b.from <= pos {
			return true
		}
	}
	return false
}

func (r *resolver) typed(name string, pos int) bool {
	if r.env.Types[name] {
		return true
	}
	for _, t := range r.types {
		if t.name == name && t.from <= pos {
			return true
		}
	}
	return false
}

func lineAt(src string, off int) int {
	if off < 0 {
		return 0
	}
	if off > len(src) {
		off = len(src)
	}
	return strings.Count(src[:off], "\n")
}
