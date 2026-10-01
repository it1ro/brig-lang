package compiler

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/it1ro/brig-lang/internal/ast"
	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/vm"
)

// recordSlot — элемент литерала записи: поле `name: val` или спред
// `..val` (name == "..").
type recordSlot struct {
	name string
	val  ast.Expr
}

// compileRecord — литерал записи `Type{ f: e, ..r }` (typ != "") или
// анонимный `{ ... }`. Неизвестный тип, поле вне декларации и повторное
// явное поле — ошибка компиляции с позицией. Форма записи (тип,
// объявленные поля, слоты) — константа в R[base], значения слотов — в
// R[base+1..]; RECORD собирает запись.
func (fc *funcCompiler) compileRecord(typ string, call ast.CallExpr, d dest) error {
	var declared []string
	rtType := ""
	if typ != "" {
		var err error
		rtType, declared, err = fc.recordType(typ, posOf(call.Callee()))
		if err != nil {
			return err
		}
	}

	slots := make([]recordSlot, 0, len(call.Args()))
	seen := make(map[string]bool)
	for _, a := range call.Args() {
		if u, ok := a.(ast.UnaryExpr); ok && u.OpStr() == ".." {
			slots = append(slots, recordSlot{"..", u.Operand()})
			continue
		}
		b, ok := a.(ast.BinaryExpr)
		if !ok || b.OpStr() != ":" {
			return fmt.Errorf("internal: элемент литерала записи %T", a)
		}
		fv, ok := b.Left().(ast.VariableExpr)
		if !ok {
			return fmt.Errorf("internal: имя поля записи %T", b.Left())
		}
		name, p := fv.Name(), posOf(fv)
		if typ != "" && !slices.Contains(declared, name) {
			return &Error{Line: int(p.Line), Col: int(p.Col), Msg: fmt.Sprintf("у типа %s нет поля %s", typ, name)}
		}
		if seen[name] {
			return &Error{Line: int(p.Line), Col: int(p.Col), Msg: fmt.Sprintf("повторное поле %s в литерале записи", name)}
		}
		seen[name] = true
		slots = append(slots, recordSlot{name, b.Right()})
	}

	decl := make([]runtime.Value, len(declared))
	for i, n := range declared {
		decl[i] = runtime.Str(n)
	}
	names := make([]runtime.Value, len(slots))
	for i, s := range slots {
		names[i] = runtime.Str(s.name)
	}
	shape := runtime.Tuple(runtime.Str(rtType), runtime.Tuple(decl...), runtime.Tuple(names...))

	mark := fc.nextReg
	dst := fc.destReg(d)
	base := fc.allocReg()
	fc.emit(vm.ABx(vm.LOADK, base, fc.konst(shape)))
	for i, s := range slots {
		r := fc.allocReg()
		if r != base+1+i {
			fc.fail("record: slot %d in r%d, want r%d", i, r, base+1+i)
		}
		if err := fc.compileExpr(s.val, val(r)); err != nil {
			return err
		}
	}
	fc.emit(vm.ABC(vm.RECORD, dst, base, len(slots)))
	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

// builtinRecords — записи, объявленные спекой вне модулей программы:
// их литерал и паттерн доступны в любом модуле без import, а имя типа
// в рантайме — без префикса (§13.2). Тип с тем же именем в своём
// модуле встроенный затеняет: recordType спрашивает его последним.
var builtinRecords = map[string][]string{
	"Behavior": {"handlers"},
}

// recordType разрешает имя типа записи в литерале или паттерне (§11.1,
// T-122 п.3): `T` — тип своего модуля, иначе тип `T` импортированного
// модуля `….T` (тип = последний сегмент модуля); `Mod.T` — тип модуля
// Mod. Возвращает имя типа в рантайме и объявленные поля.
func (fc *funcCompiler) recordType(written string, at vm.SrcPos) (string, []string, error) {
	unknown := errAt(at, "неизвестный тип записи %s", written)
	if strings.Contains(written, ".") {
		ref, typ, err := fc.resolvePath(strings.Split(written, "."), at)
		if err != nil {
			return "", nil, err
		}
		if ref.mod == nil {
			return "", nil, unknown
		}
		fields, ok := ref.mod.records[typ]
		if !ok {
			return "", nil, unknown
		}
		return ref.mod.prefix + typ, fields, nil
	}
	cur := fc.compiler.cur
	if fields, ok := cur.records[written]; ok {
		return cur.prefix + written, fields, nil
	}
	var found []string
	for _, full := range cur.locals {
		m := fc.compiler.mods[full]
		if m == nil || full[strings.LastIndex(full, ".")+1:] != written || slices.Contains(found, full) {
			continue
		}
		if _, ok := m.records[written]; ok {
			found = append(found, full)
		}
	}
	switch len(found) {
	case 0:
		if fields, ok := builtinRecords[written]; ok {
			return written, fields, nil
		}
		return "", nil, unknown
	case 1:
		m := fc.compiler.mods[found[0]]
		return m.prefix + written, m.records[written], nil
	}
	sort.Strings(found)
	return "", nil, errAt(at, "тип записи %s неоднозначен: %s", written, strings.Join(found, ", "))
}

// compileMember — доступ к полю записи `obj.field` (§4.7) или путь
// модуля `Mod.x` (compileModulePath).
func (fc *funcCompiler) compileMember(me ast.MemberExpr, d dest) error {
	// Имя с заглавной — не переменная: `M.f` — функция модуля, даже если
	// модуль неизвестен компилятору (модуль сессии REPL), как у вызова.
	if segs, ok := modulePath(me); ok {
		if name, member := splitPath(segs); fc.compiler.isModule(name) || !isUpperName(member) {
			return fc.compileModulePath(segs, pathStart(me), d)
		}
	}
	mark := fc.nextReg
	dst := fc.destReg(d)
	objReg, err := fc.operandInto(me.Obj(), fc.allocReg())
	if err != nil {
		return err
	}
	nameReg := fc.allocReg()
	fc.emit(vm.ABx(vm.LOADK, nameReg, fc.konst(runtime.Str(me.MemberName()))))
	fc.emit(vm.ABC(vm.GETFIELD, dst, objReg, nameReg))
	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

// ---- actor ops ----
