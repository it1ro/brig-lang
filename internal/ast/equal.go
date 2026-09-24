package ast

// Equal — структурное равенство двух узлов AST.
// Используется в golden-тестах и round-trip проверках.
//
// Реализация сравнивает конкретные типы и известные поля напрямую,
// не рекурсируя через интерфейс Node. Для узлов, не покрытых явно,
// используется запасная проверка Pos/End/String.
func Equal(a, b Node) bool {
	if a == nil || b == nil {
		return a == b
	}
	return equalNodes(a, b)
}

func equalNodes(a, b Node) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Pos() != b.Pos() || a.End() != b.End() {
		return false
	}
	if a.String() != b.String() {
		return false
	}

	switch x := a.(type) {
	// ---- Expressions ----

	case *binaryExpr:
		y, ok := b.(*binaryExpr)
		if !ok || x.op != y.op {
			return false
		}
		if x.left.Pos() != y.left.Pos() || x.left.End() != y.left.End() {
			return false
		}
		return x.right.Pos() == y.right.Pos() && x.right.End() == y.right.End()

	case *unaryExpr:
		y, ok := b.(*unaryExpr)
		if !ok || x.op != y.op {
			return false
		}
		return x.expr.Pos() == y.expr.Pos() && x.expr.End() == y.expr.End()

	case *groupingExpr:
		y, ok := b.(*groupingExpr)
		if !ok {
			return false
		}
		return x.expr.Pos() == y.expr.Pos() && x.expr.End() == y.expr.End()

	case *literalExpr:
		y, ok := b.(*literalExpr)
		if !ok {
			return false
		}
		return x.value == y.value

	case *variableExpr:
		y, ok := b.(*variableExpr)
		if !ok {
			return false
		}
		return x.name == y.name

	case *assignExpr:
		y, ok := b.(*assignExpr)
		if !ok || x.name != y.name {
			return false
		}
		return x.value.Pos() == y.value.Pos() && x.value.End() == y.value.End()

	case *callExpr:
		y, ok := b.(*callExpr)
		if !ok || len(x.args) != len(y.args) {
			return false
		}
		if x.callee.Pos() != y.callee.Pos() || x.callee.End() != y.callee.End() {
			return false
		}
		for i := range x.args {
			if x.args[i].Pos() != y.args[i].Pos() || x.args[i].End() != y.args[i].End() {
				return false
			}
		}
		return true

	case *pipeExpr:
		y, ok := b.(*pipeExpr)
		if !ok || len(x.args) != len(y.args) {
			return false
		}
		if x.expr.Pos() != y.expr.Pos() || x.expr.End() != y.expr.End() {
			return false
		}
		if x.callee.Pos() != y.callee.Pos() || x.callee.End() != y.callee.End() {
			return false
		}
		for i := range x.args {
			if x.args[i].Pos() != y.args[i].Pos() || x.args[i].End() != y.args[i].End() {
				return false
			}
		}
		return true

	case *memberExpr:
		y, ok := b.(*memberExpr)
		if !ok || x.name != y.name {
			return false
		}
		return x.obj.Pos() == y.obj.Pos() && x.obj.End() == y.obj.End()

	case *indexExpr:
		y, ok := b.(*indexExpr)
		if !ok {
			return false
		}
		if x.obj.Pos() != y.obj.Pos() || x.obj.End() != y.obj.End() {
			return false
		}
		return x.index.Pos() == y.index.Pos() && x.index.End() == y.index.End()

	case *ifExpr:
		y, ok := b.(*ifExpr)
		if !ok || len(x.elseIf) != len(y.elseIf) {
			return false
		}
		if x.cond.Pos() != y.cond.Pos() || x.cond.End() != y.cond.End() {
			return false
		}
		if x.thenBody.Pos() != y.thenBody.Pos() || x.thenBody.End() != y.thenBody.End() {
			return false
		}
		for i := range x.elseIf {
			xe := &x.elseIf[i]
			ye := &y.elseIf[i]
			if xe.cond.Pos() != ye.cond.Pos() || xe.cond.End() != ye.cond.End() {
				return false
			}
			if xe.thenBody.Pos() != ye.thenBody.Pos() || xe.thenBody.End() != ye.thenBody.End() {
				return false
			}
			if xe.elseBody.Pos() != ye.elseBody.Pos() || xe.elseBody.End() != ye.elseBody.End() {
				return false
			}
		}
		switch {
		case x.elseBody == nil && y.elseBody == nil:
			return true
		case x.elseBody == nil || y.elseBody == nil:
			return false
		default:
			return x.elseBody.Pos() == y.elseBody.Pos() &&
				x.elseBody.End() == y.elseBody.End()
		}

	case *matchExpr:
		y, ok := b.(*matchExpr)
		if !ok || len(x.branches) != len(y.branches) {
			return false
		}
		if x.expr.Pos() != y.expr.Pos() || x.expr.End() != y.expr.End() {
			return false
		}
		for i := range x.branches {
			xb := &x.branches[i]
			yb := &y.branches[i]
			if xb.pattern.Pos() != yb.pattern.Pos() || xb.pattern.End() != yb.pattern.End() {
				return false
			}
			if xb.expr.Pos() != yb.expr.Pos() || xb.expr.End() != yb.expr.End() {
				return false
			}
		}
		return true

	case *recvExpr:
		y, ok := b.(*recvExpr)
		if !ok || len(x.branches) != len(y.branches) {
			return false
		}
		for i := range x.branches {
			xb := &x.branches[i]
			yb := &y.branches[i]
			if xb.pattern.Pos() != yb.pattern.Pos() || xb.pattern.End() != yb.pattern.End() {
				return false
			}
			if xb.expr.Pos() != yb.expr.Pos() || xb.expr.End() != yb.expr.End() {
				return false
			}
		}
		if !equalOptionalExpr(x.elseBody, y.elseBody) {
			return false
		}
		return equalOptionalExpr(x.afterBody, y.afterBody)

	case *withExpr:
		y, ok := b.(*withExpr)
		if !ok || len(x.items) != len(y.items) {
			return false
		}
		for i := range x.items {
			xi := &x.items[i]
			yi := &y.items[i]
			if xi.pattern.Pos() != yi.pattern.Pos() || xi.pattern.End() != yi.pattern.End() {
				return false
			}
			if xi.expr.Pos() != yi.expr.Pos() || xi.expr.End() != yi.expr.End() {
				return false
			}
		}
		return equalOptionalExpr(x.elseBody, y.elseBody)

	case *trapExpr:
		y, ok := b.(*trapExpr)
		if !ok || len(x.ensures) != len(y.ensures) {
			return false
		}
		if !equalOptionalExpr(x.expr, y.expr) {
			return false
		}
		switch {
		case x.body == nil && y.body == nil:
			// ok
		case x.body == nil || y.body == nil:
			return false
		default:
			if x.body.stmt.Pos() != y.body.stmt.Pos() ||
				x.body.stmt.End() != y.body.stmt.End() {
				return false
			}
		}
		for i := range x.ensures {
			xe := &x.ensures[i]
			ye := &y.ensures[i]
			if xe.expr.Pos() != ye.expr.Pos() || xe.expr.End() != ye.expr.End() {
				return false
			}
		}
		return true

	case *lambdaShortExpr:
		y, ok := b.(*lambdaShortExpr)
		if !ok || x.param != y.param {
			return false
		}
		return x.body.Pos() == y.body.Pos() && x.body.End() == y.body.End()

	case *lambdaFullExpr:
		y, ok := b.(*lambdaFullExpr)
		if !ok || len(x.params) != len(y.params) {
			return false
		}
		for i := range x.params {
			if x.params[i] != y.params[i] {
				return false
			}
		}
		switch {
		case x.body == nil && y.body == nil:
			return true
		case x.body == nil || y.body == nil:
			return false
		default:
			return x.body.Pos() == y.body.Pos() && x.body.End() == y.body.End()
		}

	case *lambdaEmptyExpr:
		y, ok := b.(*lambdaEmptyExpr)
		if !ok {
			return false
		}
		return x.body.Pos() == y.body.Pos() && x.body.End() == y.body.End()

	case *rangeExpr:
		y, ok := b.(*rangeExpr)
		if !ok {
			return false
		}
		if x.start.Pos() != y.start.Pos() || x.start.End() != y.start.End() {
			return false
		}
		return x.end.Pos() == y.end.Pos() && x.end.End() == y.end.End()

	case *decimalExpr:
		y, ok := b.(*decimalExpr)
		if !ok {
			return false
		}
		return x.value == y.value

	case *bytesExpr:
		y, ok := b.(*bytesExpr)
		if !ok {
			return false
		}
		return x.value == y.value

	case *regexExpr:
		y, ok := b.(*regexExpr)
		if !ok {
			return false
		}
		return x.value == y.value

	case *atomExpr:
		y, ok := b.(*atomExpr)
		if !ok {
			return false
		}
		return x.ident == y.ident

	// ---- Statements ----

	case *letBind:
		y, ok := b.(*letBind)
		if !ok {
			return false
		}
		if x.pattern.Pos() != y.pattern.Pos() || x.pattern.End() != y.pattern.End() {
			return false
		}
		return x.value.Pos() == y.value.Pos() && x.value.End() == y.value.End()

	case *exprStmt:
		y, ok := b.(*exprStmt)
		if !ok {
			return false
		}
		return x.expr.Pos() == y.expr.Pos() && x.expr.End() == y.expr.End()

	case *localFnDecl:
		y, ok := b.(*localFnDecl)
		if !ok || len(x.clauses) != len(y.clauses) {
			return false
		}
		for i := range x.clauses {
			xc := &x.clauses[i]
			yc := &y.clauses[i]
			if xc.guard != yc.guard {
				return false
			}
			switch {
			case xc.body == nil && yc.body == nil:
				// ok
			case xc.body == nil || yc.body == nil:
				return false
			default:
				if xc.body.Pos() != yc.body.Pos() || xc.body.End() != yc.body.End() {
					return false
				}
			}
		}
		return true

	case *BlockStmt:
		y, ok := b.(*BlockStmt)
		if !ok || len(x.stmts) != len(y.stmts) {
			return false
		}
		for i := range x.stmts {
			if x.stmts[i].Pos() != y.stmts[i].Pos() ||
				x.stmts[i].End() != y.stmts[i].End() {
				return false
			}
		}
		return true

	// ---- Patterns ----

	case *wildcardPat:
		_, ok := b.(*wildcardPat)
		return ok

	case *identPat:
		y, ok := b.(*identPat)
		return ok && x.name == y.name

	case *literalPat:
		y, ok := b.(*literalPat)
		return ok && x.value == y.value

	case *constructorPat:
		y, ok := b.(*constructorPat)
		if !ok || x.name != y.name || len(x.fields) != len(y.fields) {
			return false
		}
		for i := range x.fields {
			if x.fields[i].pattern.Pos() != y.fields[i].pattern.Pos() ||
				x.fields[i].pattern.End() != y.fields[i].pattern.End() {
				return false
			}
		}
		return true

	case *asPat:
		y, ok := b.(*asPat)
		if !ok || x.ident != y.ident {
			return false
		}
		return x.pattern.Pos() == y.pattern.Pos() && x.pattern.End() == y.pattern.End()

	case *tuplePattern:
		y, ok := b.(*tuplePattern)
		if !ok || len(x.patterns) != len(y.patterns) {
			return false
		}
		for i := range x.patterns {
			if x.patterns[i].Pos() != y.patterns[i].Pos() ||
				x.patterns[i].End() != y.patterns[i].End() {
				return false
			}
		}
		return true

	case *listPattern:
		y, ok := b.(*listPattern)
		if !ok || x.hasRest != y.hasRest || x.restName != y.restName ||
			len(x.patterns) != len(y.patterns) {
			return false
		}
		for i := range x.patterns {
			if x.patterns[i].Pos() != y.patterns[i].Pos() ||
				x.patterns[i].End() != y.patterns[i].End() {
				return false
			}
		}
		return true

	case *mapPattern:
		y, ok := b.(*mapPattern)
		if !ok || len(x.pairs) != len(y.pairs) {
			return false
		}
		for i := range x.pairs {
			if x.pairs[i].key.Pos() != y.pairs[i].key.Pos() ||
				x.pairs[i].key.End() != y.pairs[i].key.End() {
				return false
			}
			if x.pairs[i].pat.Pos() != y.pairs[i].pat.Pos() ||
				x.pairs[i].pat.End() != y.pairs[i].pat.End() {
				return false
			}
		}
		return true

	case *recordPattern:
		y, ok := b.(*recordPattern)
		if !ok || x.typ != y.typ || len(x.fields) != len(y.fields) {
			return false
		}
		for i := range x.fields {
			if x.fields[i].name != y.fields[i].name {
				return false
			}
			if x.fields[i].pat.Pos() != y.fields[i].pat.Pos() ||
				x.fields[i].pat.End() != y.fields[i].pat.End() {
				return false
			}
		}
		return true
	}

	// Запасная проверка для типов и деклараций, у которых Pos/End/String
	// уже совпали выше: считаем равными (детальный разбор полей — на уровне
	// конкретных кейсов при необходимости).
	return true
}

// equalOptionalExpr — равенство двух Expr с учётом nil.
func equalOptionalExpr(a, b Expr) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	default:
		return a.Pos() == b.Pos() && a.End() == b.End()
	}
}
