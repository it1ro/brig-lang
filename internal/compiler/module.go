package compiler

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/it1ro/brig-lang/internal/ast"
	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/vm"
	"github.com/it1ro/brig-lang/stdlib"
)

// Compile компилирует программу из одного модуля.
func (c *Compiler) Compile(prog *ast.Program) (*ProgramImage, error) {
	return c.CompileProgram([]Module{{Name: prog.Module, Prog: prog}})
}

// CompileProgram компилирует программу из нескольких модулей (§11.1,
// T-137); mods[0] — входной модуль. Сначала собираются декларации всех
// модулей (fn, типы, конструкторы, локальные имена модулей), затем тела
// функций: порядок деклараций и модулей свободный, циклы импорта
// допустимы. Функции входного модуля в образе — под своими именами,
// остальных — `Модуль.f`; Main — `main` входного модуля.
func (c *Compiler) CompileProgram(mods []Module) (image *ProgramImage, err error) {
	c.image = &ProgramImage{Functions: make(map[string]*vm.Function)}
	c.lifted = make(map[string]*liftedFn)
	c.entry, c.cur = nil, nil
	defer func() {
		if r := recover(); r != nil {
			if ce, ok := r.(compileError); ok {
				image = nil
				err = &Error{File: c.cur.path, Line: int(ce.pos.Line), Col: int(ce.pos.Col), Msg: ce.msg}
				return
			}
			panic(r)
		}
	}()

	all := c.declareProgram(mods)

	for i, m := range mods {
		c.cur = all[i]
		for _, d := range m.Prog.Decls {
			fd, ok := d.(ast.FuncDecl)
			if !ok {
				continue
			}
			clauses := fd.FuncClauses()
			if len(clauses) == 0 {
				continue
			}
			name := c.cur.prefix + fd.FnName()
			fn, cerr := c.compileNamedFn(name, clauses)
			if cerr != nil {
				return nil, c.cur.fileErr(wrapCtx(posOf(fd), cerr, "fn %s", fd.FnName()))
			}
			c.image.Functions[name] = fn
			if c.cur == c.entry && fd.FnName() == "main" {
				c.image.Main = fn
			}
		}
	}

	c.cur = c.entry
	if prog := mods[0].Prog; len(prog.Stmts) > 0 {
		fn, cerr := c.compileBlock("__repl__", nil, prog.Stmts)
		if cerr != nil {
			return nil, c.cur.fileErr(cerr)
		}
		c.image.Functions["__repl__"] = fn
		c.image.Main = fn
	}

	if Verify {
		if verr := verifyImage(c.image); verr != nil {
			return nil, verr
		}
	}
	return c.image, nil
}

// declareProgram — первый проход CompileProgram: модули программы и их
// декларации (fn, типы, конструкторы, локальные имена модулей); mods[0] —
// входной. Текущим остаётся входной модуль.
func (c *Compiler) declareProgram(mods []Module) []*module {
	c.mods = make(map[string]*module, len(mods))
	all := make([]*module, len(mods))
	for i, m := range mods {
		prefix := ""
		if i > 0 {
			prefix = m.Name + "."
		}
		all[i] = newModule(m.Name, m.Path, prefix)
		c.mods[m.Name] = all[i]
	}
	c.entry = all[0]
	for i, m := range mods {
		c.cur = all[i]
		c.declareModule(m.Prog)
	}
	c.cur = c.entry
	return all
}

// DeclareProgram собирает декларации программы mods, как CompileProgram,
// но тела функций не компилирует: следующие CompileReplLine видят типы
// записей, конструкторы вариантов и fn входного модуля mods[0] голыми
// именами, а его import — как в файле (доктест, T-245). Функции
// программы в ВМ ставит вызывающий — из образа CompileProgram тех же mods.
func (c *Compiler) DeclareProgram(mods []Module) (err error) {
	if len(mods) == 0 {
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			if ce, ok := r.(compileError); ok {
				err = &Error{File: c.cur.path, Line: int(ce.pos.Line), Col: int(ce.pos.Col), Msg: ce.msg}
				return
			}
			panic(r)
		}
	}()
	c.declareProgram(mods)
	return nil
}

// fileErr приписывает ошибке компиляции путь модуля m.
func (m *module) fileErr(err error) error {
	var ce *Error
	if errors.As(err, &ce) {
		out := *ce
		out.File = m.path
		return &out
	}
	return &Error{File: m.path, Msg: err.Error()}
}

// declareModule собирает декларации текущего модуля: локальные имена
// модулей (§11.1), записи и конструкторы (до функций: порядок
// деклараций свободный, §11.2), имена fn.
func (c *Compiler) declareModule(prog *ast.Program) {
	m := c.cur
	for _, d := range prog.Decls {
		switch d := d.(type) {
		case ast.ImportDecl:
			full := d.ImportedModule()
			m.addLocal(full, full, d)
			m.addLocal(full[strings.LastIndex(full, ".")+1:], full, d)
		case ast.AliasDecl:
			full := d.AliasOriginal()
			m.addLocal(d.AliasName(), full, d)
			m.addLocal(full, full, d)
		case ast.TypeDecl:
			if fields, ok := d.RecordFields(); ok {
				m.records[d.TypeName()] = fields
			}
			if vs, ok := d.Variants(); ok {
				c.declareCtors(d, vs)
			}
		case ast.FuncDecl:
			m.fns[d.FnName()] = true
		}
	}
}

// addLocal связывает локальное имя модуля name с модулем full. Одно
// локальное имя для двух разных модулей — ошибка компиляции (§11.1):
// развести — через alias.
func (m *module) addLocal(name, full string, at ast.Node) {
	if prev, ok := m.locals[name]; ok && prev != full {
		panic(compileError{pos: posOf(at), msg: fmt.Sprintf(
			"module name %s already refers to %s", name, prev)})
	}
	m.locals[name] = full
}

// modRef — модуль, на который ссылается локальное имя: встроенный
// (builtin — его имя) или пользовательский (full; mod == nil, если
// модуля нет среди компилируемых — Compile одного файла).
type modRef struct {
	builtin string
	full    string
	mod     *module
}

// resolveModule разрешает локальное имя модуля в текущем модуле:
// сначала import/alias файла, затем встроенные модули (доступны без
// import, §11.1).
func (c *Compiler) resolveModule(name string) (modRef, bool) {
	full, ok := c.cur.locals[name]
	if !ok {
		if !isBuiltinModule(name) {
			return modRef{}, false
		}
		full = name
	}
	// Go-нативный модуль — всегда глобалы ВМ; модуль stdlib на Brig —
	// тоже, если он не компилируется вместе с программой (его функции
	// ставит InstallStdlib под теми же именами `M.f`).
	if isNativeModule(full) || (stdlib.IsModule(full) && c.mods[full] == nil) {
		return modRef{builtin: full, full: full}, true
	}
	return modRef{full: full, mod: c.mods[full]}, true
}

// funcModule — модуль функции `name.f` в вызове или ссылке-значении.
// Модуль программы без import/alias не виден (§11.1). Неизвестный
// модуль — глобал `name.f` (known == false): REPL и прямой Compile
// проход имён не гоняют, и имя падает в рантайме (`undefined: Mod.f`).
func (fc *funcCompiler) funcModule(name string, at vm.SrcPos) (ref modRef, known bool, err error) {
	ref, known = fc.compiler.resolveModule(name)
	if known {
		return ref, true, nil
	}
	if fc.compiler.mods[name] != nil {
		return modRef{}, false, errAt(at, "module %s is not imported", name)
	}
	return modRef{full: name}, false, nil
}

// isActorPrimitive — акторный примитив прелюдии: опкод, а не глобал
// (compileActorCall), значением не бывает.
func isActorPrimitive(name string) bool {
	switch name {
	case "send", "spawn", "spawn_linked", "spawn_watched", "exit", "self",
		"make_ref", "watch", "link", "unwatch", "mailbox_size",
		"register", "unregister", "whereis", "await", "reply":
		return true
	}
	return false
}

// isModule сообщает, что name — локальное имя модуля в текущем модуле.
func (c *Compiler) isModule(name string) bool {
	_, ok := c.resolveModule(name)
	return ok
}

// global — глобальное имя функции fn модуля r.
func (r modRef) global(fn string) string {
	switch {
	case r.builtin != "":
		return r.builtin + "." + fn
	case r.mod != nil:
		return r.mod.prefix + fn
	}
	return r.full + "." + fn
}

// declareCtors регистрирует конструкторы вариант-декларации (§14.2).
// Один тег в двух декларациях модуля — ошибка компиляции: ссылка
// неоднозначна.
func (c *Compiler) declareCtors(td ast.TypeDecl, vs []ast.VariantArg) {
	m := c.cur
	typ := m.prefix + td.TypeName()
	for ord, v := range vs {
		if prev, dup := m.ctors[v.Name]; dup {
			pos := posOf(td)
			panic(compileError{pos: pos, msg: fmt.Sprintf(
				"constructor %s already declared in type %s", v.Name, prev.typ)})
		}
		val := runtime.UserVariant(typ, ord, v.Name)
		if n := len(v.Fields); n > 0 {
			tag, ord := v.Name, ord
			// Имя с префиксом модуля: равенство Function — по имени (T-144),
			// одноимённые конструкторы двух модулей различны.
			val = runtime.Func(&runtime.FuncValue{
				Name: m.prefix + tag, Arity: n, IsNative: true,
				Native: func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
					return runtime.UserVariant(typ, ord, tag, append([]runtime.Value(nil), args...)...), nil
				},
			})
		}
		m.ctors[v.Name] = userCtor{typ: typ, val: val}
	}
}

// verifyImage прогоняет vm.Verify по всем функциям модуля в
// детерминированном порядке имён.
func verifyImage(img *ProgramImage) error {
	names := make([]string, 0, len(img.Functions))
	for name := range img.Functions {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := vm.Verify(img.Functions[name].Chunk); err != nil {
			return fmt.Errorf("verify %s: %w", name, err)
		}
	}
	return nil
}

// CompileReplLine компилирует одну инструкцию REPL как функцию от видимых
// имён. Функция возвращает значение нового связывания newName: у
// `name = expr` — значение expr, у локальной fn — её значение-функцию.
// seq различает инструкции сессии: глобальные имена вложенных fn
// (`__repl__<seq>$…`) у разных инструкций не совпадают, и переопределение
// fn не подменяет её у ранее созданных замыканий (лексический снимок).
func (c *Compiler) CompileReplLine(seq int, names []string, s ast.Stmt) (fn *vm.Function, newName string, err error) {
	defer func() {
		if r := recover(); r != nil {
			if ce, ok := r.(compileError); ok {
				fn = nil
				newName = ""
				err = ce
				return
			}
			panic(r)
		}
	}()
	c.replHelpers = true
	defer func() { c.replHelpers = false }()

	c.image = &ProgramImage{Functions: make(map[string]*vm.Function)}
	c.lifted = make(map[string]*liftedFn)

	fc := c.newFuncCompiler(nil)
	fc.prefix = fmt.Sprintf("__repl__%d$", seq)
	fc.chunk.NumParams = len(names)

	// Локальная fn затеняет одноимённое имя сессии и в своём теле:
	// рекурсивный вызов идёт в новую fn, а не в прежнее значение.
	var fnName string
	if lfd, ok := s.(ast.LocalFnDecl); ok {
		fnName = lfd.FnName()
	}
	for i, n := range names {
		r := fc.allocReg()
		if r != i {
			return nil, "", fmt.Errorf("internal: repl param %d in r%d", i, r)
		}
		if n != fnName {
			fc.bindLocal(n, i)
		}
	}

	switch st := s.(type) {
	case ast.LetBind:
		ip, ok := st.Pat().(ast.IdentPattern)
		if !ok {
			return nil, "", fmt.Errorf("repl: only simple `name = expr` bindings are supported")
		}
		newName = ip.IdentName()
		r := fc.allocReg()
		if err := fc.compileExpr(st.Val(), val(r)); err != nil {
			return nil, "", err
		}
		fc.emit(vm.ABC(vm.RETURN, r, 0, 0))
	case ast.LocalFnDecl:
		newName = st.FnName()
		if err := fc.declareLocalFns([]ast.Stmt{st}); err != nil {
			return nil, "", err
		}
		if err := fc.compileLocalFn(st, discard); err != nil {
			return nil, "", err
		}
		r := fc.allocReg()
		if err := fc.compileVar(newName, val(r)); err != nil {
			return nil, "", err
		}
		fc.emit(vm.ABC(vm.RETURN, r, 0, 0))
	case ast.ExprStmt:
		scratch := fc.allocReg()
		if err := fc.compileExpr(st.ExprValue(), dest{reg: scratch, tail: true}); err != nil {
			return nil, "", err
		}
	default:
		return nil, "", fmt.Errorf("repl: unsupported statement %T", s)
	}

	fc.chunk.NumRegs = fc.maxReg
	fn = &vm.Function{Name: "__repl__", Arity: len(names), Chunk: fc.chunk}
	if Verify {
		if verr := vm.Verify(fn.Chunk); verr != nil {
			return nil, "", fmt.Errorf("verify __repl__: %w", verr)
		}
	}
	return fn, newName, nil
}

// ---- statements ----

// modulePath разбирает выражение-путь `A.B.x` (переменная с заглавной
// буквы и цепочка членов; у pipe — одно имя с точками) на сегменты.
// ok == false — не путь модуля (например, `rec.field`).
func modulePath(e ast.Expr) (segs []string, ok bool) {
	switch x := e.(type) {
	case ast.VariableExpr:
		segs = strings.Split(x.Name(), ".")
		return segs, isUpperName(segs[0])
	case ast.MemberExpr:
		segs, ok = modulePath(x.Obj())
		if !ok {
			return nil, false
		}
		return append(segs, x.MemberName()), true
	}
	return nil, false
}

func isUpperName(s string) bool { return s != "" && s[0] >= 'A' && s[0] <= 'Z' }

// errAt — ошибка компиляции в позиции p.
func errAt(p vm.SrcPos, format string, args ...any) error {
	return &Error{Line: int(p.Line), Col: int(p.Col), Msg: fmt.Sprintf(format, args...)}
}

// resolvePath разрешает путь `Mod.x`: всё до последнего сегмента —
// локальное имя модуля. Неизвестный модуль — ошибка в позиции пути at.
func (fc *funcCompiler) resolvePath(segs []string, at vm.SrcPos) (modRef, string, error) {
	name, member := splitPath(segs)
	ref, ok := fc.compiler.resolveModule(name)
	if !ok {
		return modRef{}, "", errAt(at, "unknown module %s", name)
	}
	return ref, member, nil
}

// splitPath делит путь на имя модуля и член: `A.B.f` → `A.B`, `f`.
func splitPath(segs []string) (mod, member string) {
	return strings.Join(segs[:len(segs)-1], "."), segs[len(segs)-1]
}

// pathStart — позиция начала пути (первого сегмента).
func pathStart(e ast.Expr) vm.SrcPos {
	for {
		me, ok := e.(ast.MemberExpr)
		if !ok {
			return posOf(e)
		}
		e = me.Obj()
	}
}

// moduleCtor — конструктор tag пользовательского модуля ref.
func moduleCtor(ref modRef, tag string, at vm.SrcPos) (userCtor, error) {
	if ref.mod != nil {
		if ct, ok := ref.mod.ctors[tag]; ok {
			return ct, nil
		}
	}
	return userCtor{}, errAt(at, "unknown constructor %s in module %s", tag, ref.full)
}

// compileModulePath — значение пути `Mod.x` вне позиции вызова:
// функция модуля (§7.6, T-144) — её глобал, как у вызова `Mod.f(…)`;
// иначе конструктор пользовательского модуля.
func (fc *funcCompiler) compileModulePath(segs []string, at vm.SrcPos, d dest) error {
	if name, member := splitPath(segs); !isUpperName(member) {
		ref, _, err := fc.funcModule(name, at)
		if err != nil {
			return err
		}
		if ref.builtin == "Prelude" && isActorPrimitive(member) {
			return errAt(at, "actor primitive %s.%s cannot be used as a value (§12.6)", name, member)
		}
		return fc.loadGlobal(d, ref.global(member))
	}
	ref, member, err := fc.resolvePath(segs, at)
	if err != nil {
		return err
	}
	// У встроенного модуля (ref.mod == nil) конструкторов нет: sema ловит
	// `Vec.Foo` раньше (T-231), здесь — defensive-ветка для путей мимо sema.
	ct, err := moduleCtor(ref, member, at)
	if err != nil {
		return err
	}
	return fc.loadConst(ct.val, d)
}
