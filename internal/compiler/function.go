package compiler

import (
	"fmt"
	"strings"

	"github.com/it1ro/brig-lang/internal/ast"
	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/vm"
)

func (c *Compiler) compileNamedFn(name string, clauses []ast.FnClauseArg) (*vm.Function, error) {
	fc := c.newFuncCompiler(nil)
	fc.prefix = name + c.gen + "$"
	if err := fc.compileClauses(clauses, nil); err != nil {
		return nil, err
	}
	return fc.function(name), nil
}

// function собирает vm.Function из скомпилированного чанка fn.
func (fc *funcCompiler) function(name string) *vm.Function {
	fc.chunk.NumRegs = fc.maxReg
	arity := fc.chunk.NumParams
	if fc.chunk.Variadic {
		arity = -1
	}
	return &vm.Function{Name: name, Arity: arity, Chunk: fc.chunk}
}

func (c *Compiler) compileBlock(name string, params []string, stmts []ast.Stmt) (*vm.Function, error) {
	fc := c.newFuncCompiler(nil)
	fc.prefix = name + "$"

	fc.chunk.NumParams = len(params)
	for i, p := range params {
		r := fc.allocReg()
		if r != i {
			return nil, fmt.Errorf("internal: block param %d in r%d", i, r)
		}
		fc.bindLocal(p, i)
	}

	if len(stmts) == 0 {
		scratch := fc.allocReg()
		if err := fc.loadUnit(dest{reg: scratch, tail: true}); err != nil {
			return nil, err
		}
	} else {
		scratch := fc.allocReg()
		if err := fc.compileStmts(stmts, dest{reg: scratch, tail: true}); err != nil {
			return nil, err
		}
	}
	fc.chunk.NumRegs = fc.maxReg

	return &vm.Function{Name: name, Arity: len(params), Chunk: fc.chunk}, nil
}

// clauseVariadic — оканчивается ли клоз `..name`; спред не в конце — ошибка.
func clauseVariadic(params []ast.Pattern) (bool, error) {
	variadic := false
	for i, p := range params {
		if _, ok := p.(ast.SpreadPattern); ok {
			if i != len(params)-1 {
				return false, fmt.Errorf("variadic parameter %q must be last", p)
			}
			variadic = true
		}
	}
	return variadic, nil
}

// isIdentParam: LOWER_IDENT (с опциональным trailing `?`), не true/false.
func isIdentParam(p string) bool {
	if p == "" || p[0] < 'a' || p[0] > 'z' || p == "true" || p == "false" {
		return false
	}
	for i := 1; i < len(p); i++ {
		c := p[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_':
		case c == '?' && i == len(p)-1:
		default:
			return false
		}
	}
	return true
}

// clausesShape — форма чанка мультиклозной fn: fixed — минимум фиксированных
// параметров по клозам, variadic — есть ли клоз с `..name`. Разные арности
// допустимы только при наличии variadic-клоза; тогда все аргументы сверх
// fixed приходят в rest-списке, а клозы разбирают его list-паттерном.
func clausesShape(clauses []ast.FnClauseArg) (fixed int, variadic bool, err error) {
	fixed = -1
	arity := -1
	for _, cl := range clauses {
		v, verr := clauseVariadic(cl.Params)
		if verr != nil {
			return 0, false, verr
		}
		k := len(cl.Params)
		if v {
			k--
			variadic = true
		}
		if fixed < 0 || k < fixed {
			fixed = k
		}
		if arity >= 0 && len(cl.Params) != arity {
			arity = -2
		} else if arity == -1 {
			arity = len(cl.Params)
		}
	}
	if !variadic && arity == -2 {
		return 0, false, fmt.Errorf("клозы разной арности без variadic")
	}
	return fixed, variadic, nil
}

// compileClauses компилирует клозы fn (§6.1). Параметры лежат в
// r0..n-1, за ними — скрытые параметры захвата caps (лямбда-лифтинг
// локальной fn). Клозы проверяются по порядку: ident, `..name` и `_`
// связываются без проверки, прочие паттерны — MATCHLOCAL, затем guard
// (JMPIFNOT). Если ни один не подошёл — raise (:function_clause, [args]);
// после неопровержимого последнего клоза raise не эмитится.
func (fc *funcCompiler) compileClauses(clauses []ast.FnClauseArg, caps []upvalueInfo) error {
	if len(clauses) == 0 {
		return fmt.Errorf("нет клозов")
	}
	fixed, variadic, err := clausesShape(clauses)
	if err != nil {
		return err
	}
	// Регистры: [caps (у variadic)] fixed… [rest] [caps (иначе)].
	base, n := 0, fixed
	if variadic {
		base, n = len(caps), fixed+1
		fc.capBase = 0
	} else {
		fc.capBase = n
	}
	fc.caps = caps

	fc.chunk.Variadic = variadic
	fc.chunk.NumParams = n + len(caps)
	for i := 0; i < n+len(caps); i++ {
		if r := fc.allocReg(); r != i {
			return fmt.Errorf("internal: param %d in r%d", i, r)
		}
	}
	for i, c := range caps {
		if c.name != "" {
			fc.bindLocal(c.name, fc.capBase+i)
		}
	}

	var fails []int
	for _, cl := range clauses {
		if fails, err = fc.compileOneClause(cl, base, fixed, variadic); err != nil {
			return err
		}
		here := len(fc.chunk.Code)
		for _, j := range fails {
			if perr := fc.chunk.PatchJump(j, here); perr != nil {
				fc.fail("patch: %v", perr)
			}
		}
	}
	if len(fails) > 0 {
		fc.raiseFunctionClause(clauses[0], base, n)
	}
	return nil
}

// compileOneClause компилирует один клоз и возвращает его fail-переходы
// (к следующему клозу). Пустой список — клоз неопровержим.
func (fc *funcCompiler) compileOneClause(cl ast.FnClauseArg, base, fixed int, variadic bool) ([]int, error) {
	fc.pushScope()
	defer fc.popScope()

	var fails []int
	for i, p := range cl.Params[:fixed] {
		switch x := p.(type) {
		case ast.IdentPattern:
			if isIdentParam(x.IdentName()) {
				fc.bindLocal(x.IdentName(), base+i)
				continue
			}
		case ast.PatternWildcard:
			if p.String() == "_" {
				continue
			}
		}
		cp, err := fc.compilePattern(p)
		if err != nil {
			return nil, err
		}
		patIdx := fc.chunk.AddPattern(cp)
		fc.pos = posOf(p)
		fc.emit(vm.ABx(vm.MATCHLOCAL, base+i, patIdx))
		fails = append(fails, fc.emitJump(vm.JMP, 0))
	}
	if variadic {
		jump, err := fc.bindRestTail(cl, base, fixed)
		if err != nil {
			return nil, err
		}
		if jump >= 0 {
			fails = append(fails, jump)
		}
	}
	if cl.Guard != nil {
		mark := fc.nextReg
		g := fc.allocReg()
		if err := fc.compileExpr(cl.Guard, val(g)); err != nil {
			return nil, err
		}
		fc.pos = posOf(cl.Guard)
		fails = append(fails, fc.emitJump(vm.JMPIFNOT, g))
		fc.releaseToMark(mark)
	}

	scratch := fc.allocReg()
	if cl.Body == nil || len(cl.Body.Body()) == 0 {
		return fails, fc.loadUnit(dest{reg: scratch, tail: true})
	}
	return fails, fc.compileStmts(cl.Body.Body(), dest{reg: scratch, tail: true})
}

// bindRestTail связывает параметры клоза сверх fixed с rest-списком в
// r[base+fixed]: голый `..name` — просто имя, иначе list-паттерн
// (точная длина без `..`). Возвращает fail-переход или -1.
func (fc *funcCompiler) bindRestTail(cl ast.FnClauseArg, base, fixed int) (int, error) {
	tail := cl.Params[fixed:]
	elems, restName, hasRest := tail, "", false
	if n := len(tail); n > 0 {
		if sp, ok := tail[n-1].(ast.SpreadPattern); ok {
			elems, restName, hasRest = tail[:n-1], sp.SpreadName(), true
		}
	}
	if len(elems) == 0 && hasRest {
		fc.bindLocal(restName, base+fixed)
		return -1, nil
	}
	cp, err := fc.compilePattern(ast.NewListPattern(elems, hasRest, restName, 0, 0))
	if err != nil {
		return -1, err
	}
	switch {
	case len(tail) > 0:
		fc.pos = posOf(tail[0])
	case len(cl.Params) > 0:
		fc.pos = posOf(cl.Params[0])
	}
	fc.emit(vm.ABx(vm.MATCHLOCAL, base+fixed, fc.chunk.AddPattern(cp)))
	return fc.emitJump(vm.JMP, 0), nil
}

// raiseFunctionClause — (:function_clause, [arg0, …]) (§6.1, §5.3).
// Аргументы уже лежат подряд в r[base]..r[base+arity-1]. У variadic последний элемент
// списка — сам rest-список: конкатенации в байткоде нет.
func (fc *funcCompiler) raiseFunctionClause(first ast.FnClauseArg, base, arity int) {
	switch {
	case len(first.Params) > 0:
		fc.pos = posOf(first.Params[0])
	case first.Guard != nil:
		fc.pos = posOf(first.Guard)
	}
	w := fc.allocReg()
	fc.emit(vm.ABx(vm.LOADK, w, fc.konst(runtime.Atom("function_clause"))))
	l := fc.allocReg()
	fc.emit(vm.ABC(vm.LIST, l, base, arity))
	fc.emit(vm.ABC(vm.TUPLE, w, w, 2))
	fc.emit(vm.ABC(vm.RAISE, w, 0, 0))
}

func localClauses(cs []ast.LocalFnClauseArg) []ast.FnClauseArg {
	out := make([]ast.FnClauseArg, len(cs))
	for i, c := range cs {
		out[i] = ast.FnClauseArg{Guard: c.Guard, Params: c.Params, Body: c.Body}
	}
	return out
}

// ---- лямбда-лифтинг локальных fn (T-51) ----

// declareLocalFns регистрирует имена локальных fn блока до компиляции
// тел (взаимная рекурсия, §6.5) и вычисляет их захваты — скрытые
// параметры после объявленных. sema держит объявления в начале блока,
// так что видимые снаружи имена к этому моменту уже связаны.
//
// Захваты считаются фикспойнтом: вызов другой локальной fn блока
// (или её значение) передаёт и её захваты, поэтому они транзитивны.
// Итерация компилирует тела во временных funcCompiler и отбрасывает
// результат; новые upvalue дописываются в захваты (по связыванию, см.
// fetchCapture), наборы только растут.
func (fc *funcCompiler) declareLocalFns(stmts []ast.Stmt) error {
	var decls []ast.LocalFnDecl
	for _, s := range stmts {
		if lfd, ok := s.(ast.LocalFnDecl); ok {
			mangled := fc.prefix + lfd.FnName()
			fc.localFns[lfd.FnName()] = mangled
			arity, variadic, err := clausesShape(localClauses(lfd.Clauses()))
			if err != nil {
				return wrapCtx(posOf(lfd), err, "local fn %s", lfd.FnName())
			}
			fc.compiler.lifted[mangled] = &liftedFn{arity: arity, variadic: variadic}
			decls = append(decls, lfd)
		}
	}
	for changed := len(decls) > 0; changed; {
		changed = false
		for _, d := range decls {
			mangled := fc.localFns[d.FnName()]
			lf := fc.compiler.lifted[mangled]
			probe := fc.compiler.newFuncCompiler(fc)
			probe.prefix = mangled + "$"
			if err := probe.compileClauses(localClauses(d.Clauses()), lf.caps); err != nil {
				return wrapCtx(posOf(d), err, "local fn %s", d.FnName())
			}
			for _, uv := range probe.upvalues {
				i, ok := findCapture(lf.caps, uv)
				switch {
				case !ok:
					lf.caps = append(lf.caps, uv)
				case lf.caps[i].name == "" && uv.name != "":
					lf.caps[i].name = uv.name
				default:
					continue
				}
				changed = true
			}
		}
	}
	return nil
}

// resolveLocalFn — mangled-имя локальной fn и её владелец (fn, в
// которой она объявлена) по правилам compileVar: по уровням parent,
// на каждом локаль уровня затеняет localFns этого уровня и предков
// (T-56).
func (fc *funcCompiler) resolveLocalFn(name string) (string, *funcCompiler, bool) {
	for p := fc; p != nil; p = p.parent {
		if _, ok := p.resolveLocal(name); ok {
			return "", nil, false
		}
		if mangled, ok := p.localFns[name]; ok {
			return mangled, p, true
		}
	}
	return "", nil, false
}

func (fc *funcCompiler) compileStmts(stmts []ast.Stmt, d dest) error {
	if len(stmts) == 0 {
		return fc.loadUnit(d)
	}

	// Local fn names first — для взаимной рекурсии (§6.5).
	if err := fc.declareLocalFns(stmts); err != nil {
		return err
	}

	for i, s := range stmts {
		var sd dest
		if i == len(stmts)-1 {
			sd = d
		} else {
			sd = discard
		}
		if err := fc.compileStmt(s, sd); err != nil {
			return err
		}
	}
	return nil
}

func (fc *funcCompiler) compileStmt(s ast.Stmt, d dest) (err error) {
	if p := posOf(s); p.Line > 0 {
		saved := fc.pos
		fc.pos = p
		defer func() {
			err = fc.locate(err)
			fc.pos = saved
		}()
	}
	switch st := s.(type) {
	case ast.LetBind:
		return fc.compileLetBind(st, d)
	case ast.ExprStmt:
		return fc.compileExpr(st.ExprValue(), d)
	case ast.LocalFnDecl:
		return fc.compileLocalFn(st, d)
	}
	return fmt.Errorf("срез: неподдерживаемый стейтмент %T", s)
}

func (fc *funcCompiler) compileLetBind(st ast.LetBind, d dest) error {
	ip, ok := st.Pat().(ast.IdentPattern)
	if !ok {
		return fc.compilePatternBind(st, d, nil)
	}
	name := ip.IdentName()
	r := fc.allocReg()
	if err := fc.compileExpr(st.Val(), val(r)); err != nil {
		return err
	}
	fc.bindLocal(name, r)
	// Значение let-стейтмента — () (совместимо с регистровой VM).
	return fc.loadUnit(d)
}

// compilePatternBind — связывание `pattern = expr` (§5.1): значение в
// vReg, MATCHLOCAL связывает имена паттерна в текущей области;
// несовпадение — raise (:badmatch, v), v — значение правой части (§10.4).
// slots — предаллоцированные регистры имён (trap с ensure), иначе nil.
func (fc *funcCompiler) compilePatternBind(st ast.LetBind, d dest, slots map[string]int) error {
	vReg := fc.allocReg()
	if err := fc.compileExpr(st.Val(), val(vReg)); err != nil {
		return err
	}
	fc.patSlots = slots
	cp, err := fc.compilePattern(st.Pat())
	fc.patSlots = nil
	if err != nil {
		return err
	}
	patIdx := fc.chunk.AddPattern(cp)

	fc.emit(vm.ABx(vm.MATCHLOCAL, vReg, patIdx))
	jFail := fc.emitJump(vm.JMP, 0)
	jOk := fc.emitJump(vm.JMP, 0)

	fc.patchHere(jFail)
	mark := fc.nextReg
	w0 := fc.allocReg()
	w1 := fc.allocReg()
	fc.emit(vm.ABx(vm.LOADK, w0, fc.konst(runtime.Atom("badmatch"))))
	fc.emit(vm.ABC(vm.MOVE, w1, vReg, 0))
	fc.emit(vm.ABC(vm.TUPLE, w0, w0, 2))
	fc.emit(vm.ABC(vm.RAISE, w0, 0, 0))
	fc.releaseToMark(mark)

	fc.patchHere(jOk)
	return fc.loadUnit(d)
}

func (fc *funcCompiler) compileLocalFn(decl ast.LocalFnDecl, d dest) error {
	name := decl.FnName()
	mangled, ok := fc.localFns[name]
	if !ok {
		mangled = fc.prefix + name
		fc.localFns[name] = mangled
	}

	// Захваты — скрытые параметры после объявленных (лямбда-лифтинг).
	var caps []upvalueInfo
	if lf := fc.compiler.lifted[mangled]; lf != nil {
		caps = lf.caps
	}
	child := fc.compiler.newFuncCompiler(fc)
	child.prefix = mangled + "$"
	if err := child.compileClauses(localClauses(decl.Clauses()), caps); err != nil {
		return wrapCtx(posOf(decl), err, "local fn %s", name)
	}
	// fn кладётся как глобальная Function: upvalue здесь — захват мимо
	// фикспойнта declareLocalFns, в рантайме это `internal: upvalue`.
	if len(child.upvalues) > 0 {
		fc.fail("internal: local fn %s: capture %q outside lifted params", name, child.upvalues[0].name)
	}
	fc.compiler.image.Functions[mangled] = child.function(mangled)

	// Значение local fn — () (совместимо с регистровой VM).
	return fc.loadUnit(d)
}

// ---- expressions ----

func (fc *funcCompiler) compileLambda(name string, params []string, body ast.Expr, d dest) error {
	child := fc.compiler.newFuncCompiler(fc)
	// Уникальный префикс на лямбду: иначе одноимённые локальные fn в
	// разных лямбдах одной функции перезаписывают друг друга в image.Functions (A-F6).
	child.prefix = fmt.Sprintf("%slambda$%d$", fc.prefix, fc.lambdaSeq)
	fc.lambdaSeq++

	child.chunk.NumParams = len(params)
	for i, p := range params {
		r := child.allocReg()
		if r != i {
			fc.fail("lambda: param %d in r%d", i, r)
		}
		if name, ok := strings.CutPrefix(p, ".."); ok {
			child.chunk.Variadic = true
			p = name
		}
		child.bindLocal(p, i)
	}

	if blk, ok := body.(*ast.BlockStmt); ok {
		stmts := blk.Body()
		if len(stmts) == 0 {
			scratch := child.allocReg()
			if err := child.loadUnit(dest{reg: scratch, tail: true}); err != nil {
				return err
			}
		} else {
			scratch := child.allocReg()
			if err := child.compileStmts(stmts, dest{reg: scratch, tail: true}); err != nil {
				return err
			}
		}
	} else {
		scratch := child.allocReg()
		if err := child.compileExpr(body, dest{reg: scratch, tail: true}); err != nil {
			return err
		}
	}
	child.chunk.NumRegs = child.maxReg

	arity := len(params)
	if child.chunk.Variadic {
		arity = -1
	}
	return fc.emitClosure(child, name, arity, d)
}

// compileLambdaFull компилирует полную лямбду fn (params) -> body с
// параметрами-полными-паттернами (§6.3, T-141): образец — compileOneClause,
// но лямбда — единственный "клоз", поэтому несовпадение паттерна не
// прыгает к следующему клозу, а сразу даёт ловимый (:function_clause, args)
// (§5.3), как многоклозная fn после последнего клоза.
func (fc *funcCompiler) compileLambdaFull(name string, params []ast.Pattern, body *ast.BlockStmt, d dest) error {
	variadic, err := clauseVariadic(params)
	if err != nil {
		return err
	}
	fixed := len(params)
	if variadic {
		fixed--
	}
	n := fixed
	if variadic {
		n++
	}

	child := fc.compiler.newFuncCompiler(fc)
	child.prefix = fmt.Sprintf("%slambda$%d$", fc.prefix, fc.lambdaSeq)
	fc.lambdaSeq++

	child.chunk.Variadic = variadic
	child.chunk.NumParams = n
	for i := 0; i < n; i++ {
		r := child.allocReg()
		if r != i {
			fc.fail("lambda: param %d in r%d", i, r)
		}
	}

	var fails []int
	for i, p := range params[:fixed] {
		switch x := p.(type) {
		case ast.IdentPattern:
			if isIdentParam(x.IdentName()) {
				child.bindLocal(x.IdentName(), i)
				continue
			}
		case ast.PatternWildcard:
			if p.String() == "_" {
				continue
			}
		}
		cp, err := child.compilePattern(p)
		if err != nil {
			return err
		}
		patIdx := child.chunk.AddPattern(cp)
		child.pos = posOf(p)
		child.emit(vm.ABx(vm.MATCHLOCAL, i, patIdx))
		fails = append(fails, child.emitJump(vm.JMP, 0))
	}
	if variadic {
		jump, err := child.bindRestTail(ast.FnClauseArg{Params: params}, 0, fixed)
		if err != nil {
			return err
		}
		if jump >= 0 {
			fails = append(fails, jump)
		}
	}

	// Успешный матч продолжает выполнение прямо в тело (падает мимо JMP,
	// см. компилятор MATCHLOCAL). Raise — после тела, недостижим иначе как
	// через fail-переходы (тело всегда завершается RETURN).
	if body == nil || len(body.Body()) == 0 {
		scratch := child.allocReg()
		if err := child.loadUnit(dest{reg: scratch, tail: true}); err != nil {
			return err
		}
	} else {
		scratch := child.allocReg()
		if err := child.compileStmts(body.Body(), dest{reg: scratch, tail: true}); err != nil {
			return err
		}
	}

	if len(fails) > 0 {
		here := len(child.chunk.Code)
		for _, j := range fails {
			if perr := child.chunk.PatchJump(j, here); perr != nil {
				child.fail("patch: %v", perr)
			}
		}
		child.raiseFunctionClause(ast.FnClauseArg{Params: params}, 0, n)
	}
	child.chunk.NumRegs = child.maxReg

	arity := n
	if variadic {
		arity = -1
	}
	return fc.emitClosure(child, name, arity, d)
}

// emitClosure — общий хвост compileLambda/compileLambdaFull: константа
// Function, MAKECLOSURE с захватами по upvalues скомпилированного child.
func (fc *funcCompiler) emitClosure(child *funcCompiler, name string, arity int, d dest) error {
	fnName := name
	if fnName == "" {
		fnName = child.prefix
	}
	fn := &vm.Function{Name: fnName, Arity: arity, Chunk: child.chunk}

	fnVal := vm.FuncValue(fn)
	fnIdx := fc.chunk.AddConstant(fnVal)

	mark := fc.nextReg
	dst := fc.destReg(d)

	base := fc.allocReg()
	fc.emit(vm.ABx(vm.LOADK, base, fnIdx))

	for j, uv := range child.upvalues {
		r := fc.allocReg()
		if r != base+1+j {
			fc.fail("closure: upvalue %d in r%d, want r%d", j, r, base+1+j)
		}
		if uv.isLocal {
			fc.emit(vm.ABC(vm.MOVE, r, uv.index, 0))
		} else {
			fc.emit(vm.ABC(vm.GETUPVAL, r, uv.index, 0))
		}
	}

	fc.emit(vm.ABC(vm.MAKECLOSURE, dst, base, len(child.upvalues)))
	fc.finish(d, dst)
	fc.releaseToMark(mark)
	return nil
}

// ---- patterns ----
