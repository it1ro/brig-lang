package ast

// Walk обходит AST в глубину с помощью Visitor.
// Возвращает первую ошибку или nil.
func Walk(v Visitor, node Node) error {
	if node == nil {
		return nil
	}
	return walkNode(v, node)
}

func walkNode(v Visitor, node Node) error {
	if err := v.VisitNode(node); err != nil {
		return err
	}

	switch n := node.(type) {
	case *Program:
		for _, d := range n.Decls {
			if err := walkNode(v, d); err != nil {
				return err
			}
		}
		for _, s := range n.Stmts {
			if err := walkNode(v, s); err != nil {
				return err
			}
		}
		return nil

	case Decl:
		return walkDecl(v, n)
	case Pattern:
		return walkPattern(v, n)
	case Type:
		return walkType(v, n)
	case Expr:
		// Expr раньше Stmt: BlockStmt реализует оба. Decl/Pattern/Type раньше
		// Expr — у них тоже есть IsExpression(), иначе case Expr их глотает.
		return walkExpr(v, n)
	case Stmt:
		return walkStmt(v, n)
	}
	return nil
}

func walkExpr(v Visitor, e Expr) error {
	if err := v.VisitExpr(e); err != nil {
		return err
	}

	switch n := e.(type) {
	case *binaryExpr:
		if err := walkNode(v, n.left); err != nil {
			return err
		}
		return walkNode(v, n.right)

	case *unaryExpr:
		return walkNode(v, n.expr)

	case *groupingExpr:
		return walkNode(v, n.expr)

	case *memberExpr:
		return walkNode(v, n.obj)

	case *indexExpr:
		if err := walkNode(v, n.obj); err != nil {
			return err
		}
		return walkNode(v, n.index)

	case *callExpr:
		if err := walkNode(v, n.callee); err != nil {
			return err
		}
		for _, a := range n.args {
			if err := walkNode(v, a); err != nil {
				return err
			}
		}
		return nil

	case *pipeExpr:
		if err := walkNode(v, n.expr); err != nil {
			return err
		}
		if err := walkNode(v, n.callee); err != nil {
			return err
		}
		for _, a := range n.args {
			if err := walkNode(v, a); err != nil {
				return err
			}
		}
		return nil

	case *ifExpr:
		if err := walkNode(v, n.cond); err != nil {
			return err
		}
		if err := walkNode(v, n.thenBody); err != nil {
			return err
		}
		for i := range n.elseIf {
			if err := walkNode(v, &n.elseIf[i]); err != nil {
				return err
			}
		}
		if n.elseBody != nil {
			return walkNode(v, n.elseBody)
		}
		return nil

	case *matchExpr:
		if err := walkNode(v, n.expr); err != nil {
			return err
		}
		for i := range n.branches {
			br := &n.branches[i]
			if err := walkNode(v, br.pattern); err != nil {
				return err
			}
			if err := walkNode(v, br.expr); err != nil {
				return err
			}
		}
		return nil

	case *recvExpr:
		for i := range n.branches {
			br := &n.branches[i]
			if err := walkNode(v, br.pattern); err != nil {
				return err
			}
			if br.guard != nil {
				if err := walkNode(v, br.guard); err != nil {
					return err
				}
			}
			if err := walkNode(v, br.expr); err != nil {
				return err
			}
		}
		if n.elseBody != nil {
			if err := walkNode(v, n.elseBody); err != nil {
				return err
			}
		}
		if n.afterTime != nil {
			if err := walkNode(v, n.afterTime); err != nil {
				return err
			}
		}
		if n.afterBody != nil {
			return walkNode(v, n.afterBody)
		}
		return nil

	case *withExpr:
		for i := range n.items {
			it := &n.items[i]
			if err := walkNode(v, it.pattern); err != nil {
				return err
			}
			if err := walkNode(v, it.expr); err != nil {
				return err
			}
		}
		if n.body != nil {
			if err := walkNode(v, n.body); err != nil {
				return err
			}
		}
		for i := range n.elseBranches {
			eb := &n.elseBranches[i]
			if err := walkNode(v, eb.pattern); err != nil {
				return err
			}
			if err := walkNode(v, eb.body); err != nil {
				return err
			}
		}
		return nil

	case *trapExpr:
		if n.expr != nil {
			if err := walkNode(v, n.expr); err != nil {
				return err
			}
		}
		if n.body != nil {
			if err := walkNode(v, n.body.stmt); err != nil {
				return err
			}
		}
		for i := range n.ensures {
			if err := walkNode(v, n.ensures[i].expr); err != nil {
				return err
			}
		}
		return nil

	case *lambdaShortExpr:
		return walkNode(v, n.body)

	case *lambdaFullExpr:
		if n.body != nil {
			return walkNode(v, n.body)
		}
		return nil

	case *lambdaEmptyExpr:
		return walkNode(v, n.body)

	case *rangeExpr:
		if err := walkNode(v, n.start); err != nil {
			return err
		}
		return walkNode(v, n.end)

		// Листья: literalExpr, variableExpr, assignExpr, decimalExpr,
		// bytesExpr, regexExpr, atomExpr — детей нет.
	}
	return nil
}

func walkStmt(v Visitor, s Stmt) error {
	if err := v.VisitStmt(s); err != nil {
		return err
	}

	switch n := s.(type) {
	case *letBind:
		if err := walkNode(v, n.pattern); err != nil {
			return err
		}
		return walkNode(v, n.value)

	case *exprStmt:
		return walkNode(v, n.expr)

	case *localFnDecl:
		for i := range n.clauses {
			if n.clauses[i].body != nil {
				if err := walkNode(v, n.clauses[i].body); err != nil {
					return err
				}
			}
		}
		return nil

	case *BlockStmt:
		for _, st := range n.stmts {
			if err := walkNode(v, st); err != nil {
				return err
			}
		}
		return nil
	}
	return nil
}

func walkPattern(v Visitor, p Pattern) error {
	if err := v.VisitPattern(p); err != nil {
		return err
	}

	switch n := p.(type) {
	case *constructorPat:
		for i := range n.fields {
			if err := walkNode(v, n.fields[i].pattern); err != nil {
				return err
			}
		}
		return nil

	case *tuplePattern:
		for _, sub := range n.patterns {
			if err := walkNode(v, sub); err != nil {
				return err
			}
		}
		return nil

	case *listPattern:
		for _, sub := range n.patterns {
			if err := walkNode(v, sub); err != nil {
				return err
			}
		}
		return nil

	case *mapPattern:
		for i := range n.pairs {
			if err := walkNode(v, n.pairs[i].key); err != nil {
				return err
			}
			if err := walkNode(v, n.pairs[i].pat); err != nil {
				return err
			}
		}
		return nil

	case *recordPattern:
		for i := range n.fields {
			if err := walkNode(v, n.fields[i].pat); err != nil {
				return err
			}
		}
		return nil

	case *asPat:
		return walkNode(v, n.pattern)

		// wildcardPat, identPat, literalPat — листья.
	}
	return nil
}

func walkType(v Visitor, t Type) error {
	if err := v.VisitType(t); err != nil {
		return err
	}

	switch n := t.(type) {
	case *functionType:
		for _, p := range n.params {
			if err := walkNode(v, p); err != nil {
				return err
			}
		}
		return walkNode(v, n.result)

	case *listType:
		return walkNode(v, n.element)

	case *vectorType:
		return walkNode(v, n.element)

	case *mapType:
		if err := walkNode(v, n.key); err != nil {
			return err
		}
		return walkNode(v, n.value)

	case *setType:
		return walkNode(v, n.element)

	case *tupleType:
		for _, f := range n.fields {
			if err := walkNode(v, f); err != nil {
				return err
			}
		}
		return nil

	case *nominalType:
		for i := range n.fields {
			if err := walkNode(v, n.fields[i].typ); err != nil {
				return err
			}
		}
		return nil

	case *anonymousType:
		for i := range n.fields {
			if err := walkNode(v, n.fields[i].typ); err != nil {
				return err
			}
		}
		return nil

	case *optionType:
		return walkNode(v, n.element)

	case *resultType:
		if err := walkNode(v, n.ok); err != nil {
			return err
		}
		return walkNode(v, n.err)

		// Примитивы: intType, floatType, decimalType, boolType, strType,
		// atomType, unitType, rangeType, pidType, refType — листья.
	}
	return nil
}

func walkDecl(v Visitor, d Decl) error {
	if err := v.VisitDecl(d); err != nil {
		return err
	}

	switch n := d.(type) {
	case *typeDecl:
		for i := range n.variants {
			for j := range n.variants[i].fields {
				if err := walkNode(v, n.variants[i].fields[j].typ); err != nil {
					return err
				}
			}
		}
		if n.record != nil {
			for i := range n.record.fields {
				if err := walkNode(v, n.record.fields[i].typ); err != nil {
					return err
				}
			}
		}
		if n.alias != nil {
			return walkNode(v, n.alias)
		}
		return nil

	case *funcDecl:
		for i := range n.clauses {
			if n.clauses[i].body != nil {
				if err := walkNode(v, n.clauses[i].body); err != nil {
					return err
				}
			}
		}
		return nil

		// importDecl, aliasDecl — листья.
	}
	return nil
}
