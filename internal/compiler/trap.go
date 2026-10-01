package compiler

import (
	"github.com/it1ro/brig-lang/internal/ast"
	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/vm"
)

func (fc *funcCompiler) compileTrap(te ast.TrapExpr, d dest) error {
	if inline := te.TrapInline(); inline != nil {
		return fc.compileInlineTrap(inline, d)
	}
	return fc.compileBlockTrap(te, d)
}

func (fc *funcCompiler) compileInlineTrap(inner ast.Expr, d dest) error {
	mark := fc.nextReg
	dst := fc.destReg(d)

	fc.trapDepth++
	begin := fc.emitJump(vm.TRAPBEGIN, dst)

	if err := fc.compileExpr(inner, val(dst)); err != nil {
		return err
	}

	fc.emit(vm.ABC(vm.TRAPEND, 0, 0, 0))
	fc.trapDepth--
	fc.emit(vm.ABC(vm.MAKEOK, dst, dst, 0))
	jEnd := fc.emitJump(vm.JMP, 0)

	fc.patchHere(begin)
	fc.emit(vm.ABC(vm.MAKEERROR, dst, dst, 0))
	fc.patchHere(jEnd)

	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

func (fc *funcCompiler) compileBlockTrap(te ast.TrapExpr, d dest) error {
	var stmts []ast.Stmt
	switch body := te.TrapBody().(type) {
	case *ast.BlockStmt:
		stmts = body.Body()
	case nil:
	default:
		stmts = []ast.Stmt{body}
	}
	ensures := te.TrapEnsures()

	if len(ensures) == 0 {
		return fc.compileTrapNoEnsure(stmts, d)
	}
	return fc.compileTrapWithEnsure(stmts, ensures, d)
}

func (fc *funcCompiler) compileTrapNoEnsure(stmts []ast.Stmt, d dest) error {
	mark := fc.nextReg
	dst := fc.destReg(d)

	fc.trapDepth++
	begin := fc.emitJump(vm.TRAPBEGIN, dst)

	fc.pushScope()
	if err := fc.compileStmts(stmts, val(dst)); err != nil {
		fc.popScope()
		return err
	}
	fc.popScope()

	fc.emit(vm.ABC(vm.TRAPEND, 0, 0, 0))
	fc.trapDepth--
	fc.emit(vm.ABC(vm.MAKEOK, dst, dst, 0))
	jEnd := fc.emitJump(vm.JMP, 0)

	fc.patchHere(begin)
	fc.emit(vm.ABC(vm.MAKEERROR, dst, dst, 0))
	fc.patchHere(jEnd)

	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

// compileTrapWithEnsure — схема §5 с флаг-регистром (D-5) и
// per-ensure флагами регистрации (I-F5 / T-37).
//
// Ensure компилируются в области тела (до popScope) и исполняются
// только если до их текстовой точки дошли (LOADK true → JMPIFNOT).
func (fc *funcCompiler) compileTrapWithEnsure(stmts []ast.Stmt, ensures []ast.Expr, d dest) error {
	mark := fc.nextReg
	dst := fc.destReg(d)
	eReg := fc.allocReg()
	fReg := fc.allocReg()

	falseIdx := fc.konst(runtime.Bool(false))
	trueIdx := fc.konst(runtime.Bool(true))

	// Преинициализация dst: тело пишет dst на body-пути, но handler-путь
	// (BH) заходит в ENS без записи dst. Линейный def-assignment в
	// vm.Verify видит пересечение состояний → dst "undefined" в
	// MAKEOK. Значение на error-пути всё равно перезаписывается
	// MAKEERROR, так что семантика не меняется.
	unitIdx := fc.konst(runtime.Unit)
	fc.emit(vm.ABx(vm.LOADK, dst, unitIdx))

	fc.emit(vm.ABx(vm.LOADK, eReg, unitIdx))

	fc.emit(vm.ABx(vm.LOADK, fReg, falseIdx))

	// Флаги регистрации ensure: false до тела (Verify видит def на
	// каждом пути), true — в текстовой точке внутри тела.
	regFlags := make([]int, len(ensures))
	for i := range ensures {
		regFlags[i] = fc.allocReg()
		fc.emit(vm.ABx(vm.LOADK, regFlags[i], falseIdx))
	}

	// Локали тела, которые ensure может прочитать после join с BH:
	// Verify считает живыми на handler-пути только регистры до
	// TRAPBEGIN (как dst выше). Предаллоцируем слоты let-связей.
	letRegs := make(map[string]int)
	for _, s := range stmts {
		lb, ok := s.(ast.LetBind)
		if !ok {
			continue
		}
		for _, name := range patternNames(lb.Pat(), nil) {
			r := fc.allocReg()
			fc.emit(vm.ABx(vm.LOADK, r, unitIdx))
			letRegs[name] = r
		}
	}

	fc.trapDepth++
	// TRAPENSURE: unwind от exit (§12.7) входит в ensure, пропуская trap.
	bodyBegin := fc.emitJump(vm.TRAPENSURE, eReg)

	fc.pushScope()
	bd := val(dst)
	if d.tail && ensureTailEligible(stmts, ensures) {
		bd.ens = &ensTail{ensures: ensures, scopes: len(fc.scopes)}
	}
	if err := fc.compileTrapBodyWithEnsures(stmts, ensures, regFlags, letRegs, trueIdx, bd); err != nil {
		fc.popScope()
		return err
	}

	fc.emit(vm.ABC(vm.TRAPEND, 0, 0, 0))
	fc.trapDepth--
	jEns := fc.emitJump(vm.JMP, 0)

	fc.patchHere(bodyBegin)
	fc.emit(vm.ABx(vm.LOADK, fReg, trueIdx))

	fc.patchHere(jEns)

	// ENS: LIFO; каждый ensure — только если зарегистрирован.
	for i := len(ensures) - 1; i >= 0; i-- {
		ens := ensures[i]

		jSkip := fc.emitJump(vm.JMPIFNOT, regFlags[i])

		ensMark := fc.nextReg
		fc.trapDepth++
		// exit посреди ensure: оставшиеся ensure блока исполняются.
		ehBegin := fc.emitJump(vm.TRAPENSURE, eReg)

		sReg := fc.allocReg()
		if err := fc.compileExpr(ens, val(sReg)); err != nil {
			fc.popScope()
			return err
		}
		fc.emit(vm.ABC(vm.TRAPEND, 0, 0, 0))
		fc.trapDepth--
		fc.releaseToMark(ensMark)

		jNext := fc.emitJump(vm.JMP, 0)

		fc.patchHere(ehBegin)
		fc.emit(vm.ABx(vm.LOADK, fReg, trueIdx))

		fc.patchHere(jNext)
		fc.patchHere(jSkip)
	}
	// Конец ensure-блока: unwind от exit продолжается отсюда.
	fc.emit(vm.ABC(vm.ENSEND, 0, 0, 0))

	fc.popScope()

	jErr := fc.emitJump(vm.JMPIF, fReg)

	fc.emit(vm.ABC(vm.MAKEOK, dst, dst, 0))
	jEnd := fc.emitJump(vm.JMP, 0)

	fc.patchHere(jErr)
	fc.emit(vm.ABC(vm.MAKEERROR, dst, eReg, 0))

	fc.patchHere(jEnd)
	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

// ensureTailEligible — хвост тела trap с ensure может быть TAILCALLENS
// (doc 02 §5.1): тело не пусто и ни один ensure не стоит текстом после
// последнего стейтмента — иначе он регистрировался бы после вызова.
// Порядок — как у слияния в compileTrapBodyWithEnsures.
func ensureTailEligible(stmts []ast.Stmt, ensures []ast.Expr) bool {
	if len(stmts) == 0 {
		return false
	}
	last := stmts[len(stmts)-1]
	sLine, sCol := last.Pos(), last.End()
	for _, e := range ensures {
		eLine, eCol := e.Pos(), e.End()
		if eLine > sLine || (eLine == sLine && eCol >= sCol) {
			return false
		}
	}
	return true
}

// compileTrapBodyWithEnsures обходит стейтменты тела и точки регистрации
// ensure в текстовом порядке (Line, Col). Ensure-выражения здесь не
// компилируются — только LOADK true в их флаг-регистр.
func (fc *funcCompiler) compileTrapBodyWithEnsures(
	stmts []ast.Stmt, ensures []ast.Expr, regFlags []int, letRegs map[string]int, trueIdx int, d dest,
) error {
	if len(stmts) == 0 {
		for i := range ensures {
			fc.emit(vm.ABx(vm.LOADK, regFlags[i], trueIdx))
		}
		return fc.loadUnit(d)
	}

	if err := fc.declareLocalFns(stmts); err != nil {
		return err
	}

	type item struct {
		isEnsure bool
		idx      int
	}
	merged := make([]item, 0, len(stmts)+len(ensures))
	si, ei := 0, 0
	for si < len(stmts) || ei < len(ensures) {
		switch {
		case ei >= len(ensures):
			merged = append(merged, item{false, si})
			si++
		case si >= len(stmts):
			merged = append(merged, item{true, ei})
			ei++
		default:
			sLine, sCol := stmts[si].Pos(), stmts[si].End()
			eLine, eCol := ensures[ei].Pos(), ensures[ei].End()
			if eLine < sLine || (eLine == sLine && eCol < sCol) {
				merged = append(merged, item{true, ei})
				ei++
			} else {
				merged = append(merged, item{false, si})
				si++
			}
		}
	}

	lastStmt := len(stmts) - 1
	for _, it := range merged {
		if it.isEnsure {
			fc.emit(vm.ABx(vm.LOADK, regFlags[it.idx], trueIdx))
			continue
		}
		sd := discard
		if it.idx == lastStmt {
			sd = d
		}
		s := stmts[it.idx]
		if lb, ok := s.(ast.LetBind); ok {
			if err := fc.compileTrapLetBind(lb, sd, letRegs); err != nil {
				return err
			}
			continue
		}
		if err := fc.compileStmt(s, sd); err != nil {
			return err
		}
	}
	return nil
}

// compileTrapLetBind — как compileLetBind, но пишет в предаллоцированный
// слот (Verify: жив на BH-пути) при наличии в letRegs.
func (fc *funcCompiler) compileTrapLetBind(st ast.LetBind, d dest, letRegs map[string]int) error {
	ip, ok := st.Pat().(ast.IdentPattern)
	if !ok {
		return fc.compilePatternBind(st, d, letRegs)
	}
	name := ip.IdentName()
	r, ok := letRegs[name]
	if ok {
		delete(letRegs, name)
	} else {
		r = fc.allocReg()
	}
	if err := fc.compileExpr(st.Val(), val(r)); err != nil {
		return err
	}
	fc.bindLocal(name, r)
	return fc.loadUnit(d)
}

// ---- match (§8.3) ----
