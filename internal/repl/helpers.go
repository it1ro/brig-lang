package repl

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/it1ro/brig-lang/internal/ast"
	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/highlight"
	"github.com/it1ro/brig-lang/internal/lexer"
	"github.com/it1ro/brig-lang/internal/loader"
	"github.com/it1ro/brig-lang/internal/parser"
	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/sema"
	"github.com/it1ro/brig-lang/internal/vm"
	"github.com/it1ro/brig-lang/stdlib"
)

// Хелперы консоли (§11.4) — нативы модуля Repl. В сессии они стоят и
// голыми именами, и как Repl.h. Вне сессии глобалов нет: brig check
// файла с h(x) — undefined function h/1.

func (s *Session) installHelpers() {
	s.initDocs()
	s.indexStdlib()
	defs := map[string]runtime.Value{}
	// Голое имя — своя FuncValue с голым Name: h(v) печатает `v/0`, а
	// Repl.h(Repl.v) — `Repl.v/0`, как ввёл пользователь.
	add := func(name string, arity int, bare bool, fn runtime.NativeFunc) {
		defs["Repl."+name] = runtime.Func(&runtime.FuncValue{Name: "Repl." + name, Arity: arity, IsNative: true, Native: fn})
		if bare {
			defs[name] = runtime.Func(&runtime.FuncValue{Name: name, Arity: arity, IsNative: true, Native: fn})
		}
	}
	add("h", 1, true, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		return s.help(args[0])
	})
	add("i", 1, true, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if err := s.info(args[0]); err != nil {
			return runtime.Unit, err
		}
		return runtime.Unit, nil
	})
	add("v", -1, true, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		return s.value(args)
	})
	add("bindings", 0, true, func(_ runtime.Caller, _ []runtime.Value) (runtime.Value, error) {
		return s.bindingsMap(), nil
	})
	add("reset", 0, true, func(_ runtime.Caller, _ []runtime.Value) (runtime.Value, error) {
		s.Reset()
		return runtime.Unit, nil
	})
	add("load", 1, true, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		return s.load(args[0])
	})
	add("flush", 0, true, func(_ runtime.Caller, _ []runtime.Value) (runtime.Value, error) {
		return s.flush()
	})
	add("time", 1, true, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		return s.timed(args[0])
	})
	add("dis", 1, true, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		return s.dis(args[0])
	})
	add("recompile", 0, true, func(_ runtime.Caller, _ []runtime.Value) (runtime.Value, error) {
		return s.recompileHelper()
	})
	add("tree", 0, true, func(_ runtime.Caller, _ []runtime.Value) (runtime.Value, error) {
		return s.tree()
	})
	add("info", 1, true, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		return s.infoActor(args[0])
	})
	add("top", 1, true, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		return s.top(args[0])
	})
	add("observe", 0, true, func(_ runtime.Caller, _ []runtime.Value) (runtime.Value, error) {
		return s.observe()
	})
	// register — не голая команда: модуль зовёт Repl.register("M").
	add("register", 1, false, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		return s.register(args[0])
	})
	if err := s.vm.SessionRedefine(defs, nil); err != nil {
		panic(err)
	}
}

func (s *Session) extraNames() []string {
	if len(s.extra) == 0 {
		return nil
	}
	out := make([]string, 0, len(s.extra))
	for n := range s.extra {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// ---- h / Doc ----

type helpDoc struct {
	name   string
	module bool
	text   string
	sigs   []clauseSig
	funs   []string
}

type clauseSig struct {
	label string
	text  string
}

func (d *helpDoc) format(pal highlight.Palette) string {
	var b strings.Builder
	if d.module {
		b.WriteString(d.name)
		b.WriteString("\n\n")
		writeDocBody(&b, d.text, pal)
		b.WriteByte('\n')
		for _, f := range d.funs {
			b.WriteString(f)
			b.WriteByte('\n')
		}
		return b.String()
	}
	for i := 0; i < len(d.sigs); {
		label := d.sigs[i].label
		fmt.Fprintf(&b, "%s/%s\n\n", d.name, label)
		for i < len(d.sigs) && d.sigs[i].label == label {
			b.WriteString(d.sigs[i].text)
			b.WriteByte('\n')
			i++
		}
		b.WriteByte('\n')
	}
	writeDocBody(&b, d.text, pal)
	return b.String()
}

func writeDocBody(b *strings.Builder, text string, pal highlight.Palette) {
	if strings.TrimSpace(text) == "" {
		b.WriteString("нет документации\n")
		return
	}
	painted := paintDoc(text, pal)
	b.WriteString(painted)
	if !strings.HasSuffix(painted, "\n") {
		b.WriteByte('\n')
	}
}

// paintDoc прогоняет код внутри ``` через highlight (T-203).
func paintDoc(text string, pal highlight.Palette) string {
	var b strings.Builder
	rest := text
	for {
		i := strings.Index(rest, "```")
		if i < 0 {
			b.WriteString(rest)
			return b.String()
		}
		b.WriteString(rest[:i])
		rest = rest[i+3:]
		j := strings.Index(rest, "```")
		if j < 0 {
			b.WriteString("```")
			b.WriteString(rest)
			return b.String()
		}
		body := rest[:j]
		code := body
		head := ""
		if nl := strings.IndexByte(body, '\n'); nl >= 0 {
			head = body[:nl+1]
			code = body[nl+1:]
		}
		b.WriteString("```")
		b.WriteString(head)
		b.WriteString(highlight.Highlight(strings.TrimRight(code, "\n"), -1, highlight.REPLEnv(), pal))
		if strings.HasSuffix(code, "\n") {
			b.WriteByte('\n')
		}
		b.WriteString("```")
		rest = rest[j+3:]
	}
}

// help — h(f) и h(M). Имя модуля компилятор передаёт атомом с заглавной
// буквы (`h(Map)` → `:Map`), который в исходнике не записать: строка
// `h("Map")` — не модуль.
func (s *Session) help(v runtime.Value) (runtime.Value, error) {
	if v.Kind == runtime.KindAtom {
		d, ok := s.docs[v.Atom]
		if !ok || !d.module {
			return runtime.Unit, raiseType("h", v)
		}
		if err := s.writeOut("%s", d.format(s.pal)); err != nil {
			return runtime.Unit, err
		}
		return runtime.Unit, nil
	}
	name, _, arity, _, ok := fnParts(v)
	if !ok {
		return runtime.Unit, raiseType("h", v)
	}
	if d, ok := s.docs[name]; ok {
		if err := s.writeOut("%s", d.format(s.pal)); err != nil {
			return runtime.Unit, err
		}
		return runtime.Unit, nil
	}
	name = displayName(name)
	label := fmt.Sprintf("%d", arity)
	if arity < 0 {
		label = "*"
	}
	sig := name + "()"
	if arity > 0 {
		sig = fmt.Sprintf("%s/%s", name, label)
	}
	d := &helpDoc{name: name, sigs: []clauseSig{{label: label, text: sig}}}
	if err := s.writeOut("%s", d.format(s.pal)); err != nil {
		return runtime.Unit, err
	}
	return runtime.Unit, nil
}

func (s *Session) writeOut(format string, args ...any) error {
	_, err := fmt.Fprintf(s.out, format, args...)
	return err
}

func fnParts(v runtime.Value) (name string, kind runtime.Kind, arity int, body runtime.Code, ok bool) {
	switch v.Kind {
	case runtime.KindFunction:
		if v.Func == nil {
			return "", 0, 0, nil, false
		}
		var code runtime.Code
		if !v.Func.IsNative {
			code = v.Func.Body
		}
		return v.Func.Name, v.Kind, v.Func.Arity, code, true
	case runtime.KindClosure:
		if v.ClosureVal == nil {
			return "", 0, 0, nil, false
		}
		return v.ClosureVal.Name, v.Kind, v.ClosureVal.Arity, v.ClosureVal.Func, true
	default:
		return "", 0, 0, nil, false
	}
}

func (s *Session) noteLocal(lfd ast.LocalFnDecl, src string, seq int) {
	var lists [][]ast.Pattern
	for _, cl := range lfd.Clauses() {
		lists = append(lists, cl.Params)
	}
	d := &helpDoc{
		name: lfd.FnName(),
		text: docBefore(src, lfd.Pos()),
		sigs: sigsFrom(lfd.FnName(), lists),
	}
	// Только под внутренним именем функции: голое `len` — документация
	// прелюдии, её h(Prelude.len) и печатает.
	s.docs[fmt.Sprintf("__repl__%d$%s", seq, lfd.FnName())] = d
}

// displayName — имя функции, как его ввёл пользователь: без префикса
// инструкции REPL (`__repl__3$sq` → `sq`).
func displayName(name string) string {
	rest, ok := strings.CutPrefix(name, "__repl__")
	if !ok {
		return name
	}
	if i := strings.IndexByte(rest, '$'); i >= 0 {
		return rest[i+1:]
	}
	return name
}

func clauseArity(lists [][]ast.Pattern) int {
	if len(lists) == 0 {
		return 0
	}
	n := len(lists[0])
	if n > 0 {
		if _, ok := lists[0][n-1].(ast.SpreadPattern); ok {
			n--
		}
	}
	return n
}

func sigsFrom(name string, lists [][]ast.Pattern) []clauseSig {
	out := make([]clauseSig, 0, len(lists))
	for _, ps := range lists {
		n := len(ps)
		variadic := false
		if n > 0 {
			if _, ok := ps[n-1].(ast.SpreadPattern); ok {
				variadic = true
				n--
			}
		}
		label := fmt.Sprintf("%d", n)
		if variadic {
			label += ".."
		}
		parts := make([]string, len(ps))
		for i, p := range ps {
			parts[i] = p.String()
		}
		out = append(out, clauseSig{label: label, text: name + "(" + strings.Join(parts, ", ") + ")"})
	}
	return out
}

func docBefore(src string, line int) string {
	lines := strings.Split(src, "\n")
	for _, d := range DocComments(src) {
		i := d.Line - 1
		for i < len(lines) {
			s := strings.TrimLeft(lines[i], " \t")
			if (strings.HasPrefix(s, "##") && !strings.HasPrefix(s, "###")) || strings.TrimSpace(lines[i]) == "" {
				i++
				continue
			}
			break
		}
		if i == line-1 {
			return d.Text
		}
	}
	return ""
}

func (s *Session) indexLoaded(mods []*loader.Module) {
	for _, m := range mods {
		if m == nil || m.Prog == nil {
			continue
		}
		s.indexModule(m)
	}
}

func (s *Session) indexModule(m *loader.Module) {
	src := ""
	if b, err := os.ReadFile(m.Path); err == nil {
		src = string(b)
	}
	if sm := s.mods[m.Name]; sm != nil {
		if sm.arity == nil {
			sm.arity = map[string]int{}
		}
		for _, d := range m.Prog.Decls {
			if fd, ok := d.(ast.FuncDecl); ok {
				sm.arity[fd.FnName()] = fnArity(fd)
			}
		}
		sm.priv = sema.PrivateFns(m.Prog)
	}
	s.indexDocs(m.Name, m.Prog, src, s.deps[m.Name])
}

// indexStdlib — документация встроенных модулей на Brig (T-146) из
// встроенного исходника; как у зависимости, h(M) перечисляет только pub.
func (s *Session) indexStdlib() {
	for _, m := range stdlib.MustModules() {
		s.indexDocs(m.Name, m.Prog, m.Src, true)
	}
}

func fnArity(fd ast.FuncDecl) int {
	var lists [][]ast.Pattern
	for _, cl := range fd.FuncClauses() {
		lists = append(lists, cl.Params)
	}
	return clauseArity(lists)
}

// indexDocs строит h-документацию модуля name из его исходника src.
// dep — зависимость: h(M) перечисляет только pub (§11.4).
func (s *Session) indexDocs(name string, prog *ast.Program, src string, dep bool) {
	modText, fnText := attachDocs(src)
	var funs []string
	for _, d := range prog.Decls {
		fd, ok := d.(ast.FuncDecl)
		if !ok {
			continue
		}
		var lists [][]ast.Pattern
		for _, cl := range fd.FuncClauses() {
			lists = append(lists, cl.Params)
		}
		qualified := name + "." + fd.FnName()
		doc := &helpDoc{
			name: qualified,
			text: fnText[fd.FnName()],
			sigs: sigsFrom(qualified, lists),
		}
		if len(doc.sigs) == 0 {
			continue
		}
		s.docs[qualified] = doc
		// Зависимость: h(M) перечисляет только pub (§11.4).
		if dep && !fd.IsPub() {
			continue
		}
		funs = append(funs, fd.FnName()+"/"+doc.sigs[0].label)
	}
	sort.Strings(funs)
	s.docs[name] = &helpDoc{name: name, module: true, text: modText, funs: funs}
}

func attachDocs(src string) (mod string, fns map[string]string) {
	fns = map[string]string{}
	if src == "" {
		return "", fns
	}
	lines := strings.Split(src, "\n")
	for _, d := range DocComments(src) {
		i := d.Line - 1
		for i < len(lines) {
			s := strings.TrimLeft(lines[i], " \t")
			if (strings.HasPrefix(s, "##") && !strings.HasPrefix(s, "###")) || strings.TrimSpace(lines[i]) == "" {
				i++
				continue
			}
			break
		}
		if i >= len(lines) {
			continue
		}
		s := strings.TrimSpace(lines[i])
		switch {
		case strings.HasPrefix(s, "module "):
			mod = d.Text
		default:
			if name, ok := fnNameAt(s); ok {
				fns[name] = d.Text
			}
		}
	}
	return mod, fns
}

func fnNameAt(s string) (string, bool) {
	s = strings.TrimPrefix(s, "pub ")
	if !strings.HasPrefix(s, "fn ") {
		return "", false
	}
	rest := strings.TrimPrefix(s, "fn ")
	if i := strings.IndexAny(rest, "( \t"); i >= 0 {
		rest = rest[:i]
	}
	return rest, rest != ""
}

// ---- i, v, bindings, flush, time, dis ----

func (s *Session) info(v runtime.Value) error {
	switch v.Kind {
	case runtime.KindList:
		return s.writeOut("List size=%d\n", len(v.List))
	case runtime.KindVector:
		return s.writeOut("Vector size=%d\n", len(v.Vector))
	case runtime.KindMap:
		return s.writeOut("Map size=%d\n", len(v.Map))
	case runtime.KindSet:
		return s.writeOut("Set size=%d\n", len(v.Set))
	case runtime.KindTuple:
		return s.writeOut("Tuple size=%d\n", len(v.Tuple))
	case runtime.KindStr:
		return s.writeOut("Str size=%d\n", utf8.RuneCountInString(v.Str))
	case runtime.KindBytes:
		return s.writeOut("Bytes size=%d\n", len(v.Bytes))
	case runtime.KindRecord:
		names := make([]string, len(v.Record.Fields))
		for i, f := range v.Record.Fields {
			names[i] = f.Name
		}
		return s.writeOut("Record size=%d type=%s fields=%s\n", len(v.Record.Fields), v.Record.Type, strings.Join(names, ", "))
	case runtime.KindPid:
		alive, n := s.vm.Scheduler().ActorInfo(v.Pid)
		status := "dead"
		if alive {
			status = "alive"
		}
		return s.writeOut("Pid %s mailbox=%d\n", status, n)
	default:
		return s.writeOut("%s\n", v.Kind)
	}
}

func (s *Session) value(args []runtime.Value) (runtime.Value, error) {
	if len(args) > 1 {
		return runtime.Unit, &vm.ErrRaise{Val: runtime.Tuple(
			runtime.Atom("function_clause"),
			runtime.List(append([]runtime.Value(nil), args...)...))}
	}
	n := len(s.history)
	if len(args) == 1 {
		if args[0].Kind != runtime.KindInt || !args[0].IsSmall {
			return runtime.Unit, raiseType("v", args[0])
		}
		n = int(args[0].SmallInt)
	}
	if n < 1 || n > len(s.history) {
		return runtime.Unit, &vm.ErrRaise{Val: runtime.Tuple(
			runtime.Atom("no_value"),
			runtime.Int(int64(n)))}
	}
	return s.history[n-1], nil
}

func (s *Session) bindingsMap() runtime.Value {
	ents := make([]runtime.MapEntry, len(s.order))
	for i, name := range s.order {
		ents[i] = runtime.MapEntry{Key: runtime.Str(name), Val: s.env[name]}
	}
	return runtime.Map(ents)
}

func (s *Session) flush() (runtime.Value, error) {
	msgs := s.vm.Scheduler().TakeSessionMail()
	opt := Print{Pal: s.pal, Env: s.HighlightEnv()}
	for _, m := range msgs {
		if err := s.writeOut("%s\n", Format(m, opt)); err != nil {
			return runtime.Unit, err
		}
	}
	return runtime.List(msgs...), nil
}

func (s *Session) timed(f runtime.Value) (runtime.Value, error) {
	if f.Kind != runtime.KindFunction && f.Kind != runtime.KindClosure {
		return runtime.Unit, raiseType("time", f)
	}
	start := time.Now()
	r, err := s.vm.Scheduler().CallNested(f, nil)
	us := time.Since(start).Microseconds()
	if err != nil {
		return runtime.Unit, err
	}
	return runtime.Tuple(runtime.Int(us), r), nil
}

func (s *Session) dis(v runtime.Value) (runtime.Value, error) {
	text, ok := disassemble(v)
	if !ok {
		return runtime.Unit, raiseType("dis", v)
	}
	if err := s.writeOut("%s", text); err != nil {
		return runtime.Unit, err
	}
	if !strings.HasSuffix(text, "\n") {
		if err := s.writeOut("\n"); err != nil {
			return runtime.Unit, err
		}
	}
	return runtime.Unit, nil
}

func disassemble(v runtime.Value) (string, bool) {
	switch v.Kind {
	case runtime.KindFunction:
		if v.Func == nil {
			return "", false
		}
		if v.Func.IsNative || v.Func.Body == nil {
			return nativeLine(v.Func.Name, v.Func.Arity), true
		}
		ch, ok := v.Func.Body.(*vm.Chunk)
		if !ok {
			return "", false
		}
		return (&vm.Function{Name: displayName(v.Func.Name), Arity: v.Func.Arity, Chunk: ch}).Disassemble(), true
	case runtime.KindClosure:
		if v.ClosureVal == nil {
			return "", false
		}
		ch, ok := v.ClosureVal.Func.(*vm.Chunk)
		if !ok {
			return "", false
		}
		return (&vm.Function{Name: displayName(v.ClosureVal.Name), Arity: v.ClosureVal.Arity, Chunk: ch}).Disassemble(), true
	default:
		return "", false
	}
}

func nativeLine(name string, arity int) string {
	if arity < 0 {
		return fmt.Sprintf("native %s/*\n", name)
	}
	return fmt.Sprintf("native %s/%d\n", name, arity)
}

// ---- load ----

func (s *Session) load(v runtime.Value) (runtime.Value, error) {
	if v.Kind != runtime.KindStr {
		return runtime.Unit, raiseType("load", v)
	}
	if err := s.loadPath(v.Str); err != nil {
		return runtime.Unit, err
	}
	return runtime.Unit, nil
}

func (s *Session) loadPath(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return loadErr(path, err)
	}
	mod, err := sourceIsModule(b)
	if err != nil {
		return loadErr(path, err)
	}
	if mod {
		if _, err := s.loadModules(path, true); err != nil {
			return loadErr(path, err)
		}
		return nil
	}
	return s.runScript(path, string(b))
}

// sourceIsModule — первый значимый токен файла это `module` (§11.3).
func sourceIsModule(src []byte) (bool, error) {
	toks, err := lexer.Lex(string(src))
	if err != nil {
		return false, err
	}
	for _, t := range toks {
		if t.Type == lexer.NEWLINE || t.Type == lexer.EOF {
			continue
		}
		return t.Type == lexer.KW_MODULE, nil
	}
	return false, nil
}

func (s *Session) runScript(path, src string) error {
	if err := s.execScript(path, src, true); err != nil {
		if errors.Is(err, vm.ErrInterrupted) {
			return err
		}
		return loadErr(path, err)
	}
	return nil
}

// execScript исполняет script-файл как вводы сессии. here — вызов с
// горутины актора (хелпер load). wrap не используется здесь: ошибку
// оборачивает runScript, а загрузка CLI печатает её как есть.
func (s *Session) execScript(path, src string, here bool) error {
	lines, err := parser.ParseReplInput(src)
	if err != nil {
		if here {
			return err
		}
		return s.present(path, src, err)
	}
	prev := s.diagFile
	s.diagFile = path
	defer func() { s.diagFile = prev }()
	for _, line := range lines {
		var res Result
		if here {
			res, err = s.evalLineHere(src, line)
		} else {
			res, err = s.evalLine(src, line)
		}
		if errors.Is(err, vm.ErrInterrupted) {
			return err
		}
		if err != nil {
			if here {
				return err
			}
			return s.present(path, src, err)
		}
		if res.Value.Kind != runtime.KindUnit {
			s.history = append(s.history, res.Value)
		}
	}
	return nil
}

func loadErr(path string, err error) error {
	return &vm.ErrRaise{Val: runtime.Tuple(
		runtime.Atom("load_error"),
		runtime.Tuple(runtime.Str(path), runtime.Str(err.Error())))}
}

// ---- recompile ----

// recompileHelper — recompile() (§11.4, семантика — T-208 #246): печатает
// перекомпилированные модули. Ошибка — :load_error по образцу load, старый
// код остаётся (её уже напечатал report в формате E.1).
func (s *Session) recompileHelper() (runtime.Value, error) {
	mods, err := s.recompile(true)
	if err != nil {
		return runtime.Unit, loadErr(errPath(err), err)
	}
	if len(mods) == 0 {
		return runtime.Unit, s.writeOut("нет изменений\n")
	}
	return runtime.Unit, s.writeOut("перекомпилировано: %s\n", strings.Join(mods, ", "))
}

// errPath — файл, в котором ошибка загрузки или компиляции; "" — неизвестен.
func errPath(err error) string {
	var le *loader.Error
	var ce *compiler.Error
	var pe *fs.PathError
	switch {
	case errors.As(err, &le):
		return le.File
	case errors.As(err, &ce):
		return ce.File
	case errors.As(err, &pe):
		return pe.Path
	}
	return ""
}

// ---- регистрация хелперов фреймворка ----

// RegisterHelpers делает pub-функции загруженного модуля module голыми
// именами сессии — так же, как функции Repl. Из кода модуля то же делает
// Repl.register(name): `Calmar.console()` зовёт его до первого ввода.
func (s *Session) RegisterHelpers(module string) error {
	return s.vm.Scheduler().Sync(func() error {
		return s.registerNow(module)
	})
}

func (s *Session) register(v runtime.Value) (runtime.Value, error) {
	if v.Kind != runtime.KindStr {
		return runtime.Unit, raiseType("register", v)
	}
	// Натив исполняет цикл сессии: регистрация — сразу, без Sync.
	if err := s.registerNow(v.Str); err != nil {
		return runtime.Unit, loadErr(v.Str, err)
	}
	return runtime.Unit, nil
}

func (s *Session) registerNow(module string) error {
	m := s.mods[module]
	if m == nil {
		return fmt.Errorf("module %s is not loaded", module)
	}
	if s.extra == nil {
		s.extra = map[string]bool{}
	}
	for _, g := range m.globals {
		if strings.Contains(g, "$") {
			continue
		}
		bare := strings.TrimPrefix(g, module+".")
		if bare == g || strings.Contains(bare, ".") || m.priv[bare] {
			continue
		}
		cur := s.vm.Global(bare)
		if cur.Kind != runtime.KindUnit && !s.extra[bare] {
			continue
		}
		v := s.vm.Global(g)
		if v.Kind != runtime.KindFunction && v.Kind != runtime.KindClosure {
			continue
		}
		s.vm.DefineGlobal(bare, v)
		s.extra[bare] = true
		if d, ok := s.docs[g]; ok {
			s.docs[bare] = d
		}
	}
	return nil
}

func raiseType(op string, v runtime.Value) error {
	return &vm.ErrRaise{Val: runtime.Tuple(
		runtime.Atom("type_error"),
		runtime.Tuple(runtime.Atom(op), v))}
}

// initDocs строит документацию встроенных функций. Имена и арности — из
// сигнатур sema (sema.BuiltinArities), здесь только имена параметров:
// builtinParams["Json.encode/2"] = "v, opts". Функция без параметров в
// таблице или лишняя строка таблицы — паника при старте сессии.
func (s *Session) initDocs() {
	s.docs = map[string]*helpDoc{}
	used := map[string]bool{}
	sigs := func(qualified, key string, labels []string) []clauseSig {
		out := make([]clauseSig, len(labels))
		for i, l := range labels {
			k := key + "/" + l
			ps, ok := builtinParams[k]
			if !ok {
				panic("repl: no doc params for " + k)
			}
			used[k] = true
			out[i] = clauseSig{label: l, text: qualified + "(" + ps + ")"}
		}
		return out
	}
	all := sema.BuiltinArities()
	for mod, fns := range all {
		var funs []string
		for name, labels := range fns {
			key := name
			if mod != "" {
				key = mod + "." + name
			}
			s.docs[key] = &helpDoc{name: key, sigs: sigs(key, key, labels)}
			for _, l := range labels {
				funs = append(funs, name+"/"+l)
			}
		}
		if mod != "" {
			sort.Strings(funs)
			s.docs[mod] = &helpDoc{name: mod, module: true, funs: funs}
		}
	}
	// Голые хелперы — те же параметры, что у Repl.*.
	for _, name := range sema.ReplHelperNames() {
		s.docs[name] = &helpDoc{name: name, sigs: sigs(name, "Repl."+name, all["Repl"][name])}
	}
	observeHelp := "Полноэкранный вид на TTY: дерево акторов, ящик, счётчики и лента последних [:vm, :actor, :crash] и [:vm, :actor, :down]. Обновление раз в секунду. q, Esc и Ctrl-C возвращают в REPL; Enter показывает Actor.info. Без TTY печатает один кадр и возвращает (). Если observe уже открыт — «observe уже открыт» и ()."
	if d := s.docs["observe"]; d != nil {
		d.text = observeHelp
	}
	if d := s.docs["Repl.observe"]; d != nil {
		d.text = observeHelp
	}
	for k := range builtinParams {
		if !used[k] {
			panic("repl: doc params for unknown builtin " + k)
		}
	}
}

// builtinParams — имена параметров встроенных функций по "имя/арность".
var builtinParams = map[string]string{
	"map/2": "xs, f", "filter/2": "xs, p", "find/2": "xs, p",
	"all/2": "xs, p", "any/2": "xs, p", "fold/3": "xs, acc, f",
	"len/1": "v", "list/0..": "..xs", "set/0..": "..xs",
	"to_str/1": "v", "to_int/1": "v", "to_float/1": "v",
	"send/2": "pid, msg", "spawn/1": "f", "spawn_linked/1": "f",
	"spawn_watched/1": "f", "spawn/2": "f, limits", "spawn_linked/2": "f, limits",
	"spawn_watched/2": "f, limits", "exit/2": "pid, reason",
	"register/2": "name, pid", "unregister/1": "name", "whereis/1": "name",
	"await/2": "ref, timeout", "reply/3": "pid, ref, value",
	"link/1": "pid", "watch/1": "pid", "unwatch/1": "ref",
	"self/0": "", "make_ref/0": "", "mailbox_size/0": "", "mailbox_size/1": "pid",
	"print/0..": "..vs", "eprint/0..": "..vs", "log/0..": "..vs",
	"assert/1": "x", "raise/1": "e",
	"Some/1": "v", "Ok/1": "v", "Error/1": "e",

	"Vec.push/2": "v, x", "Vec.set/3": "v, i, x", "Vec.get/2": "v, i", "Vec.len/1": "v",
	"Map.put/3": "m, k, v", "Map.get/2": "m, k", "Map.remove/2": "m, k", "Map.keys/1": "m",
	"Record.to_anon/1": "r",
	"Str.to_bytes/1":   "s", "Bytes.to_str/1": "b",
	"Str.split/2": "s, sep", "Str.join/2": "xs, sep", "Str.trim/1": "s",
	"Str.find/2": "s, sub", "Str.replace/3": "s, old, new",
	"Str.starts_with?/2": "s, prefix", "Str.ends_with?/2": "s, suffix",
	"Str.lower/1": "s", "Str.upper/1": "s", "Str.slice/3": "s, start, end",
	"Str.to_int/1":  "s",
	"Bytes.slice/3": "b, start, end", "Bytes.find/2": "b, sub", "Bytes.split/2": "b, sep",
	"Bytes.concat/2": "a, b", "Bytes.at/2": "b, i",
	"Json.encode/1": "v", "Json.encode/2": "v, opts", "Json.decode/1": "s",
	"Test.describe/1": "name", "Test.it/2": "name, thunk", "Test.run/0": "",
	"Test.assert_eq/2": "a, b", "Test.assert_ne/2": "a, b", "Test.assert/1": "x", "Test.fail/1": "msg",
	"Sys.args/0": "", "Sys.halt/1": "code",
	"Actor.info/1": "pid", "Actor.list/0": "",
	"Global.put/2": "name, value", "Global.get/1": "name",
	"Timer.send_after/3": "ms, pid, msg", "Timer.cancel/1": "ref",
	"Time.monotonic_ms/0": "", "Time.now/0": "",
	"Telemetry.attach/3": "id, prefix, handler", "Telemetry.detach/1": "id",
	"Telemetry.emit/3": "event, measurements, meta",
	"Port.close/1":     "port", "Port.request/1": "port", "Port.write/2": "port, data",
	"Port.give/2": "port, pid", "Signal.subscribe/1": "names", "File.open/2": "path, mode",
	"HttpServer.listen/1": "addr", "HttpServer.respond/3": "req, status, headers",

	"Repl.h/1": "f", "Repl.i/1": "v", "Repl.v/0": "", "Repl.v/1": "n",
	"Repl.load/1": "path", "Repl.flush/0": "", "Repl.time/1": "f", "Repl.dis/1": "f",
	"Repl.bindings/0": "", "Repl.reset/0": "", "Repl.recompile/0": "", "Repl.register/1": "module",
	"Repl.tree/0": "", "Repl.info/1": "pid", "Repl.top/1": "n", "Repl.observe/0": "",
}
