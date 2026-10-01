package compiler

import (
	"fmt"
	"strings"

	"github.com/it1ro/brig-lang/internal/ast"
	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/vm"
)

// i1TestLeak — white-box hook for TestCompileExprStackNeutral (T-32).
// When set, compileExpr calls it instead of the real dispatch so the test
// can simulate a callee that forgets releaseToMark.
var i1TestLeak func(*funcCompiler)

func (fc *funcCompiler) compileExpr(e ast.Expr, d dest) (err error) {
	// I-1: compileExpr стек-нейтрален. Регистр dest выделяет вызывающий
	// до входа; временные внутри обязаны быть сняты releaseToMark.
	entry := fc.nextReg
	defer func() {
		if err != nil || fc.nextReg == entry {
			return
		}
		// Уже паникуем (fc.fail в callee) — не подменять исходную ошибку I-1.
		if p := recover(); p != nil {
			panic(p)
		}
		fc.fail("compileExpr: nextReg %d != %d (I-1)", fc.nextReg, entry)
	}()
	if i1TestLeak != nil {
		i1TestLeak(fc)
		return nil
	}
	// Позиция узла действует, пока он компилируется; инструкции
	// родителя после него снова получают позицию родителя (O-F4, T-41).
	if p := posOf(e); p.Line > 0 {
		saved := fc.pos
		fc.pos = p
		defer func() {
			err = fc.locate(err)
			fc.pos = saved
		}()
	}
	switch ex := e.(type) {
	case ast.LiteralExpr:
		return fc.compileLiteralExpr(ex.ValueStr(), d)
	case ast.InterpExpr:
		return fc.compileInterp(ex, d)
	case ast.AtomExpr:
		return fc.loadConst(runtime.Atom(ex.AtomName()), d)
	case ast.DecimalExpr:
		return fc.compileDecimal(ex.ValueStr(), d)
	case ast.BytesExpr:
		return fc.compileBytes(ex.ValueStr(), d)
	case ast.RegexExpr:
		return fmt.Errorf("срез: regex не реализован")
	case ast.VariableExpr:
		return fc.compileVar(ex.Name(), d)
	case ast.GroupingExpr:
		return fc.compileExpr(ex.Inner(), d)
	case ast.UnaryExpr:
		return fc.compileUnary(ex, d)
	case ast.BinaryExpr:
		return fc.compileBinary(ex, d)
	case ast.CallExpr:
		return fc.compileCall(ex, d)
	case ast.IfExpr:
		return fc.compileIf(ex, d)
	case ast.TrapExpr:
		return fc.compileTrap(ex, d)
	case ast.MatchExpr:
		return fc.compileMatch(ex, d)
	case ast.WithExpr:
		return fc.compileWith(ex, d)
	case ast.RecvExpr:
		return fc.compileRecv(ex, d)
	case ast.RangeExpr:
		return fc.compileRange(ex, d)
	case ast.IndexExpr:
		return fc.compileIndex(ex, d)
	case ast.MemberExpr:
		return fc.compileMember(ex, d)
	case ast.LambdaShort:
		return fc.compileLambda("", ex.ParamNames(), ex.Body(), d)
	case ast.LambdaEmpty:
		return fc.compileLambda("", nil, ex.Body(), d)
	case ast.LambdaFull:
		return fc.compileLambdaFull("", ex.Params(), ex.BlockBody(), d)
	case ast.PipeExpr:
		return fc.compilePipe(ex, d)
	}
	return fmt.Errorf("срез: неподдерживаемое выражение %T", e)
}

func (fc *funcCompiler) compileLiteralExpr(lit string, d dest) error {
	v, err := parseLiteralValue(lit)
	if err != nil {
		return err
	}
	return fc.loadConst(v, d)
}

// compileInterp lowers "a \(e) b" to string concat of decoded parts and
// to_str(e) for each interpolated expression (S-F1 / T-54).
// parts has len(exprs)+1; empty parts are skipped except as the initial
// accumulator when the string starts with \(...).
func (fc *funcCompiler) compileInterp(ie ast.InterpExpr, d dest) error {
	mark := fc.nextReg
	dst := fc.destReg(d)
	parts := ie.InterpParts()
	exprs := ie.InterpExprs()

	if err := fc.loadConst(runtime.Str(decodeStrBody(parts[0])), val(dst)); err != nil {
		return err
	}

	for i, expr := range exprs {
		iterMark := fc.nextReg

		strReg := fc.allocReg()
		if err := fc.compileGlobalCall(fc.compiler.bareGlobal("to_str"), []ast.Expr{expr}, val(strReg), ie); err != nil {
			return err
		}
		fc.pos = posOf(ie)
		fc.emit(vm.ABC(vm.CONCAT, dst, dst, strReg))

		if parts[i+1] != "" {
			partReg := fc.allocReg()
			if err := fc.loadConst(runtime.Str(decodeStrBody(parts[i+1])), val(partReg)); err != nil {
				return err
			}
			fc.pos = posOf(ie)
			fc.emit(vm.ABC(vm.CONCAT, dst, dst, partReg))
		}

		fc.releaseToMark(iterMark)
	}

	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

func (fc *funcCompiler) compileDecimal(body string, d dest) error {
	r, err := runtime.ParseDecimal(body)
	if err != nil {
		return err
	}
	return fc.loadConst(runtime.Decimal(r), d)
}

func (fc *funcCompiler) compileBytes(body string, d dest) error {
	b, err := decodeBytesBody(body)
	if err != nil {
		return fmt.Errorf("bytes literal: %w", err)
	}
	return fc.loadConst(runtime.Bytes(b), d)
}

// compileVar — порядок разрешения (§7):
// локаль → локальная fn (своя и предков) → upvalue → глобал.
func (fc *funcCompiler) compileVar(name string, d dest) error {
	if r, ok := fc.resolveLocal(name); ok {
		return fc.loadVal(d, r)
	}
	if mangled, owner, ok := fc.resolveLocalFn(name); ok {
		return fc.loadLocalFn(d, mangled, owner)
	}
	if idx, ok := fc.resolveUpvalue(name); ok {
		return fc.loadUpval(d, idx)
	}
	if strings.Contains(name, ".") {
		// `x |> Mod.Ctor` — pipe держит путь одним именем.
		return fc.compileModulePath(strings.Split(name, "."), fc.pos, d)
	}
	if ct, ok := fc.compiler.cur.ctors[name]; ok {
		return fc.loadConst(ct.val, d)
	}
	return fc.loadGlobal(d, fc.compiler.bareGlobal(name))
}

// bareGlobal — глобальное имя для голого имени name в текущем модуле:
// своя fn модуля, иначе прелюдия. fn входного модуля лежат под голыми
// именами и затеняют прелюдию только в нём самом: в других модулях имя
// прелюдии, совпавшее с fn входного, берётся как `Prelude.name` (§11.5).
// С preludeQualified — любое имя функции (конструкторы и `None` — не
// функции, их fn входного модуля не затеняет).
func (c *Compiler) bareGlobal(name string) string {
	switch {
	case c.cur.fns[name]:
		return c.cur.prefix + name
	case c.cur != c.entry && c.entry.fns[name]:
		return "Prelude." + name
	case c.cur != c.entry && c.preludeQualified && !isUpperName(name):
		return "Prelude." + name
	}
	return name
}

// loadLocalFn — локальная fn как значение. Без захватов это глобальная
// Function. С захватами — замыкание над обёрткой: обёртка арности
// объявленных параметров хвостом вызывает лифтнутую fn, дописывая
// захваты из своих upvalue (T-39).
func (fc *funcCompiler) loadLocalFn(d dest, mangled string, owner *funcCompiler) error {
	lf := fc.compiler.lifted[mangled]
	if lf == nil || len(lf.caps) == 0 {
		return fc.loadGlobal(d, mangled)
	}
	if d.reg == -1 && !d.tail {
		return nil
	}

	w := fc.compiler.newFuncCompiler(nil)
	w.prefix = mangled + "$"
	w.pos = fc.pos // у обёртки нет своего исходника — позиция ссылки
	w.chunk.NumParams = lf.arity
	if lf.variadic {
		w.chunk.NumParams++ // rest-список
		w.chunk.Variadic = true
	}
	for i := 0; i < w.chunk.NumParams; i++ {
		w.allocReg()
	}
	wbase := w.allocReg()
	w.emit(vm.ABx(vm.GETGLOBAL, wbase, w.konst(runtime.Str(mangled))))
	movesCaps := func() {
		for j := range lf.caps {
			w.emit(vm.ABC(vm.GETUPVAL, w.allocReg(), j, 0))
		}
	}
	if lf.variadic {
		movesCaps()
	}
	for i := 0; i < w.chunk.NumParams; i++ {
		w.emit(vm.ABC(vm.MOVE, w.allocReg(), i, 0))
	}
	if lf.variadic {
		w.emit(vm.ABC(vm.TAILCALLSPREAD, wbase, w.chunk.NumParams+len(lf.caps), 0))
	} else {
		movesCaps()
		w.emit(vm.ABC(vm.TAILCALL, wbase, lf.arity+len(lf.caps), 0))
	}
	fnIdx := fc.chunk.AddConstant(vm.FuncValue(w.function(mangled)))

	mark := fc.nextReg
	dst := fc.destReg(d)
	base := fc.allocReg()
	fc.emit(vm.ABx(vm.LOADK, base, fnIdx))
	for j, uv := range lf.caps {
		r := fc.allocReg()
		if r != base+1+j {
			fc.fail("closure: capture %d in r%d, want r%d", j, r, base+1+j)
		}
		fc.loadCapture(owner, uv, r)
	}
	fc.emit(vm.ABC(vm.MAKECLOSURE, dst, base, len(lf.caps)))
	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

func (fc *funcCompiler) loadVal(d dest, src int) error {
	if d.tail {
		fc.emit(vm.ABC(vm.RETURN, src, 0, 0))
		return nil
	}
	if d.reg == -1 {
		return nil
	}
	if src != d.reg {
		fc.emit(vm.ABC(vm.MOVE, d.reg, src, 0))
	}
	return nil
}

func (fc *funcCompiler) loadGlobal(d dest, name string) error {
	var r int
	switch {
	case d.tail:
		r = fc.allocReg()
	case d.reg == -1:
		return nil
	default:
		r = d.reg
	}
	gidx := fc.konst(runtime.Str(name))
	fc.emit(vm.ABx(vm.GETGLOBAL, r, gidx))
	if d.tail {
		fc.emit(vm.ABC(vm.RETURN, r, 0, 0))
		fc.releaseToMark(r)
	}
	return nil
}

func (fc *funcCompiler) loadUpval(d dest, idx int) error {
	var r int
	switch {
	case d.tail:
		r = fc.allocReg()
	case d.reg == -1:
		return nil
	default:
		r = d.reg
	}
	fc.emit(vm.ABC(vm.GETUPVAL, r, idx, 0))
	if d.tail {
		fc.emit(vm.ABC(vm.RETURN, r, 0, 0))
		fc.releaseToMark(r)
	}
	return nil
}

func (fc *funcCompiler) compileUnary(u ast.UnaryExpr, d dest) error {
	if u.OpStr() == ".." {
		return fmt.Errorf("спред .. здесь недопустим")
	}
	mark := fc.nextReg
	dst := fc.destReg(d)

	opReg, err := fc.operandInto(u.Operand(), dst)
	if err != nil {
		return err
	}

	fc.pos = posOf(u)
	switch u.OpStr() {
	case "-":
		fc.emit(vm.ABC(vm.NEG, dst, opReg, 0))
	case "not":
		fc.emit(vm.ABC(vm.NOT, dst, opReg, 0))
	default:
		return fmt.Errorf("internal: унарный оператор %q", u.OpStr())
	}
	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

func (fc *funcCompiler) compileBinary(b ast.BinaryExpr, d dest) error {
	switch b.OpStr() {
	case "and":
		return fc.compileAndOr(b, true, d)
	case "or":
		return fc.compileAndOr(b, false, d)
	}

	mark := fc.nextReg
	dst := fc.destReg(d)

	leftReg, err := fc.operandInto(b.Left(), dst)
	if err != nil {
		return err
	}
	rightReg, err := fc.operand(b.Right())
	if err != nil {
		return err
	}

	fc.pos = posOf(b)
	op, ok := binOp(b.OpStr())
	if !ok {
		return fmt.Errorf("срез: оператор %q", b.OpStr())
	}
	fc.emit(vm.ABC(op, dst, leftReg, rightReg))
	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

func binOp(s string) (vm.OpCode, bool) {
	switch s {
	case "+":
		return vm.ADD, true
	case "-":
		return vm.SUB, true
	case "*":
		return vm.MUL, true
	case "/":
		return vm.DIV, true
	case "div":
		return vm.INTDIV, true
	case "rem":
		return vm.REM, true
	case "**":
		return vm.POW, true
	case "==":
		return vm.EQ, true
	case "!=":
		return vm.NEQ, true
	case "<":
		return vm.LT, true
	case ">":
		return vm.GT, true
	case "<=":
		return vm.LE, true
	case ">=":
		return vm.GE, true
	case "<>":
		return vm.CONCAT, true
	}
	return 0, false
}

// compileAndOr: строгий Bool (DD #41 вариант A). Левый операнд проверяет сам
// JMPIF/JMPIFNOT (не-Bool → (:type_error, (:expected_bool, v))); правый
// проверяется после вычисления «холостым» условным переходом на следующую
// инструкцию (оба исхода — ip+1, различие лишь в проверке типа). Правый
// операнд по-прежнему не хвостовой (T-82).
func (fc *funcCompiler) compileAndOr(b ast.BinaryExpr, isAnd bool, d dest) error {
	mark := fc.nextReg
	acc := fc.destReg(d)

	if err := fc.compileExpr(b.Left(), val(acc)); err != nil {
		return err
	}

	var jumpOp vm.OpCode
	if isAnd {
		jumpOp = vm.JMPIFNOT
	} else {
		jumpOp = vm.JMPIF
	}
	jEnd := fc.emitJump(jumpOp, acc)

	if err := fc.compileExpr(b.Right(), val(acc)); err != nil {
		return err
	}
	fc.emitJump(jumpOp, acc) // холостой: только проверка Bool у правого операнда

	fc.patchHere(jEnd)
	fc.finish(d, acc)
	fc.releaseToMark(mark)
	return nil
}

func (fc *funcCompiler) compileIf(ie ast.IfExpr, d dest) error {
	pos := posOf(ie)
	mark := fc.nextReg

	cReg, err := fc.operand(ie.Cond())
	if err != nil {
		return err
	}

	fc.pos = pos
	jElse := fc.emitJump(vm.JMPIFNOT, cReg)
	fc.releaseToMark(mark)

	thenMark := fc.nextReg
	if err := fc.compileBranch(ie.ThenBody(), d); err != nil {
		return err
	}
	fc.releaseToMark(thenMark)

	jEnd := -1
	if !d.tail {
		jEnd = fc.emitJump(vm.JMP, 0)
	}

	fc.patchHere(jElse)

	elseMark := fc.nextReg
	if eb := ie.ElseBody(); eb != nil {
		if err := fc.compileBranch(eb, d); err != nil {
			return err
		}
	} else {
		if err := fc.loadUnit(d); err != nil {
			return err
		}
	}
	fc.releaseToMark(elseMark)

	if jEnd >= 0 {
		fc.patchHere(jEnd)
	}
	return nil
}

func (fc *funcCompiler) compileBranch(body ast.Expr, d dest) error {
	if blk, ok := body.(*ast.BlockStmt); ok {
		fc.pushScope()
		defer fc.popScope()
		return fc.compileStmts(blk.Body(), d)
	}
	return fc.compileExpr(body, d)
}

// ---- calls ----

// compileSeq: элементы в последовательные регистры от base (первый
// элемент задаёт base), затем ctor. Окно чтения ctor — base..base+n-1.
// Спред `..expr` в списке или векторе — LISTSPREAD/VECSPREAD (§5.2).
func (fc *funcCompiler) compileSeq(elems []ast.Expr, op vm.OpCode, d dest) error {
	if op != vm.TUPLE && hasSpread(elems) {
		spreadOp := vm.LISTSPREAD
		if op == vm.VECTOR {
			spreadOp = vm.VECSPREAD
		}
		return fc.compileSpreadSeq(elems, spreadOp, d)
	}
	mark := fc.nextReg
	dst := fc.destReg(d)

	base := 0
	for i, e := range elems {
		r := fc.allocReg()
		if i == 0 {
			base = r
		}
		if r != base+i {
			fc.fail("seq: elem %d in r%d, want r%d", i, r, base+i)
		}
		if err := fc.compileExpr(e, val(r)); err != nil {
			return err
		}
	}
	// Пустая коллекция: base не читается (C=0), значение не важно.
	fc.emit(vm.ABC(op, dst, base, len(elems)))
	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

// compileMap: пары k,v в последовательных регистрах от base (первая
// пара задаёт base), затем MAP. Окно чтения — base..base+2n-1.
// Спред `..expr` — MAPSPREAD (§5.2, §4.5).
func (fc *funcCompiler) compileMap(elems []ast.Expr, d dest) error {
	if hasSpread(elems) {
		return fc.compileSpreadMap(elems, d)
	}
	mark := fc.nextReg
	dst := fc.destReg(d)

	base := 0
	n := 0
	for _, e := range elems {
		pair, ok := e.(ast.BinaryExpr)
		if !ok || pair.OpStr() != "=>" {
			return fmt.Errorf("internal: элемент мапы %T", e)
		}
		if n == 0 {
			base = fc.nextReg
		}
		kReg := fc.allocReg()
		if err := fc.compileExpr(pair.Left(), val(kReg)); err != nil {
			return err
		}
		vReg := fc.allocReg()
		if err := fc.compileExpr(pair.Right(), val(vReg)); err != nil {
			return err
		}
		n++
	}
	fc.emit(vm.ABC(vm.MAP, dst, base, n))
	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

// spreadOperand — выражение под `..`, если e сам спред.
func spreadOperand(e ast.Expr) (ast.Expr, bool) {
	if u, ok := e.(ast.UnaryExpr); ok && u.OpStr() == ".." {
		return u.Operand(), true
	}
	return nil, false
}

func hasSpread(elems []ast.Expr) bool {
	for _, e := range elems {
		if _, ok := spreadOperand(e); ok {
			return true
		}
	}
	return false
}

// spreadShape: число спредов и «ровно один, и он последний».
func spreadShape(args []ast.Expr) (n int, singleTrailing bool) {
	last := false
	for i, a := range args {
		if _, ok := spreadOperand(a); ok {
			n++
			last = i == len(args)-1
		}
	}
	return n, n == 1 && last
}

// compileSpreadPairs кладёт сегменты списка/вектора: на элемент два
// регистра (Bool-тег, значение) начиная с текущего nextReg.
func (fc *funcCompiler) compileSpreadPairs(elems []ast.Expr) error {
	for _, e := range elems {
		tag := fc.allocReg()
		operand, spread := spreadOperand(e)
		if !spread {
			operand = e
		}
		fc.emit(vm.ABx(vm.LOADK, tag, fc.konst(runtime.Bool(spread))))
		r := fc.allocReg()
		if err := fc.compileExpr(operand, val(r)); err != nil {
			return err
		}
	}
	return nil
}

func (fc *funcCompiler) compileSpreadSeq(elems []ast.Expr, op vm.OpCode, d dest) error {
	mark := fc.nextReg
	dst := fc.destReg(d)
	base := fc.nextReg
	if err := fc.compileSpreadPairs(elems); err != nil {
		return err
	}
	fc.emit(vm.ABC(op, dst, base, len(elems)))
	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

func (fc *funcCompiler) compileSpreadMap(elems []ast.Expr, d dest) error {
	mark := fc.nextReg
	dst := fc.destReg(d)
	base := fc.nextReg
	for _, e := range elems {
		tag := fc.allocReg()
		if operand, ok := spreadOperand(e); ok {
			fc.emit(vm.ABx(vm.LOADK, tag, fc.konst(runtime.Bool(true))))
			r := fc.allocReg()
			if err := fc.compileExpr(operand, val(r)); err != nil {
				return err
			}
			pad := fc.allocReg()
			fc.emit(vm.ABx(vm.LOADK, pad, fc.konst(runtime.Unit)))
			continue
		}
		pair, ok := e.(ast.BinaryExpr)
		if !ok || pair.OpStr() != "=>" {
			return fmt.Errorf("internal: элемент мапы %T", e)
		}
		fc.emit(vm.ABx(vm.LOADK, tag, fc.konst(runtime.Bool(false))))
		kReg := fc.allocReg()
		if err := fc.compileExpr(pair.Left(), val(kReg)); err != nil {
			return err
		}
		vReg := fc.allocReg()
		if err := fc.compileExpr(pair.Right(), val(vReg)); err != nil {
			return err
		}
	}
	fc.emit(vm.ABC(vm.MAPSPREAD, dst, base, len(elems)))
	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

// compileMatch: субъект в регистр, ветки — compileCaseBranches.
func (fc *funcCompiler) compileMatch(me ast.MatchExpr, d dest) error {
	mark := fc.nextReg

	sReg, err := fc.operand(me.MatchSubject())
	if err != nil {
		return err
	}
	if err := fc.compileCaseBranches(sReg, me.MatchBranches(), posOf(me), d); err != nil {
		return err
	}
	fc.releaseToMark(mark)
	return nil
}

// compileCaseBranches: ветки по порядку над R[sReg] — MATCHLOCAL+JMP к
// следующей ветке (схема веток recv). Ветки наследуют d (d.tail → хвостовые).
// Ни одна ветка не подошла — raise (:case_clause, val) с позицией pos (§10.4).
// Общий код match (§8.3) и with/else (§8.2).
func (fc *funcCompiler) compileCaseBranches(sReg int, branches []ast.MatchBranchArg, pos vm.SrcPos, d dest) error {
	mark := fc.nextReg

	var endJumps []int
	for _, br := range branches {
		brMark := fc.nextReg
		fc.pushScope()

		cp, err := fc.compilePattern(br.Pattern)
		if err != nil {
			fc.popScope()
			return err
		}
		patIdx := fc.chunk.AddPattern(cp)

		fc.pos = posOf(br.Pattern)
		fc.emit(vm.ABx(vm.MATCHLOCAL, sReg, patIdx))
		jFail := fc.emitJump(vm.JMP, 0)

		if err := fc.compileBranch(br.Body, d); err != nil {
			fc.popScope()
			return err
		}
		if !d.tail {
			endJumps = append(endJumps, fc.emitJump(vm.JMP, 0))
		}

		fc.patchHere(jFail)
		fc.popScope()
		fc.releaseToMark(brMark)
	}

	fc.pos = pos
	w0 := fc.allocReg()
	w1 := fc.allocReg()
	fc.emit(vm.ABx(vm.LOADK, w0, fc.konst(runtime.Atom("case_clause"))))
	fc.emit(vm.ABC(vm.MOVE, w1, sReg, 0))
	fc.emit(vm.ABC(vm.TUPLE, w0, w0, 2))
	fc.emit(vm.ABC(vm.RAISE, w0, 0, 0))

	for _, j := range endJumps {
		fc.patchHere(j)
	}
	fc.releaseToMark(mark)
	return nil
}

// ---- with/else (§8.2) ----

// compileWith: binds по порядку — значение в vReg, MATCHLOCAL+JMP на метку
// сбоя; затем тело (наследует d). На метке сбоя в vReg — первое несовпавшее
// значение: без else оно и есть результат (пропагация, не raise), с else —
// ветки compileCaseBranches, без совпадения — (:case_clause, val) (§10.4).
// Связывания binds видны binds ниже и телу, но не веткам else.
func (fc *funcCompiler) compileWith(we ast.WithExpr, d dest) error {
	mark := fc.nextReg
	vReg := fc.allocReg()

	fc.pushScope()
	var fails []int
	for _, it := range we.WithItems() {
		if err := fc.compileExpr(it.Expr, val(vReg)); err != nil {
			fc.popScope()
			return err
		}
		cp, err := fc.compilePattern(it.Pattern)
		if err != nil {
			fc.popScope()
			return err
		}
		patIdx := fc.chunk.AddPattern(cp)

		fc.pos = posOf(it.Pattern)
		fc.emit(vm.ABx(vm.MATCHLOCAL, vReg, patIdx))
		fails = append(fails, fc.emitJump(vm.JMP, 0))
	}

	var body ast.Expr = we.WithBody()
	if we.WithBody() == nil {
		body = ast.NewBlockStmt(nil, 0, 0)
	}
	if err := fc.compileBranch(body, d); err != nil {
		fc.popScope()
		return err
	}
	jEnd := -1
	if !d.tail {
		jEnd = fc.emitJump(vm.JMP, 0)
	}
	fc.popScope()
	fc.releaseToMark(vReg + 1)

	for _, j := range fails {
		fc.patchHere(j)
	}
	fc.pos = posOf(we)
	if elseBranches := we.WithElseBranches(); len(elseBranches) > 0 {
		branches := make([]ast.MatchBranchArg, 0, len(elseBranches))
		for _, eb := range elseBranches {
			branches = append(branches, ast.MatchBranchArg(eb))
		}
		if err := fc.compileCaseBranches(vReg, branches, posOf(we), d); err != nil {
			return err
		}
	} else {
		fc.finish(d, vReg)
	}

	if jEnd >= 0 {
		fc.patchHere(jEnd)
	}
	fc.releaseToMark(mark)
	return nil
}

// ---- recv (§7) ----

func (fc *funcCompiler) compileRecv(re ast.RecvExpr, d dest) error {
	mark := fc.nextReg
	mReg := fc.allocReg()

	afterTime := re.RecvAfterTime()
	afterBody := re.RecvAfterBody()
	hasAfter := afterBody != nil

	if hasAfter && afterTime != nil {
		tMark := fc.nextReg
		tReg := fc.allocReg()
		if err := fc.compileExpr(afterTime, val(tReg)); err != nil {
			return err
		}
		fc.emit(vm.ABC(vm.RECVTIMER, tReg, 0, 0))
		fc.releaseToMark(tMark)
	}

	recvTakeAt := fc.emit(vm.AsBx(vm.RECVTAKE, mReg, 0))

	var endJumps []int
	for _, br := range re.RecvBranches() {
		fc.pushScope()

		cp, err := fc.compilePattern(br.Pattern)
		if err != nil {
			fc.popScope()
			return err
		}
		patIdx := fc.chunk.AddPattern(cp)

		fc.emit(vm.ABx(vm.MATCHLOCAL, mReg, patIdx))
		fails := []int{fc.emitJump(vm.JMP, 0)}

		// Guard видит связывания паттерна; ложный — к следующей ветке (§12.4).
		if br.Guard != nil {
			gMark := fc.nextReg
			g := fc.allocReg()
			if err := fc.compileExpr(br.Guard, val(g)); err != nil {
				fc.popScope()
				return err
			}
			fc.pos = posOf(br.Guard)
			fails = append(fails, fc.emitJump(vm.JMPIFNOT, g))
			fc.releaseToMark(gMark)
		}

		if err := fc.compileBranch(br.Body, d); err != nil {
			fc.popScope()
			return err
		}

		jEnd := -1
		if !d.tail {
			jEnd = fc.emitJump(vm.JMP, 0)
		}

		for _, j := range fails {
			fc.patchHere(j)
		}
		if jEnd >= 0 {
			endJumps = append(endJumps, jEnd)
		}
		fc.popScope()
	}

	if elseBody := re.RecvElseBody(); elseBody != nil {
		fc.pushScope()
		if elseName := re.RecvElseName(); elseName != "" {
			// D-3: алиас имени else на регистр сообщения.
			fc.bindLocal(elseName, mReg)
		}
		if err := fc.compileBranch(elseBody, d); err != nil {
			fc.popScope()
			return err
		}
		if !d.tail {
			jEnd := fc.emitJump(vm.JMP, 0)
			endJumps = append(endJumps, jEnd)
		}
		fc.popScope()
	} else {
		// :recv_clause, msg → raise
		rclauseIdx := fc.konst(runtime.Atom("recv_clause"))
		w0 := fc.allocReg()
		w1 := fc.allocReg()
		fc.emit(vm.ABx(vm.LOADK, w0, rclauseIdx))
		fc.emit(vm.ABC(vm.MOVE, w1, mReg, 0))
		fc.emit(vm.ABC(vm.TUPLE, w0, w0, 2))
		fc.emit(vm.ABC(vm.RAISE, w0, 0, 0))
	}

	afterAt := len(fc.chunk.Code)
	if hasAfter {
		if err := fc.compileBranch(afterBody, d); err != nil {
			return err
		}
		if !d.tail {
			jEnd := fc.emitJump(vm.JMP, 0)
			endJumps = append(endJumps, jEnd)
		}
	}

	if hasAfter {
		sbx := afterAt - (recvTakeAt + 1)
		fc.chunk.Code[recvTakeAt] = vm.AsBx(vm.RECVTAKE, mReg, sbx)
	}

	endAt := len(fc.chunk.Code)
	for _, j := range endJumps {
		if err := fc.chunk.PatchJump(j, endAt); err != nil {
			fc.fail("patch: %v", err)
		}
	}

	fc.releaseToMark(mark)
	return nil
}

// ---- range / index ----

func (fc *funcCompiler) compileRange(re ast.RangeExpr, d dest) error {
	mark := fc.nextReg
	dst := fc.destReg(d)

	startReg, err := fc.operandInto(re.RangeStart(), dst)
	if err != nil {
		return err
	}
	endReg, err := fc.operand(re.RangeEnd())
	if err != nil {
		return err
	}

	fc.pos = posOf(re)
	fc.emit(vm.ABC(vm.RANGE, dst, startReg, endReg))
	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

func (fc *funcCompiler) compileIndex(ie ast.IndexExpr, d dest) error {
	mark := fc.nextReg
	dst := fc.destReg(d)

	objReg, err := fc.operandInto(ie.Obj(), dst)
	if err != nil {
		return err
	}
	idxReg, err := fc.operand(ie.Index())
	if err != nil {
		return err
	}

	fc.pos = posOf(ie)
	fc.emit(vm.ABC(vm.INDEX, dst, objReg, idxReg))
	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

// ---- lambda / closure ----
