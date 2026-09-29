package sema

import (
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/it1ro/brig-lang/internal/ast"
	"github.com/it1ro/brig-lang/stdlib"
)

// Проход имён (§F.3, T-139): вызов имени, которое не связано как значение,
// не является функцией своего модуля, прелюдии или видимого модуля, либо
// не имеет функции этой арности, — ошибка
// `undefined function name/arity`. Вызов значения из переменной и вызов со
// спредом по арности не проверяются. REPL этот проход не вызывает:
// неизвестное имя остаётся ошибкой рантайма (§11.4).

// Module — модуль программы для NewWorld. Name — имя, под которым модуль
// виден импортёрам (как его разрешил загрузчик).
type Module struct {
	Name string
	Prog *ast.Program
}

// World — сигнатуры функций и конструкторов пользовательских модулей.
type World struct {
	mods map[string]map[string]sig
	// priv — функции без `pub` (§11.2). Конструкторы всегда публичны.
	priv map[string]map[string]bool
}

// NewWorld собирает сигнатуры модулей. Встроенные модули сюда не входят.
func NewWorld(mods []Module) *World {
	w := &World{
		mods: make(map[string]map[string]sig, len(mods)),
		priv: make(map[string]map[string]bool, len(mods)),
	}
	for _, m := range mods {
		if m.Name == "" || m.Prog == nil {
			continue
		}
		w.mods[m.Name] = signatures(m.Prog)
		w.priv[m.Name] = PrivateFns(m.Prog)
	}
	return w
}

// PrivateFns — имена функций модуля без `pub` (§11.2).
func PrivateFns(prog *ast.Program) map[string]bool {
	out := map[string]bool{}
	if prog == nil {
		return out
	}
	for _, d := range prog.Decls {
		if fd, ok := d.(ast.FuncDecl); ok && !fd.IsPub() {
			out[fd.FnName()] = true
		}
	}
	return out
}

func (w *World) private(mod, fn string) bool {
	return w != nil && w.priv[mod][fn]
}

func (w *World) lookup(mod, fn string) (sig, bool) {
	if w == nil {
		return sig{}, false
	}
	m := w.mods[mod]
	if m == nil {
		return sig{}, false
	}
	s, ok := m[fn]
	return s, ok
}

func (w *World) has(mod string) bool {
	if w == nil {
		return false
	}
	_, ok := w.mods[mod]
	return ok
}

// CheckNames — контекстный анализ плюс разрешение имён по world.
// world == nil: видны только функции этого файла и встроенные модули.
func CheckNames(prog *ast.Program, world *World) *Result {
	return checkNames(prog, world, false)
}

// CheckNamesSession — CheckNames для модуля, загруженного в сессию REPL.
// Квалифицированные `Repl.*` разрешены (модуль может вызвать регистрацию
// хелперов). Голых имён хелперов по-прежнему нет (§11.4).
func CheckNamesSession(prog *ast.Program, world *World) *Result {
	return checkNames(prog, world, true)
}

func checkNames(prog *ast.Program, world *World, session bool) *Result {
	c := &checker{
		prelude: preludeNames(),
		resolve: true,
		session: session,
		world:   world,
		module:  prog.Module,
		own:     signatures(prog),
		ctors:   ctorNames(prog),
		imports: importMap(prog),
	}
	c.checkProgram(prog)
	return &Result{Diagnostics: c.diags}
}

// replMod — функции модуля Repl, видимые в сессии квалифицированно.
// register — API регистрации (T-206), не голая команда консоли.
var replMod = map[string]sig{
	"h": exact(1), "i": exact(1), "v": exact(0, 1),
	"bindings": exact(0), "reset": exact(0), "load": exact(1),
	"flush": exact(0), "time": exact(1), "dis": exact(1),
	"recompile": exact(0), "register": exact(1),
	"tree": exact(0), "info": exact(1), "top": exact(1), "observe": exact(0),
}

// sig — допустимые арности. varMin >= 0 — вариадик: любой вызов с argc >= varMin.
// exact — точные арности (у не-вариадика; у mailbox_size — 0 и 1).
type sig struct {
	exact  []int
	varMin int
}

func exact(ns ...int) sig { return sig{exact: ns, varMin: -1} }

// label — арности для сообщения: `1`, `1,2`, `1..` у вариадика.
func (s sig) label() string {
	return strings.Join(arityLabels(s), ",")
}

// arityLabels — арности по возрастанию: "2", у вариадика "0..".
func arityLabels(s sig) []string {
	ns := append([]int(nil), s.exact...)
	sort.Ints(ns)
	var ls []string
	for _, n := range ns {
		ls = append(ls, strconv.Itoa(n))
	}
	if s.varMin >= 0 {
		ls = append(ls, strconv.Itoa(s.varMin)+"..")
	}
	return ls
}

func variadic(fixed int) sig { return sig{varMin: fixed} }

func (s sig) matches(n int) bool {
	if s.varMin >= 0 && n >= s.varMin {
		return true
	}
	for _, a := range s.exact {
		if a == n {
			return true
		}
	}
	return false
}

// bareBuiltins — голые имена прелюдии, конструкторы и акторные опкоды (§11.5).
// Список функций совпадает с InstallPrelude / InstallJSONPrelude / InstallTestPrelude.
var bareBuiltins = map[string]sig{
	"map": exact(2), "filter": exact(2), "find": exact(2),
	"all": exact(2), "any": exact(2), "fold": exact(3),
	"len": exact(1), "list": variadic(0), "set": variadic(0),
	"to_str": exact(1), "to_int": exact(1), "to_float": exact(1),
	"print": variadic(0), "eprint": variadic(0), "log": variadic(0),
	"assert": exact(1), "raise": exact(1),
	"Some": exact(1), "Ok": exact(1), "Error": exact(1),
	"send": exact(2), "spawn": exact(1, 2), "spawn_linked": exact(1, 2),
	"spawn_watched": exact(1, 2), "exit": exact(2),
	"register": exact(2), "unregister": exact(1), "whereis": exact(1),
	"await": exact(2), "reply": exact(3),
	"link": exact(1), "watch": exact(1), "unwatch": exact(1),
	"self": exact(0), "make_ref": exact(0), "mailbox_size": exact(0, 1),
}

// modBuiltins — функции встроенных модулей. Json.encode — 1 или 2
// аргумента (§4.7), не открытый вариадик.
var modBuiltins = map[string]map[string]sig{
	"Vec":    {"push": exact(2), "set": exact(3), "get": exact(2), "len": exact(1)},
	"Map":    {"put": exact(3), "get": exact(2), "remove": exact(2), "keys": exact(1)},
	"Record": {"to_anon": exact(1)},
	"Str": {
		"to_bytes": exact(1), "split": exact(2), "join": exact(2), "trim": exact(1),
		"find": exact(2), "replace": exact(3), "starts_with?": exact(2), "ends_with?": exact(2),
		"lower": exact(1), "upper": exact(1), "slice": exact(3), "to_int": exact(1),
	},
	"Bytes": {
		"to_str": exact(1), "slice": exact(3), "find": exact(2), "split": exact(2),
		"concat": exact(2), "at": exact(2),
	},
	"Json": {"encode": exact(1, 2), "decode": exact(1)},
	"Test": {
		"describe": exact(1), "it": exact(2), "run": exact(0),
		"assert_eq": exact(2), "assert_ne": exact(2), "assert": exact(1), "fail": exact(1),
	},
	"Sys":       {"args": exact(0), "halt": exact(1)},
	"Actor":     {"info": exact(1), "list": exact(0)},
	"Global":    {"put": exact(2), "get": exact(1)},
	"Timer":     {"send_after": exact(3), "cancel": exact(1)},
	"Time":      {"monotonic_ms": exact(0), "now": exact(0)},
	"Telemetry": {"attach": exact(3), "detach": exact(1), "emit": exact(3)},
	"Port":      {"close": exact(1), "request": exact(1), "write": exact(2), "give": exact(2)},
	"Signal":    {"subscribe": exact(1)},
	"File":      {"open": exact(2)},
}

// isNativeMod — встроенный модуль на Go. Список совпадает с
// loader.builtinModules и compiler.isNativeModule.
func isNativeMod(name string) bool {
	switch name {
	case "Vec", "Map", "Record", "Str", "Bytes", "Json", "Test", "Sys", "Actor", "Prelude", "Global", "Timer", "Time", "Telemetry", "Port", "Signal", "File":
		return true
	}
	return false
}

// isBuiltinMod — модуль, доступный без import (§11.1): Go-нативный или
// встроенный модуль на Brig (stdlib, T-146).
func isBuiltinMod(name string) bool { return isNativeMod(name) || stdlib.IsModule(name) }

// stdlibWorld — сигнатуры и приватность модулей stdlib на Brig. Строится
// из тех же встроенных исходников, что компилирует compiler.StdlibImage.
var stdlibWorld = sync.OnceValue(func() *World {
	ms := stdlib.MustModules()
	mods := make([]Module, len(ms))
	for i, m := range ms {
		mods[i] = Module{Name: m.Name, Prog: m.Prog}
	}
	return NewWorld(mods)
})

// worldFor — где искать функции модуля full: модуль stdlib, которого нет
// среди модулей программы, — во встроенном мире stdlib.
func (c *checker) worldFor(full string) *World {
	if stdlib.IsModule(full) && !c.world.has(full) {
		return stdlibWorld()
	}
	return c.world
}

// stdlibPub — публичные функции модулей stdlib (для подсветки). Их
// документацию h строит из исходника, как у загруженных модулей.
func stdlibPub() map[string]map[string]sig {
	w := stdlibWorld()
	out := make(map[string]map[string]sig, len(w.mods))
	for mod, fns := range w.mods {
		pub := make(map[string]sig, len(fns))
		for name, sg := range fns {
			if !w.priv[mod][name] {
				pub[name] = sg
			}
		}
		out[mod] = pub
	}
	return out
}

func lookupBuiltin(mod, member string) (sig, bool) {
	if mod == "Prelude" {
		// Акторные примитивы — опкоды, не глобалы Prelude.* (aliasPrelude),
		// но вызов `Prelude.send(…)` компилируется в тот же опкод (§11.5).
		s, ok := bareBuiltins[member]
		return s, ok
	}
	m := modBuiltins[mod]
	if m == nil {
		return sig{}, false
	}
	s, ok := m[member]
	return s, ok
}

func signatures(prog *ast.Program) map[string]sig {
	out := map[string]sig{}
	if prog == nil {
		return out
	}
	for _, d := range prog.Decls {
		switch d := d.(type) {
		case ast.TypeDecl:
			vs, ok := d.Variants()
			if !ok {
				continue
			}
			for _, v := range vs {
				// Конструктор без полей — значение, не функция.
				if n := len(v.Fields); n > 0 {
					out[v.Name] = exact(n)
				}
			}
		case ast.FuncDecl:
			out[d.FnName()] = sigOfFn(d.FuncClauses())
		}
	}
	return out
}

func sigOfFn(clauses []ast.FnClauseArg) sig {
	lists := make([][]ast.Pattern, len(clauses))
	for i, cl := range clauses {
		lists[i] = cl.Params
	}
	return sigFrom(lists)
}

func sigFrom(paramLists [][]ast.Pattern) sig {
	s := sig{varMin: -1}
	for _, ps := range paramLists {
		n := len(ps)
		variadic := false
		if n > 0 {
			if _, ok := ps[n-1].(ast.SpreadPattern); ok {
				variadic = true
				n--
			}
		}
		if variadic {
			if s.varMin < 0 || n < s.varMin {
				s.varMin = n
			}
			continue
		}
		s.exact = append(s.exact, n)
	}
	return s
}

func importMap(prog *ast.Program) map[string]string {
	out := map[string]string{}
	if prog == nil {
		return out
	}
	add := func(local, full string) {
		if local != "" {
			out[local] = full
		}
	}
	for _, d := range prog.Decls {
		switch d := d.(type) {
		case ast.ImportDecl:
			full := d.ImportedModule()
			add(full, full)
			add(lastSeg(full), full)
		case ast.AliasDecl:
			full := d.AliasOriginal()
			add(d.AliasName(), full)
			add(full, full)
			add(lastSeg(full), full)
		}
	}
	return out
}

func lastSeg(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 {
		return name[i+1:]
	}
	return name
}

func (c *checker) prebindLocalFns(stmts []ast.Stmt) {
	if !c.resolve {
		return
	}
	for _, s := range stmts {
		lfd, ok := s.(ast.LocalFnDecl)
		if !ok {
			continue
		}
		var lists [][]ast.Pattern
		for _, cl := range lfd.Clauses() {
			lists = append(lists, cl.Params)
		}
		line, col := posOf(s)
		c.bindLocalFn(lfd.FnName(), sigFrom(lists), line, col)
	}
}

func (c *checker) bindLocalFn(name string, s sig, line, col int) {
	c.bind(name, "local fn", line, col)
	scope := c.topScope()
	b, ok := scope[name]
	if !ok || b.kind != "local fn" {
		return
	}
	b.isFn = true
	b.sig = s
	scope[name] = b
}

func (c *checker) checkCall(call ast.CallExpr) {
	if !c.resolve {
		return
	}
	switch cal := call.Callee().(type) {
	case ast.VariableExpr:
		c.checkNamed(cal.Name(), call.Args(), cal)
	case ast.MemberExpr:
		segs, ok := modulePath(cal)
		if !ok {
			return // obj.method(a) — вызов значения, не имени функции
		}
		mod, member := splitPath(segs)
		c.checkQual(mod, member, call.Args(), pathStart(cal))
	}
}

func (c *checker) checkPipeName(v ast.VariableExpr, p ast.PipeExpr) {
	if !c.resolve {
		return
	}
	args := make([]ast.Expr, 0, 1+len(p.PipeArgs()))
	args = append(args, p.PipeLHS())
	args = append(args, p.PipeArgs()...)
	c.checkNamed(v.Name(), args, v)
}

func (c *checker) checkNamed(name string, args []ast.Expr, at ast.Node) {
	if isSpecialCall(name) {
		return
	}
	if mod, member, ok := qualName(name); ok {
		c.checkQual(mod, member, args, at)
		return
	}
	s, dynamic, found := c.bareSig(name)
	c.finish(name, s, found, dynamic, args, at)
}

func (c *checker) checkQual(mod, member string, args []ast.Expr, at ast.Node) {
	s, full, found, missingImport := c.qualSig(mod, member)
	if missingImport {
		line, col := posOf(at)
		c.err(line, col, "module %s is not imported", mod)
		return
	}
	if found && full != c.module && c.worldFor(full).private(full, member) {
		line, col := posOf(at)
		argc, _ := argcOf(args)
		c.err(line, col, "%s/%d is private to %s", member, argc, full)
		return
	}
	c.finish(mod+"."+member, s, found, false, args, at)
}

// checkQualRef — путь `M.x` вне позиции вызова (§7.6, T-144). `M.f` —
// значение-функция: неизвестная функция, приватная функция другого модуля
// (§11.2) и модуль без import — ошибки, как у вызова; арность в сообщении
// — из объявления. Акторный примитив опкод, не значение (§12.6).
// Конструкторы `M.Ctor` проверяет компилятор.
func (c *checker) checkQualRef(me ast.MemberExpr, segs []string) {
	mod, member := splitPath(segs)
	if isUpper(member) {
		return
	}
	line, col := posOf(pathStart(me))
	if mod == "Prelude" && actorPrimitives[member] {
		c.err(line, col, "actor primitive %s.%s cannot be used as a value (§12.6)", mod, member)
		return
	}
	if !c.resolve {
		return
	}
	s, full, found, missingImport := c.qualSig(mod, member)
	switch {
	case missingImport:
		c.err(line, col, "module %s is not imported", mod)
	case !found:
		c.err(line, col, "undefined function %s.%s", mod, member)
	case full != c.module && c.worldFor(full).private(full, member):
		c.err(line, col, "%s/%s is private to %s", member, s.label(), full)
	}
}

// checkModuleValue — имя модуля в позиции выражения значением не является
// (§7.6): модули как значения вне v0.4.8.
func (c *checker) checkModuleValue(v ast.VariableExpr) {
	name := v.Name()
	if !c.resolve || !isUpper(name) || strings.Contains(name, ".") || c.ctors[name] {
		return
	}
	if _, imported := c.imports[name]; imported || isBuiltinMod(name) || c.world.has(name) {
		line, col := posOf(v)
		c.err(line, col, "module %s is not a value (§7.6)", name)
	}
}

// isHelperH — callee `Repl.h`: в сессии его аргумент может быть именем модуля.
func isHelperH(callee ast.Expr) bool {
	segs, ok := modulePath(callee)
	return ok && len(segs) == 2 && segs[0] == "Repl" && segs[1] == "h"
}

// ctorNames — конструкторы вариант-деклараций модуля, в том числе без полей.
func ctorNames(prog *ast.Program) map[string]bool {
	out := map[string]bool{}
	for _, d := range prog.Decls {
		if td, ok := d.(ast.TypeDecl); ok {
			if vs, ok := td.Variants(); ok {
				for _, v := range vs {
					out[v.Name] = true
				}
			}
		}
	}
	return out
}

// qualSig разрешает Mod.f: import/alias, затем встроенный модуль (§11.1).
// full — полное имя модуля. missingImport — модуль есть в программе, но в
// этом файле не импортирован.
func (c *checker) qualSig(mod, member string) (s sig, full string, found, missingImport bool) {
	if c.session && mod == "Repl" {
		s, found = replMod[member]
		return s, mod, found, false
	}
	full, imported := c.imports[mod]
	switch {
	case imported:
		// full — имя модуля
	case isBuiltinMod(mod):
		full = mod
	case c.world.has(mod):
		return sig{}, mod, false, true
	default:
		return sig{}, mod, false, false
	}
	if isNativeMod(full) {
		s, found = lookupBuiltin(full, member)
		return s, full, found, false
	}
	s, found = c.worldFor(full).lookup(full, member)
	return s, full, found, false
}

// bareSig: переменная области → динамический вызов; иначе локальная fn,
// функция своего модуля, прелюдия.
func (c *checker) bareSig(name string) (s sig, dynamic, found bool) {
	for i := len(c.scopes) - 1; i >= 0; i-- {
		b, ok := c.scopes[i][name]
		if !ok {
			continue
		}
		if b.isFn {
			return b.sig, false, true
		}
		return sig{}, true, true
	}
	if s, ok := c.own[name]; ok {
		return s, false, true
	}
	if s, ok := bareBuiltins[name]; ok {
		return s, false, true
	}
	return sig{}, false, false
}

func (c *checker) finish(display string, s sig, found, dynamic bool, args []ast.Expr, at ast.Node) {
	if dynamic {
		return
	}
	argc, spread := argcOf(args)
	if found && (spread || s.matches(argc)) {
		return
	}
	line, col := posOf(at)
	c.err(line, col, "undefined function %s/%d", display, argc)
}

func argcOf(args []ast.Expr) (n int, spread bool) {
	for _, a := range args {
		if u, ok := a.(ast.UnaryExpr); ok && u.OpStr() == ".." {
			spread = true
		}
	}
	return len(args), spread
}

func isSpecialCall(name string) bool {
	switch name {
	case "()", "[]", "%[]", "%{}", "{}":
		return true
	}
	return strings.HasSuffix(name, "{}")
}

func qualName(name string) (mod, member string, ok bool) {
	i := strings.LastIndex(name, ".")
	if i <= 0 || i == len(name)-1 {
		return "", "", false
	}
	mod, member = name[:i], name[i+1:]
	return mod, member, isUpper(mod)
}

func isUpper(s string) bool { return s != "" && s[0] >= 'A' && s[0] <= 'Z' }

func modulePath(e ast.Expr) (segs []string, ok bool) {
	switch x := e.(type) {
	case ast.VariableExpr:
		segs = strings.Split(x.Name(), ".")
		return segs, isUpper(segs[0])
	case ast.MemberExpr:
		segs, ok = modulePath(x.Obj())
		if !ok {
			return nil, false
		}
		return append(segs, x.MemberName()), true
	}
	return nil, false
}

func splitPath(segs []string) (mod, member string) {
	return strings.Join(segs[:len(segs)-1], "."), segs[len(segs)-1]
}

func pathStart(e ast.Expr) ast.Expr {
	for {
		me, ok := e.(ast.MemberExpr)
		if !ok {
			return e
		}
		e = me.Obj()
	}
}

// PreludeNames — голые имена прелюдии (§11.5): функции, конструкторы
// и акторные примитивы. Для подсветки и подсказок консоли.
func PreludeNames() []string { return sortedKeys(preludeNames()) }

// ReplHelperNames — голые имена хелперов консоли (§11.4).
func ReplHelperNames() []string { return sortedKeys(replHelperNames()) }

// BuiltinModules — функции встроенных модулей по имени модуля, включая
// Prelude.* без акторных примитивов: они опкоды, не глобалы.
func BuiltinModules() map[string][]string {
	out := make(map[string][]string, len(modBuiltins)+1)
	for mod, fns := range modBuiltins {
		out[mod] = sortedKeys(fns)
	}
	var pre []string
	for n := range bareBuiltins {
		if !actorPrimitives[n] {
			pre = append(pre, n)
		}
	}
	sort.Strings(pre)
	out["Prelude"] = pre
	for mod, fns := range stdlibPub() {
		out[mod] = sortedKeys(fns)
	}
	return out
}

// BuiltinArities — арности Go-нативных встроенных функций для документации
// h (§11.4), из тех же сигнатур, что проверяет sema. Модули stdlib на Brig
// сюда не входят: их h берёт из исходника. Ключ "" — голые имена прелюдии,
// "Repl" — хелперы консоли, остальные — встроенные модули. Метка — "2"
// или "0.." у вариадика, по возрастанию.
func BuiltinArities() map[string]map[string][]string {
	labels := func(fns map[string]sig) map[string][]string {
		out := make(map[string][]string, len(fns))
		for name, sg := range fns {
			out[name] = arityLabels(sg)
		}
		return out
	}
	out := make(map[string]map[string][]string, len(modBuiltins)+2)
	out[""] = labels(bareBuiltins)
	out["Repl"] = labels(replMod)
	for mod, fns := range modBuiltins {
		out[mod] = labels(fns)
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
