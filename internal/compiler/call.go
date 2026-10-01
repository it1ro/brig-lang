package compiler

import (
	"fmt"
	"strings"

	"github.com/it1ro/brig-lang/internal/ast"
	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/vm"
	"github.com/it1ro/brig-lang/stdlib"
)

func (fc *funcCompiler) compileCall(call ast.CallExpr, d dest) error {
	callee := call.Callee()

	// `Mod.f(a…)`: функция встроенного (Vec.*, Json.*, …) или
	// пользовательского модуля (§11.1). `Mod.Ctor(a…)` — обычный вызов
	// значения конструктора (compileMember).
	// Модуль программы без import/alias не виден (§11.1). Неизвестная
	// функция и неверная арность — ошибка sema.CheckNames (§F.3, T-139):
	// её сообщают brig check/run. Здесь неизвестный модуль по-прежнему
	// компилируется в GETGLOBAL — REPL и прямой Compile проход имён не
	// гоняют, и имя падает в рантайме (`undefined: Mod.f`).
	if me, ok := callee.(ast.MemberExpr); ok {
		if segs, ok := modulePath(me); ok {
			name, member := splitPath(segs)
			ref, known, err := fc.funcModule(name, pathStart(me))
			if err != nil {
				return err
			}
			// `Prelude.send(…)` — акторный примитив по имени прелюдии, когда
			// голое имя затенено fn модуля (§11.5): тот же опкод.
			if ref.builtin == "Prelude" {
				if ok, err := fc.compileActorCall(member, call.Args(), d); ok {
					return err
				}
			}
			if !known || ref.builtin != "" || !isUpperName(member) {
				return fc.compileGlobalCall(ref.global(member), call.Args(), d, call)
			}
		}
	}

	// Специальные формы по имени.
	if v, ok := callee.(ast.VariableExpr); ok {
		name := v.Name()
		switch name {
		case "()":
			return fc.compileSeq(call.Args(), vm.TUPLE, d)
		case "[]":
			return fc.compileSeq(call.Args(), vm.LIST, d)
		case "%[]":
			return fc.compileSeq(call.Args(), vm.VECTOR, d)
		case "%{}":
			return fc.compileMap(call.Args(), d)
		}
		// fn модуля и лексическая привязка (локаль, параметр, имя из
		// паттерна, локальная fn, захват) с именем акторного примитива
		// его затеняют (§11.5); примитив тогда — `Prelude.<name>(…)`.
		if !fc.compiler.cur.fns[name] && !fc.boundLexically(name) {
			if ok, err := fc.compileActorCall(name, call.Args(), d); ok {
				return err
			}
		}
		if strings.HasSuffix(name, "{}") {
			return fc.compileRecord(strings.TrimSuffix(name, "{}"), call, d)
		}
		// Голое имя, разрешаемое в функцию модуля stdlib (§13.2):
		// `spawn_behavior(b, state)` — `Behavior.spawn`. fn своего
		// модуля и лексическая привязка его затеняют, как и у акторных
		// примитивов (§11.5).
		if global, ok := bareStdlibFn[name]; ok && !fc.compiler.cur.fns[name] && !fc.boundLexically(name) {
			return fc.compileGlobalCall(global, call.Args(), d, call)
		}
	}

	return fc.compileGenericCall(call, d)
}

// boundLexically — связано ли name лексически в fc или в охватывающих
// fn: локаль, локальная fn или имя, которое станет upvalue. Без побочных
// эффектов (resolveUpvalue регистрирует захват).
func (fc *funcCompiler) boundLexically(name string) bool {
	for p := fc; p != nil; p = p.parent {
		if _, ok := p.resolveLocal(name); ok {
			return true
		}
		if _, ok := p.localFns[name]; ok {
			return true
		}
		for _, uv := range p.upvalues {
			if uv.name == name {
				return true
			}
		}
	}
	return false
}

// bareStdlibFn — голые имена, которые компилируются в вызов функции
// модуля stdlib на Brig. Список совпадает с sema.bareStdlib.
var bareStdlibFn = map[string]string{"spawn_behavior": "Behavior.spawn"}

// compileActorCall компилирует вызов акторного примитива name в его
// опкод. false — name не акторный примитив.
func (fc *funcCompiler) compileActorCall(name string, args []ast.Expr, d dest) (bool, error) {
	switch name {
	case "spawn":
		return true, fc.compileSpawn(args, 0, d)
	case "spawn_linked":
		return true, fc.compileSpawn(args, 1, d)
	case "spawn_watched":
		return true, fc.compileSpawn(args, 2, d)
	case "exit":
		return true, fc.compileExit(args, d)
	case "send":
		return true, fc.compileSend(args, d)
	case "self":
		return true, fc.compileSelf(d)
	case "make_ref":
		return true, fc.compileMakeRef(d)
	case "watch":
		return true, fc.compileWatch(args, d)
	case "link":
		return true, fc.compileLink(args, d)
	case "unwatch":
		return true, fc.compileUnwatch(args, d)
	case "mailbox_size":
		return true, fc.compileMailboxSize(args, d)
	case "register":
		return true, fc.compileActorOp(vm.REGISTER, name, 2, args, d)
	case "unregister":
		return true, fc.compileActorOp(vm.UNREGISTER, name, 1, args, d)
	case "whereis":
		return true, fc.compileActorOp(vm.WHEREIS, name, 1, args, d)
	case "await":
		return true, fc.compileActorOp(vm.AWAIT, name, 2, args, d)
	case "reply":
		return true, fc.compileActorOp(vm.REPLY, name, 3, args, d)
	}
	return false, nil
}

// compilePipe: `x |> f(a…)` — вызов `f(x, a…)` (§7.5); `x |> Mod.f(a…)` —
// вызов функции модуля, неизвестный модуль — ошибка с его именем (G-10).
// Форма `obj.method(a)` до методов — ошибка компиляции.
func (fc *funcCompiler) compilePipe(p ast.PipeExpr, d dest) error {
	callee := p.PipeRHS()
	args := append([]ast.Expr{p.PipeLHS()}, p.PipeArgs()...)
	if v, ok := callee.(ast.VariableExpr); ok && strings.Contains(v.Name(), ".") {
		segs, ok := modulePath(v)
		if !ok {
			return fmt.Errorf("%d:%d: |>: форма obj.method(a) не поддерживается: %s", p.Pos(), p.End(), v.Name())
		}
		ref, member, err := fc.resolvePath(segs, posOf(v))
		if err != nil {
			return err
		}
		if ref.builtin != "" || !isUpperName(member) {
			fc.pos = posOf(p)
			return fc.compileGlobalCall(ref.global(member), args, d, p)
		}
	}
	return fc.compileCall(ast.NewCallExpr(callee, args, p.Pos(), p.End()).(ast.CallExpr), d)
}

func (fc *funcCompiler) compileGenericCall(call ast.CallExpr, d dest) error {
	mark := fc.nextReg
	base := fc.allocReg()

	// Вызов локальной fn с захватом: захваты — хвост аргументов (T-51),
	// достаются по связыванию в точке объявления (T-39).
	var caps []upvalueInfo
	var owner *funcCompiler
	callee, isLocalFn := "", false
	if v, ok := call.Callee().(ast.VariableExpr); ok {
		callee, owner, isLocalFn = fc.resolveLocalFn(v.Name())
	}
	args := call.Args()
	nSpread, singleTrailing := spreadShape(args)
	if isLocalFn {
		if lf := fc.compiler.lifted[callee]; lf != nil {
			caps = lf.caps
		}
		// У variadic-fn захваты идут перед аргументами (см. liftedFn), у прочих —
		// после, и при спреде их место неизвестно до рантайма: такой вызов идёт
		// через значение-обёртку (loadLocalFn), захваты — её upvalue.
		if nSpread > 0 && len(caps) > 0 && !fc.compiler.lifted[callee].variadic {
			caps = nil
			if err := fc.loadLocalFn(val(base), callee, owner); err != nil {
				return err
			}
		} else if err := fc.loadGlobal(val(base), callee); err != nil {
			return err
		}
	} else if err := fc.compileExpr(call.Callee(), val(base)); err != nil {
		return err
	}

	leadCaps := len(caps) > 0 && fc.compiler.lifted[callee].variadic
	// Несколько спредов или спред не в конце: собрать аргументы списком
	// и развернуть его одним CALLSPREAD (§5.2, §5.3).
	if nSpread > 0 && !singleTrailing {
		if err := fc.compileCallViaList(base, args, caps, owner, leadCaps, d, posOf(call)); err != nil {
			return err
		}
		fc.releaseToMark(mark)
		return nil
	}
	spread := singleTrailing

	argc := len(args) + len(caps)
	next := 0
	loadCaps := func() {
		for _, uv := range caps {
			r := fc.allocReg()
			if r != base+1+next {
				fc.fail("call: arg %d in r%d, want r%d", next, r, base+1+next)
			}
			fc.loadCapture(owner, uv, r)
			next++
		}
	}
	if leadCaps {
		loadCaps()
	}
	for _, a := range args {
		r := fc.allocReg()
		if r != base+1+next {
			fc.fail("call: arg %d in r%d, want r%d", next, r, base+1+next)
		}
		if u, ok := a.(ast.UnaryExpr); ok && u.OpStr() == ".." {
			a = u.Operand()
		}
		if err := fc.compileArg(a, r, fc.passModuleName(call)); err != nil {
			return err
		}
		next++
	}
	if !leadCaps {
		loadCaps()
	}

	if err := fc.emitInvoke(base, argc, spread, d, posOf(call)); err != nil {
		return err
	}
	fc.releaseToMark(mark)
	return nil
}

func (fc *funcCompiler) compileGlobalCall(name string, args []ast.Expr, d dest, pos ast.Node) error {
	mark := fc.nextReg
	base := fc.allocReg()
	gidx := fc.konst(runtime.Str(name))
	fc.emit(vm.ABx(vm.GETGLOBAL, base, gidx))

	nSpread, singleTrailing := spreadShape(args)
	if nSpread > 0 && !singleTrailing {
		if err := fc.compileCallViaList(base, args, nil, nil, false, d, posOf(pos)); err != nil {
			return err
		}
		fc.releaseToMark(mark)
		return nil
	}
	argc := len(args)
	for i, a := range args {
		r := fc.allocReg()
		if r != base+1+i {
			fc.fail("call: arg %d in r%d, want r%d", i, r, base+1+i)
		}
		if operand, ok := spreadOperand(a); ok {
			a = operand
		}
		if err := fc.compileArg(a, r, fc.compiler.replHelpers && name == "Repl.h"); err != nil {
			return err
		}
	}

	if err := fc.emitInvoke(base, argc, singleTrailing, d, posOf(pos)); err != nil {
		return err
	}
	fc.releaseToMark(mark)
	return nil
}

// passModuleName — вызов голого `h`, не затенённого связыванием этой
// инструкции. Аргумент-модуль тогда не ищется как значение.
func (fc *funcCompiler) passModuleName(call ast.CallExpr) bool {
	if !fc.compiler.replHelpers {
		return false
	}
	v, ok := call.Callee().(ast.VariableExpr)
	if !ok || v.Name() != "h" {
		return false
	}
	if _, ok := fc.resolveLocal("h"); ok {
		return false
	}
	if _, _, ok := fc.resolveLocalFn("h"); ok {
		return false
	}
	return true
}

// compileArg компилирует аргумент вызова в уже выделенный регистр r.
// Для `h`/`Repl.h` в REPL: голое имя модуля — атом с этим именем
// (`h(Map)` → `:Map`; атом с заглавной буквы в исходнике не записать,
// поэтому строку `h("Map")` хелпер отличает). Вне этого случая, в том
// числе `M.f`, — обычное выражение.
func (fc *funcCompiler) compileArg(a ast.Expr, r int, helperH bool) error {
	if helperH {
		if v, ok := a.(ast.VariableExpr); ok && isUpperName(v.Name()) {
			return fc.loadConst(runtime.Atom(v.Name()), val(r))
		}
	}
	return fc.compileExpr(a, val(r))
}

// isNativeModule — встроенный модуль на Go: его функции ставит в ВМ
// vm.New. Список совпадает с loader.builtinModules и sema.isNativeMod.
func isNativeModule(name string) bool {
	switch name {
	case "Vec", "Map", "Record", "Str", "Bytes", "Json", "Test", "Sys", "Actor", "Prelude", "Global", "Timer", "Time", "Telemetry", "Port", "Signal", "File", "HttpServer":
		return true
	}
	return false
}

// isBuiltinModule — модуль, доступный без import (§11.1): Go-нативный
// или встроенный модуль на Brig (stdlib, T-146).
func isBuiltinModule(name string) bool {
	return isNativeModule(name) || stdlib.IsModule(name)
}

// ---- collections ----

// compileCallViaList собирает аргументы со спредом в один список и
// вызывает через CALLSPREAD. Захваты variadic-fn идут перед списком.
func (fc *funcCompiler) compileCallViaList(base int, args []ast.Expr, caps []upvalueInfo, owner *funcCompiler, leadCaps bool, d dest, at vm.SrcPos) error {
	next := 0
	if leadCaps {
		for _, uv := range caps {
			r := fc.allocReg()
			if r != base+1+next {
				fc.fail("call: arg %d in r%d, want r%d", next, r, base+1+next)
			}
			fc.loadCapture(owner, uv, r)
			next++
		}
	}
	listReg := fc.allocReg()
	if listReg != base+1+next {
		fc.fail("call: spread list in r%d, want r%d", listReg, base+1+next)
	}
	seg := fc.nextReg
	if err := fc.compileSpreadPairs(args); err != nil {
		return err
	}
	fc.pos = at
	fc.emit(vm.ABC(vm.LISTSPREAD, listReg, seg, len(args)))
	return fc.emitInvoke(base, next+1, true, d, at)
}

func (fc *funcCompiler) emitInvoke(base, argc int, spread bool, d dest, at vm.SrcPos) error {
	fc.pos = at
	if d.tail && fc.trapDepth > 0 {
		fc.fail("TAILCALL under active trap (trapDepth=%d)", fc.trapDepth)
	}
	if d.ens != nil && !spread {
		return fc.emitTailCallEns(base, argc, d.ens, at)
	}
	tailOp, callOp := vm.TAILCALL, vm.CALL
	if spread {
		tailOp, callOp = vm.TAILCALLSPREAD, vm.CALLSPREAD
	}
	if d.tail {
		fc.emit(vm.ABC(tailOp, base, argc, 0))
		return nil
	}
	dst := d.reg
	if dst == -1 {
		dst = fc.allocReg()
	}
	fc.emit(vm.ABC(callOp, base, argc, dst))
	return nil
}

// emitTailCallEns — хвостовой вызов из тела trap с ensure (doc 02 §5.1):
// замыкания всех ensure trap в порядке LIFO в R[base+argc+1..], затем
// TAILCALLENS. Имена в ensure разрешаются в областях тела, как в блоке
// ENS: область ветки, где стоит вызов, их не затеняет.
func (fc *funcCompiler) emitTailCallEns(base, argc int, et *ensTail, at vm.SrcPos) error {
	if fc.trapDepth != 1 {
		fc.fail("TAILCALLENS at trapDepth=%d, want 1", fc.trapDepth)
	}
	if fc.nextReg != base+1+argc {
		fc.fail("TAILCALLENS: args end at r%d, want r%d", fc.nextReg, base+1+argc)
	}
	saved := fc.scopes
	fc.scopes = append([]scope(nil), fc.scopes[:et.scopes]...)
	defer func() { fc.scopes = saved }()
	for i := len(et.ensures) - 1; i >= 0; i-- {
		ens := et.ensures[i]
		r := fc.allocReg()
		fc.pos = posOf(ens)
		if err := fc.compileLambda("", nil, ens, val(r)); err != nil {
			return err
		}
	}
	fc.pos = at
	fc.emit(vm.ABC(vm.TAILCALLENS, base, argc, len(et.ensures)))
	return nil
}

// ---- records (T-73, §4.7) ----

// compileSpawn: mode — операнд C у SPAWN: 0 — spawn, 1 — spawn_linked,
// 2 — spawn_watched (результат (pid, ref)). Второй аргумент — лимиты хода
// (§12.10): в регистре за fn, в C — бит vm.SpawnLimits.
func (fc *funcCompiler) compileSpawn(args []ast.Expr, mode int, d dest) error {
	if len(args) != 1 && len(args) != 2 {
		return fmt.Errorf("spawn требует 1 или 2 аргумента (fn, limits)")
	}
	mark := fc.nextReg
	dst := fc.destReg(d)

	fnReg := fc.allocReg()
	if err := fc.compileExpr(args[0], val(fnReg)); err != nil {
		return err
	}
	if len(args) == 2 {
		limReg := fc.allocReg()
		if limReg != fnReg+1 {
			return fmt.Errorf("internal: spawn limits register r%d, want r%d", limReg, fnReg+1)
		}
		if err := fc.compileExpr(args[1], val(limReg)); err != nil {
			return err
		}
		mode |= vm.SpawnLimits
	}

	fc.emit(vm.ABC(vm.SPAWN, dst, fnReg, mode))
	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

func (fc *funcCompiler) compileSend(args []ast.Expr, d dest) error {
	if len(args) != 2 {
		return fmt.Errorf("send требует 2 аргумента (pid, msg)")
	}
	mark := fc.nextReg
	dst := fc.destReg(d)

	pidReg := fc.allocReg()
	if err := fc.compileExpr(args[0], val(pidReg)); err != nil {
		return err
	}
	msgReg := fc.allocReg()
	if err := fc.compileExpr(args[1], val(msgReg)); err != nil {
		return err
	}
	fc.emit(vm.ABC(vm.SEND, dst, pidReg, msgReg))
	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

// compileExit: exit(pid, reason) (§12.7).
func (fc *funcCompiler) compileExit(args []ast.Expr, d dest) error {
	if len(args) != 2 {
		return fmt.Errorf("exit требует 2 аргумента (pid, reason)")
	}
	mark := fc.nextReg
	dst := fc.destReg(d)

	pidReg := fc.allocReg()
	if err := fc.compileExpr(args[0], val(pidReg)); err != nil {
		return err
	}
	reasonReg := fc.allocReg()
	if err := fc.compileExpr(args[1], val(reasonReg)); err != nil {
		return err
	}
	fc.emit(vm.ABC(vm.EXIT, dst, pidReg, reasonReg))
	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

func (fc *funcCompiler) compileSelf(d dest) error {
	mark := fc.nextReg
	dst := fc.destReg(d)
	fc.emit(vm.ABC(vm.SELF, dst, 0, 0))
	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

func (fc *funcCompiler) compileMakeRef(d dest) error {
	mark := fc.nextReg
	dst := fc.destReg(d)
	fc.emit(vm.ABC(vm.MAKEREF, dst, 0, 0))
	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

func (fc *funcCompiler) compileWatch(args []ast.Expr, d dest) error {
	if len(args) != 1 {
		return fmt.Errorf("watch требует 1 аргумент (pid)")
	}
	mark := fc.nextReg
	dst := fc.destReg(d)
	pidReg := fc.allocReg()
	if err := fc.compileExpr(args[0], val(pidReg)); err != nil {
		return err
	}
	fc.emit(vm.ABC(vm.WATCH, dst, pidReg, 0))
	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

func (fc *funcCompiler) compileUnwatch(args []ast.Expr, d dest) error {
	if len(args) != 1 {
		return fmt.Errorf("unwatch требует 1 аргумент (ref)")
	}
	mark := fc.nextReg
	dst := fc.destReg(d)
	refReg := fc.allocReg()
	if err := fc.compileExpr(args[0], val(refReg)); err != nil {
		return err
	}
	fc.emit(vm.ABC(vm.UNWATCH, dst, refReg, 0))
	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

// compileLink — `link(pid)`: вызывающий становится владельцем pid
// (§12.2, T-252), результат (). Это не `watch`: ref не выдаётся,
// и тег ошибки — свой, `(:type_error, (:link, pid))`.
func (fc *funcCompiler) compileLink(args []ast.Expr, d dest) error {
	if len(args) != 1 {
		return fmt.Errorf("link требует 1 аргумент (pid)")
	}
	mark := fc.nextReg
	dst := fc.destReg(d)
	pidReg := fc.allocReg()
	if err := fc.compileExpr(args[0], val(pidReg)); err != nil {
		return err
	}
	fc.emit(vm.ABC(vm.LINK, dst, pidReg, 0))
	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

// compileMailboxSize — `mailbox_size(pid)`; без аргументов — своя очередь.
func (fc *funcCompiler) compileMailboxSize(args []ast.Expr, d dest) error {
	if len(args) > 1 {
		return fmt.Errorf("mailbox_size требует 0 или 1 аргумент (pid)")
	}
	mark := fc.nextReg
	dst := fc.destReg(d)
	pidReg := fc.allocReg()
	if len(args) == 0 {
		fc.emit(vm.ABC(vm.SELF, pidReg, 0, 0))
	} else if err := fc.compileExpr(args[0], val(pidReg)); err != nil {
		return err
	}
	fc.emit(vm.ABC(vm.MAILBOXSIZE, dst, pidReg, 0))
	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

// compileActorOp — акторный примитив с фиксированной арностью, например
// `register(name, pid)`, `unregister(name)`, `whereis(name)` (§12.8),
// `await(ref, timeout)`, `reply(pid, ref, v)` (§12.9): аргументы в подряд
// идущих регистрах от B; при двух аргументах второй — ещё и C.
func (fc *funcCompiler) compileActorOp(op vm.OpCode, name string, arity int, args []ast.Expr, d dest) error {
	if len(args) != arity {
		return fmt.Errorf("%s требует %d аргумента(ов)", name, arity)
	}
	mark := fc.nextReg
	dst := fc.destReg(d)
	regs := make([]int, arity)
	for i, arg := range args {
		regs[i] = fc.allocReg()
		if err := fc.compileExpr(arg, val(regs[i])); err != nil {
			return err
		}
	}
	c := 0
	if arity == 2 {
		c = regs[1]
	}
	fc.emit(vm.ABC(op, dst, regs[0], c))
	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

// ---- trap / ensure (§5) ----
