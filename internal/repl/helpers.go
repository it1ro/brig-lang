package repl

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/it1ro/brig-lang/internal/ast"
	"github.com/it1ro/brig-lang/internal/highlight"
	"github.com/it1ro/brig-lang/internal/lexer"
	"github.com/it1ro/brig-lang/internal/loader"
	"github.com/it1ro/brig-lang/internal/parser"
	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/vm"
)

// Хелперы консоли (§11.4) — нативы модуля Repl. В сессии они стоят и
// голыми именами, и как Repl.h. Вне сессии глобалов нет: brig check
// файла с h(x) — undefined function h/1. recompile() — T-210.

func (s *Session) installHelpers() {
	s.initDocs()
	defs := map[string]runtime.Value{}
	add := func(name string, arity int, bare bool, fn runtime.NativeFunc) {
		fv := &runtime.FuncValue{Name: "Repl." + name, Arity: arity, IsNative: true, Native: fn}
		v := runtime.Func(fv)
		defs["Repl."+name] = v
		if bare {
			defs[name] = v
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

func (s *Session) help(v runtime.Value) (runtime.Value, error) {
	if v.Kind == runtime.KindStr {
		d, ok := s.docs[v.Str]
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
	s.docs[lfd.FnName()] = d
	s.docs[fmt.Sprintf("__repl__%d$%s", seq, lfd.FnName())] = d
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
	modText, fnText := attachDocs(src)
	var funs []string
	for _, d := range m.Prog.Decls {
		fd, ok := d.(ast.FuncDecl)
		if !ok {
			continue
		}
		var lists [][]ast.Pattern
		for _, cl := range fd.FuncClauses() {
			lists = append(lists, cl.Params)
		}
		qualified := m.Name + "." + fd.FnName()
		doc := &helpDoc{
			name: qualified,
			text: fnText[fd.FnName()],
			sigs: sigsFrom(qualified, lists),
		}
		if len(doc.sigs) == 0 {
			continue
		}
		s.docs[qualified] = doc
		funs = append(funs, fd.FnName()+"/"+doc.sigs[0].label)
	}
	sort.Strings(funs)
	s.docs[m.Name] = &helpDoc{name: m.Name, module: true, text: modText, funs: funs}
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
		return (&vm.Function{Name: v.Func.Name, Arity: v.Func.Arity, Chunk: ch}).Disassemble(), true
	case runtime.KindClosure:
		if v.ClosureVal == nil {
			return "", false
		}
		ch, ok := v.ClosureVal.Func.(*vm.Chunk)
		if !ok {
			return "", false
		}
		return (&vm.Function{Name: v.ClosureVal.Name, Arity: v.ClosureVal.Arity, Chunk: ch}).Disassemble(), true
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
	lines, err := parser.ParseReplInput(src)
	if err != nil {
		return loadErr(path, err)
	}
	prev := s.diagFile
	s.diagFile = path
	defer func() { s.diagFile = prev }()
	for _, line := range lines {
		res, err := s.evalLineHere(src, line)
		if errors.Is(err, vm.ErrInterrupted) {
			return err
		}
		if err != nil {
			return loadErr(path, err)
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

// ---- регистрация хелперов фреймворка ----

// RegisterHelpers делает pub-функции загруженного модуля module голыми
// именами сессии — так же, как функции Repl. Из кода модуля то же делает
// Repl.register(name): `Calmar.console()` зовёт его до первого ввода.
// pub в AST пока не отмечен: перечисляются все fn модуля.
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
		if bare == g || strings.Contains(bare, ".") {
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

func (s *Session) initDocs() {
	s.docs = map[string]*helpDoc{}
	fn := func(name string, sigs ...clauseSig) {
		s.docs[name] = &helpDoc{name: name, sigs: sigs}
	}
	one := func(name, label, text string) {
		fn(name, clauseSig{label: label, text: text})
	}
	one("map", "2", "map(xs, f)")
	one("filter", "2", "filter(xs, p)")
	one("find", "2", "find(xs, p)")
	one("all", "2", "all(xs, p)")
	one("any", "2", "any(xs, p)")
	one("fold", "3", "fold(xs, acc, f)")
	one("len", "1", "len(v)")
	one("list", "0..", "list(..xs)")
	one("set", "0..", "set(..xs)")
	one("to_str", "1", "to_str(v)")
	one("to_int", "1", "to_int(v)")
	one("to_float", "1", "to_float(v)")
	one("send", "2", "send(pid, msg)")
	one("spawn", "1", "spawn(f)")
	one("spawn_linked", "1", "spawn_linked(f)")
	one("link", "1", "link(pid)")
	one("watch", "1", "watch(pid)")
	one("unwatch", "1", "unwatch(ref)")
	one("self", "0", "self()")
	one("make_ref", "0", "make_ref()")
	fn("mailbox_size",
		clauseSig{label: "0", text: "mailbox_size()"},
		clauseSig{label: "1", text: "mailbox_size(pid)"})
	one("print", "0..", "print(..vs)")
	one("eprint", "0..", "eprint(..vs)")
	one("log", "0..", "log(..vs)")
	one("assert", "1", "assert(x)")
	one("raise", "1", "raise(e)")
	one("Some", "1", "Some(v)")
	one("Ok", "1", "Ok(v)")
	one("Error", "1", "Error(e)")

	mod := func(name string, members ...clauseSig) {
		funs := make([]string, len(members))
		for i, m := range members {
			sig := name + "." + m.text
			s.docs[name+"."+fnBare(m.text)] = &helpDoc{
				name: name + "." + fnBare(m.text),
				sigs: []clauseSig{{label: m.label, text: sig}},
			}
			funs[i] = fnBare(m.text) + "/" + m.label
		}
		sort.Strings(funs)
		s.docs[name] = &helpDoc{name: name, module: true, funs: funs}
	}
	mod("Vec",
		clauseSig{"2", "push(v, x)"},
		clauseSig{"3", "set(v, i, x)"},
		clauseSig{"2", "get(v, i)"},
		clauseSig{"1", "len(v)"})
	mod("Map",
		clauseSig{"3", "put(m, k, v)"},
		clauseSig{"2", "get(m, k)"},
		clauseSig{"2", "remove(m, k)"},
		clauseSig{"1", "keys(m)"})
	mod("Str", clauseSig{"1", "to_bytes(s)"})
	mod("Bytes", clauseSig{"1", "to_str(b)"})
	s.docs["Json.encode"] = &helpDoc{name: "Json.encode", sigs: []clauseSig{
		{label: "1", text: "Json.encode(v)"},
		{label: "2", text: "Json.encode(v, opts)"},
	}}
	one("Json.decode", "1", "Json.decode(s)")
	s.docs["Json"] = &helpDoc{name: "Json", module: true, funs: []string{"decode/1", "encode/1"}}
	mod("Test",
		clauseSig{"1", "describe(name)"},
		clauseSig{"2", "it(name, thunk)"},
		clauseSig{"0", "run()"},
		clauseSig{"2", "assert_eq(a, b)"},
		clauseSig{"2", "assert_ne(a, b)"},
		clauseSig{"1", "assert(x)"},
		clauseSig{"1", "fail(msg)"})
	mod("Sys", clauseSig{"0", "args()"})
	mod("Repl",
		clauseSig{"1", "h(f)"},
		clauseSig{"1", "i(v)"},
		clauseSig{"0", "v()"},
		clauseSig{"1", "load(path)"},
		clauseSig{"0", "flush()"},
		clauseSig{"1", "time(f)"},
		clauseSig{"1", "dis(f)"},
		clauseSig{"0", "bindings()"},
		clauseSig{"0", "reset()"})
	// v — две арности; mod записал бы одну. Поправить.
	s.docs["Repl.v"] = &helpDoc{name: "Repl.v", sigs: []clauseSig{
		{label: "0", text: "Repl.v()"},
		{label: "1", text: "Repl.v(n)"},
	}}
	s.docs["v"] = &helpDoc{name: "v", sigs: []clauseSig{
		{label: "0", text: "v()"},
		{label: "1", text: "v(n)"},
	}}
	// Голые имена хелперов — те же тексты, что Repl.*, но без префикса.
	for _, n := range []string{"h", "i", "load", "flush", "time", "dis", "bindings", "reset"} {
		src := s.docs["Repl."+n]
		bare := *src
		bare.name = n
		bare.sigs = append([]clauseSig(nil), src.sigs...)
		for i := range bare.sigs {
			bare.sigs[i].text = strings.TrimPrefix(bare.sigs[i].text, "Repl.")
		}
		s.docs[n] = &bare
	}
	funs := []string{"bindings/0", "dis/1", "flush/0", "h/1", "i/1", "load/1", "reset/0", "time/1", "v/0", "v/1"}
	s.docs["Repl"].funs = funs
}

func fnBare(sig string) string {
	i := strings.IndexByte(sig, '(')
	if i < 0 {
		return sig
	}
	return sig[:i]
}
