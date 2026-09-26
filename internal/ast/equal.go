package ast

// Equal — структурное равенство двух узлов AST.
//
// Pos/End сознательно НЕ сравниваются: они отражают исходный текст и
// меняются при форматировании. Round-trip проверяет структуру, а не позиции.
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
	switch x := a.(type) {
	case *Program:
		y, ok := b.(*Program)
		if !ok || x.Module != y.Module ||
			len(x.Decls) != len(y.Decls) ||
			len(x.Stmts) != len(y.Stmts) {
			return false
		}
		for i := range x.Decls {
			if !equalNodes(x.Decls[i], y.Decls[i]) {
				return false
			}
		}
		for i := range x.Stmts {
			if !equalNodes(x.Stmts[i], y.Stmts[i]) {
				return false
			}
		}
		return true

	// ---- Expressions ----

	case *binaryExpr:
		y, ok := b.(*binaryExpr)
		if !ok || x.op != y.op {
			return false
		}
		return equalNodes(x.left, y.left) && equalNodes(x.right, y.right)

	case *unaryExpr:
		y, ok := b.(*unaryExpr)
		return ok && x.op == y.op && equalNodes(x.expr, y.expr)

	case *groupingExpr:
		y, ok := b.(*groupingExpr)
		return ok && equalNodes(x.expr, y.expr)

	case *literalExpr:
		y, ok := b.(*literalExpr)
		return ok && x.value == y.value

	case *interpExpr:
		y, ok := b.(*interpExpr)
		if !ok || len(x.parts) != len(y.parts) || len(x.exprs) != len(y.exprs) {
			return false
		}
		for i := range x.parts {
			if x.parts[i] != y.parts[i] {
				return false
			}
		}
		for i := range x.exprs {
			if !equalNodes(x.exprs[i], y.exprs[i]) {
				return false
			}
		}
		return true

	case *variableExpr:
		y, ok := b.(*variableExpr)
		return ok && x.name == y.name

	case *assignExpr:
		y, ok := b.(*assignExpr)
		return ok && x.name == y.name && equalNodes(x.value, y.value)

	case *callExpr:
		y, ok := b.(*callExpr)
		if !ok || !equalNodes(x.callee, y.callee) || len(x.args) != len(y.args) {
			return false
		}
		for i := range x.args {
			if !equalNodes(x.args[i], y.args[i]) {
				return false
			}
		}
		return true

	case *pipeExpr:
		y, ok := b.(*pipeExpr)
		if !ok || !equalNodes(x.expr, y.expr) ||
			!equalNodes(x.callee, y.callee) ||
			len(x.args) != len(y.args) {
			return false
		}
		for i := range x.args {
			if !equalNodes(x.args[i], y.args[i]) {
				return false
			}
		}
		return true

	case *memberExpr:
		y, ok := b.(*memberExpr)
		return ok && x.name == y.name && equalNodes(x.obj, y.obj)

	case *indexExpr:
		y, ok := b.(*indexExpr)
		return ok && equalNodes(x.obj, y.obj) && equalNodes(x.index, y.index)

	case *ifExpr:
		y, ok := b.(*ifExpr)
		if !ok || len(x.elseIf) != len(y.elseIf) {
			return false
		}
		if !equalNodes(x.cond, y.cond) || !equalNodes(x.thenBody, y.thenBody) {
			return false
		}
		for i := range x.elseIf {
			if !equalNodes(&x.elseIf[i], &y.elseIf[i]) {
				return false
			}
		}
		return equalOptionalExpr(x.elseBody, y.elseBody)

	case *matchExpr:
		y, ok := b.(*matchExpr)
		if !ok || len(x.branches) != len(y.branches) {
			return false
		}
		if !equalNodes(x.expr, y.expr) {
			return false
		}
		for i := range x.branches {
			if !equalNodes(x.branches[i].pattern, y.branches[i].pattern) ||
				!equalNodes(x.branches[i].expr, y.branches[i].expr) {
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
			if !equalNodes(x.branches[i].pattern, y.branches[i].pattern) ||
				!equalOptionalExpr(x.branches[i].guard, y.branches[i].guard) ||
				!equalNodes(x.branches[i].expr, y.branches[i].expr) {
				return false
			}
		}
		if x.elseName != y.elseName {
			return false
		}
		if !equalOptionalExpr(x.elseBody, y.elseBody) {
			return false
		}
		if !equalOptionalExpr(x.afterTime, y.afterTime) {
			return false
		}
		return equalOptionalExpr(x.afterBody, y.afterBody)

	case *withExpr:
		y, ok := b.(*withExpr)
		if !ok || len(x.items) != len(y.items) ||
			len(x.elseBranches) != len(y.elseBranches) {
			return false
		}
		for i := range x.items {
			if !equalNodes(x.items[i].pattern, y.items[i].pattern) ||
				!equalNodes(x.items[i].expr, y.items[i].expr) {
				return false
			}
		}
		switch {
		case x.body == nil && y.body == nil:
			// ok
		case x.body == nil || y.body == nil:
			return false
		default:
			if !equalNodes(x.body, y.body) {
				return false
			}
		}
		for i := range x.elseBranches {
			if !equalNodes(x.elseBranches[i].pattern, y.elseBranches[i].pattern) ||
				!equalNodes(x.elseBranches[i].body, y.elseBranches[i].body) {
				return false
			}
		}
		return true

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
			if !equalNodes(x.body.stmt, y.body.stmt) {
				return false
			}
		}
		for i := range x.ensures {
			if !equalNodes(x.ensures[i].expr, y.ensures[i].expr) {
				return false
			}
		}
		return true

	case *lambdaShortExpr:
		y, ok := b.(*lambdaShortExpr)
		return ok && x.param == y.param && equalNodes(x.body, y.body)

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
			return equalNodes(x.body, y.body)
		}

	case *lambdaEmptyExpr:
		y, ok := b.(*lambdaEmptyExpr)
		return ok && equalNodes(x.body, y.body)

	case *rangeExpr:
		y, ok := b.(*rangeExpr)
		return ok && equalNodes(x.start, y.start) && equalNodes(x.end, y.end)

	case *decimalExpr:
		y, ok := b.(*decimalExpr)
		return ok && x.value == y.value

	case *bytesExpr:
		y, ok := b.(*bytesExpr)
		return ok && x.value == y.value

	case *regexExpr:
		y, ok := b.(*regexExpr)
		return ok && x.value == y.value

	case *atomExpr:
		y, ok := b.(*atomExpr)
		return ok && x.ident == y.ident

	// ---- Statements ----

	case *letBind:
		y, ok := b.(*letBind)
		return ok && equalNodes(x.pattern, y.pattern) && equalNodes(x.value, y.value)

	case *exprStmt:
		y, ok := b.(*exprStmt)
		return ok && equalNodes(x.expr, y.expr)

	case *localFnDecl:
		y, ok := b.(*localFnDecl)
		if !ok || x.name != y.name || len(x.clauses) != len(y.clauses) {
			return false
		}
		for i := range x.clauses {
			xc := &x.clauses[i]
			yc := &y.clauses[i]
			if !equalOptionalExpr(xc.guard, yc.guard) {
				return false
			}
			if len(xc.params) != len(yc.params) {
				return false
			}
			for j := range xc.params {
				if !equalNodes(xc.params[j], yc.params[j]) {
					return false
				}
			}
			switch {
			case xc.body == nil && yc.body == nil:
				// ok
			case xc.body == nil || yc.body == nil:
				return false
			default:
				if !equalNodes(xc.body, yc.body) {
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
			if !equalNodes(x.stmts[i], y.stmts[i]) {
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
			if !equalNodes(x.fields[i].pattern, y.fields[i].pattern) {
				return false
			}
		}
		return true

	case *asPat:
		y, ok := b.(*asPat)
		return ok && x.ident == y.ident && equalNodes(x.pattern, y.pattern)

	case *tuplePattern:
		y, ok := b.(*tuplePattern)
		if !ok || len(x.patterns) != len(y.patterns) {
			return false
		}
		for i := range x.patterns {
			if !equalNodes(x.patterns[i], y.patterns[i]) {
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
			if !equalNodes(x.patterns[i], y.patterns[i]) {
				return false
			}
		}
		return true

	case *spreadPat:
		y, ok := b.(*spreadPat)
		return ok && x.name == y.name

	case *mapPattern:
		y, ok := b.(*mapPattern)
		if !ok || len(x.pairs) != len(y.pairs) {
			return false
		}
		for i := range x.pairs {
			if !equalNodes(x.pairs[i].key, y.pairs[i].key) ||
				!equalNodes(x.pairs[i].pat, y.pairs[i].pat) {
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
			if x.fields[i].name != y.fields[i].name ||
				!equalNodes(x.fields[i].pat, y.fields[i].pat) {
				return false
			}
		}
		return true

	// ---- Types ----

	case *intType:
		_, ok := b.(*intType)
		return ok
	case *floatType:
		_, ok := b.(*floatType)
		return ok
	case *decimalType:
		_, ok := b.(*decimalType)
		return ok
	case *boolType:
		_, ok := b.(*boolType)
		return ok
	case *strType:
		_, ok := b.(*strType)
		return ok
	case *atomType:
		_, ok := b.(*atomType)
		return ok
	case *unitType:
		_, ok := b.(*unitType)
		return ok
	case *rangeType:
		_, ok := b.(*rangeType)
		return ok
	case *pidType:
		_, ok := b.(*pidType)
		return ok
	case *refType:
		_, ok := b.(*refType)
		return ok

	case *functionType:
		y, ok := b.(*functionType)
		if !ok || len(x.params) != len(y.params) {
			return false
		}
		for i := range x.params {
			if !equalNodes(x.params[i], y.params[i]) {
				return false
			}
		}
		return equalNodes(x.result, y.result)

	case *listType:
		y, ok := b.(*listType)
		return ok && equalNodes(x.element, y.element)
	case *vectorType:
		y, ok := b.(*vectorType)
		return ok && equalNodes(x.element, y.element)
	case *setType:
		y, ok := b.(*setType)
		return ok && equalNodes(x.element, y.element)
	case *mapType:
		y, ok := b.(*mapType)
		return ok && equalNodes(x.key, y.key) && equalNodes(x.value, y.value)
	case *tupleType:
		y, ok := b.(*tupleType)
		if !ok || len(x.fields) != len(y.fields) {
			return false
		}
		for i := range x.fields {
			if !equalNodes(x.fields[i], y.fields[i]) {
				return false
			}
		}
		return true
	case *optionType:
		y, ok := b.(*optionType)
		return ok && equalNodes(x.element, y.element)
	case *resultType:
		y, ok := b.(*resultType)
		return ok && equalNodes(x.ok, y.ok) && equalNodes(x.err, y.err)

	case *nominalType:
		y, ok := b.(*nominalType)
		if !ok || x.name != y.name || len(x.fields) != len(y.fields) {
			return false
		}
		for i := range x.fields {
			if x.fields[i].name != y.fields[i].name ||
				!equalNodes(x.fields[i].typ, y.fields[i].typ) {
				return false
			}
		}
		return true

	case *anonymousType:
		y, ok := b.(*anonymousType)
		if !ok || len(x.fields) != len(y.fields) {
			return false
		}
		for i := range x.fields {
			if x.fields[i].name != y.fields[i].name ||
				!equalNodes(x.fields[i].typ, y.fields[i].typ) {
				return false
			}
		}
		return true

	// ---- Declarations ----

	case *importDecl:
		y, ok := b.(*importDecl)
		return ok && x.module == y.module
	case *aliasDecl:
		y, ok := b.(*aliasDecl)
		return ok && x.original == y.original && x.alias == y.alias
	case *typeDecl:
		y, ok := b.(*typeDecl)
		if !ok || x.name != y.name || len(x.generic) != len(y.generic) ||
			len(x.variants) != len(y.variants) {
			return false
		}
		for i := range x.generic {
			if x.generic[i] != y.generic[i] {
				return false
			}
		}
		for i := range x.variants {
			xv := &x.variants[i]
			yv := &y.variants[i]
			if xv.name != yv.name || len(xv.fields) != len(yv.fields) {
				return false
			}
			for j := range xv.fields {
				if !equalNodes(xv.fields[j].typ, yv.fields[j].typ) {
					return false
				}
			}
		}
		switch {
		case x.record == nil && y.record == nil:
			// ok
		case x.record == nil || y.record == nil:
			return false
		default:
			if len(x.record.fields) != len(y.record.fields) {
				return false
			}
			for i := range x.record.fields {
				if x.record.fields[i].name != y.record.fields[i].name ||
					!equalNodes(x.record.fields[i].typ, y.record.fields[i].typ) {
					return false
				}
			}
		}
		return equalOptionalType(x.alias, y.alias)

	case *funcDecl:
		y, ok := b.(*funcDecl)
		if !ok || x.name != y.name || len(x.clauses) != len(y.clauses) {
			return false
		}
		for i := range x.clauses {
			xc := &x.clauses[i]
			yc := &y.clauses[i]
			if !equalOptionalExpr(xc.guard, yc.guard) || len(xc.params) != len(yc.params) {
				return false
			}
			for j := range xc.params {
				if !equalNodes(xc.params[j], yc.params[j]) {
					return false
				}
			}
			switch {
			case xc.body == nil && yc.body == nil:
				// ok
			case xc.body == nil || yc.body == nil:
				return false
			default:
				if !equalNodes(xc.body, yc.body) {
					return false
				}
			}
		}
		return true
	}
	return false
}

func equalOptionalExpr(a, b Expr) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	default:
		return equalNodes(a, b)
	}
}

func equalOptionalType(a, b Type) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	default:
		return equalNodes(a, b)
	}
}
