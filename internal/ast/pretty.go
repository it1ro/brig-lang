package ast

import (
	"bytes"
	"fmt"
)

// Pretty — S-expression печать AST для отладки и golden-тестов.
func Pretty(node Node) string {
	var buf bytes.Buffer
	prettyNode(&buf, node, 0)
	return buf.String()
}

func prettyNode(buf *bytes.Buffer, node Node, indent int) {
	if node == nil {
		buf.WriteString("(nil)")
		return
	}
	switch n := node.(type) {
	case *Program:
		buf.WriteString("(program")
		if n.Module != "" {
			buf.WriteString(" (module " + n.Module + ")")
		}
		for _, d := range n.Decls {
			buf.WriteString(" ")
			prettyNode(buf, d, indent)
		}
		for _, s := range n.Stmts {
			buf.WriteString(" ")
			prettyNode(buf, s, indent)
		}
		buf.WriteString(")")
	case Expr:
		prettyExpr(buf, n, indent)
	case Stmt:
		prettyStmt(buf, n, indent)
	case Pattern:
		prettyPattern(buf, n, indent)
	case Type:
		prettyType(buf, n, indent)
	case Decl:
		prettyDecl(buf, n, indent)
	default:
		fmt.Fprintf(buf, "(unknown %T)", node)
	}
}

func prettyExpr(buf *bytes.Buffer, e Expr, indent int) {
	switch n := e.(type) {
	case *binaryExpr:
		buf.WriteString("(")
		buf.WriteString(n.op)
		buf.WriteString(" ")
		prettyNode(buf, n.left, indent)
		buf.WriteString(" ")
		prettyNode(buf, n.right, indent)
		buf.WriteString(")")
	case *unaryExpr:
		buf.WriteString("(")
		buf.WriteString(n.op)
		buf.WriteString(" ")
		prettyNode(buf, n.expr, indent)
		buf.WriteString(")")
	case *groupingExpr:
		buf.WriteString("(group ")
		prettyNode(buf, n.expr, indent)
		buf.WriteString(")")
	case *literalExpr:
		fmt.Fprintf(buf, "(lit %s)", n.value)
	case *variableExpr:
		fmt.Fprintf(buf, "(var %s)", n.name)
	case *assignExpr:
		buf.WriteString("(= ")
		buf.WriteString(n.name)
		buf.WriteString(" ")
		prettyNode(buf, n.value, indent)
		buf.WriteString(")")
	case *callExpr:
		buf.WriteString("(call ")
		prettyNode(buf, n.callee, indent)
		for _, arg := range n.args {
			buf.WriteString(" ")
			prettyNode(buf, arg, indent)
		}
		buf.WriteString(")")
	case *pipeExpr:
		buf.WriteString("(|> ")
		prettyNode(buf, n.expr, indent)
		buf.WriteString(" ")
		prettyNode(buf, n.callee, indent)
		for _, arg := range n.args {
			buf.WriteString(" ")
			prettyNode(buf, arg, indent)
		}
		buf.WriteString(")")
	case *memberExpr:
		buf.WriteString("(. ")
		prettyNode(buf, n.obj, indent)
		buf.WriteString(" " + n.name + ")")
	case *indexExpr:
		buf.WriteString("(idx ")
		prettyNode(buf, n.obj, indent)
		buf.WriteString(" ")
		prettyNode(buf, n.index, indent)
		buf.WriteString(")")
	case *ifExpr:
		buf.WriteString("(if ")
		prettyNode(buf, n.cond, indent)
		buf.WriteString(" ")
		prettyNode(buf, n.thenBody, indent)
		for i := range n.elseIf {
			buf.WriteString(" (elseif ")
			prettyNode(buf, n.elseIf[i].cond, indent)
			buf.WriteString(" ")
			prettyNode(buf, n.elseIf[i].thenBody, indent)
			buf.WriteString(")")
		}
		if n.elseBody != nil {
			buf.WriteString(" (else ")
			prettyNode(buf, n.elseBody, indent)
			buf.WriteString(")")
		}
		buf.WriteString(")")
	case *matchExpr:
		buf.WriteString("(match ")
		prettyNode(buf, n.expr, indent)
		for i := range n.branches {
			br := &n.branches[i]
			buf.WriteString(" (case ")
			prettyNode(buf, br.pattern, indent)
			buf.WriteString(" ")
			prettyNode(buf, br.expr, indent)
			buf.WriteString(")")
		}
		buf.WriteString(")")
	case *recvExpr:
		buf.WriteString("(recv")
		for i := range n.branches {
			br := &n.branches[i]
			buf.WriteString(" (case ")
			prettyNode(buf, br.pattern, indent)
			buf.WriteString(" ")
			prettyNode(buf, br.expr, indent)
			buf.WriteString(")")
		}
		if n.elseName != "" {
			buf.WriteString(" (else " + n.elseName + " ")
			prettyNode(buf, n.elseBody, indent)
			buf.WriteString(")")
		}
		if n.afterBody != nil {
			buf.WriteString(" (after ")
			prettyNode(buf, n.afterTime, indent)
			buf.WriteString(" ")
			prettyNode(buf, n.afterBody, indent)
			buf.WriteString(")")
		}
		buf.WriteString(")")
	case *withExpr:
		buf.WriteString("(with")
		for i := range n.items {
			it := &n.items[i]
			buf.WriteString(" (bind ")
			prettyNode(buf, it.pattern, indent)
			buf.WriteString(" ")
			prettyNode(buf, it.expr, indent)
			buf.WriteString(")")
		}
		if n.body != nil {
			buf.WriteString(" (body ")
			prettyNode(buf, n.body, indent)
			buf.WriteString(")")
		}
		for i := range n.elseBranches {
			eb := &n.elseBranches[i]
			buf.WriteString(" (else ")
			prettyNode(buf, eb.pattern, indent)
			buf.WriteString(" ")
			prettyNode(buf, eb.body, indent)
			buf.WriteString(")")
		}
		buf.WriteString(")")
	case *trapExpr:
		buf.WriteString("(trap")
		if n.expr != nil {
			buf.WriteString(" ")
			prettyNode(buf, n.expr, indent)
		}
		if n.body != nil {
			buf.WriteString(" (body ")
			prettyNode(buf, n.body.stmt, indent)
			buf.WriteString(")")
		}
		for i := range n.ensures {
			buf.WriteString(" (ensure ")
			prettyNode(buf, n.ensures[i].expr, indent)
			buf.WriteString(")")
		}
		buf.WriteString(")")
	case *lambdaShortExpr:
		fmt.Fprintf(buf, "(lambda %s ", n.param)
		prettyNode(buf, n.body, indent)
		buf.WriteString(")")
	case *lambdaFullExpr:
		buf.WriteString("(fn (")
		for i, p := range n.params {
			if i > 0 {
				buf.WriteString(" ")
			}
			buf.WriteString(p)
		}
		buf.WriteString(") ")
		prettyNode(buf, n.body, indent)
		buf.WriteString(")")
	case *lambdaEmptyExpr:
		buf.WriteString("(fn () ")
		prettyNode(buf, n.body, indent)
		buf.WriteString(")")
	case *rangeExpr:
		buf.WriteString("(to ")
		prettyNode(buf, n.start, indent)
		buf.WriteString(" ")
		prettyNode(buf, n.end, indent)
		buf.WriteString(")")
	case *decimalExpr:
		fmt.Fprintf(buf, "(dec %s)", n.value)
	case *bytesExpr:
		fmt.Fprintf(buf, "(bytes %s)", n.value)
	case *regexExpr:
		fmt.Fprintf(buf, "(regex %s)", n.value)
	case *atomExpr:
		fmt.Fprintf(buf, "(:%s)", n.ident)
	case *BlockStmt:
		// BlockStmt реализует и Expr, и Stmt; prettyNode выбирает Expr
		// первым, поэтому рендер здесь.
		buf.WriteString("(block")
		for _, st := range n.stmts {
			buf.WriteString(" ")
			prettyNode(buf, st, indent)
		}
		buf.WriteString(")")
	}
}

func prettyStmt(buf *bytes.Buffer, s Stmt, indent int) {
	switch n := s.(type) {
	case *letBind:
		buf.WriteString("(let ")
		prettyNode(buf, n.pattern, indent)
		buf.WriteString(" ")
		prettyNode(buf, n.value, indent)
		buf.WriteString(")")
	case *exprStmt:
		prettyNode(buf, n.expr, indent)
	case *localFnDecl:
		buf.WriteString("(local-fn " + n.name)
		for i := range n.clauses {
			cl := &n.clauses[i]
			buf.WriteString(" (clause")
			if cl.guard != "" {
				buf.WriteString(" (when " + cl.guard + ")")
			}
			buf.WriteString(" (params")
			for _, pm := range cl.params {
				buf.WriteString(" " + pm)
			}
			buf.WriteString(")")
			buf.WriteString(" ")
			prettyNode(buf, cl.body, indent)
			buf.WriteString(")")
		}
		buf.WriteString(")")
	}
}

func prettyPattern(buf *bytes.Buffer, p Pattern, indent int) {
	switch n := p.(type) {
	case *wildcardPat:
		buf.WriteString("_")
	case *identPat:
		buf.WriteString(n.name)
	case *literalPat:
		buf.WriteString(n.value)
	case *constructorPat:
		buf.WriteString("(" + n.name)
		for i := range n.fields {
			buf.WriteString(" ")
			prettyNode(buf, n.fields[i].pattern, indent)
		}
		buf.WriteString(")")
	case *asPat:
		buf.WriteString("(as ")
		prettyNode(buf, n.pattern, indent)
		buf.WriteString(" " + n.ident + ")")
	case *tuplePattern:
		buf.WriteString("(")
		for i, pat := range n.patterns {
			if i > 0 {
				buf.WriteString(" ")
			}
			prettyNode(buf, pat, indent)
		}
		buf.WriteString(")")
	case *listPattern:
		buf.WriteString("[")
		for i, pat := range n.patterns {
			if i > 0 {
				buf.WriteString(" ")
			}
			prettyNode(buf, pat, indent)
		}
		if n.hasRest {
			if len(n.patterns) > 0 {
				buf.WriteString(" ")
			}
			buf.WriteString("..")
			if n.restName != "" {
				buf.WriteString(n.restName)
			}
		}
		buf.WriteString("]")
	case *mapPattern:
		buf.WriteString("%{")
		for i := range n.pairs {
			if i > 0 {
				buf.WriteString(" ")
			}
			prettyNode(buf, n.pairs[i].key, indent)
			buf.WriteString(" => ")
			prettyNode(buf, n.pairs[i].pat, indent)
		}
		buf.WriteString("}")
	case *recordPattern:
		if n.typ != "" {
			buf.WriteString(n.typ)
		}
		buf.WriteString("{")
		for i := range n.fields {
			if i > 0 {
				buf.WriteString(" ")
			}
			buf.WriteString(n.fields[i].name + ": ")
			prettyNode(buf, n.fields[i].pat, indent)
		}
		buf.WriteString("}")
	}
}

func prettyType(buf *bytes.Buffer, t Type, indent int) {
	switch n := t.(type) {
	case *intType:
		buf.WriteString("Int")
	case *floatType:
		buf.WriteString("Float")
	case *decimalType:
		buf.WriteString("Decimal")
	case *boolType:
		buf.WriteString("Bool")
	case *strType:
		buf.WriteString("Str")
	case *atomType:
		buf.WriteString("Atom")
	case *functionType:
		buf.WriteString("(fn (")
		for i, p := range n.params {
			if i > 0 {
				buf.WriteString(" ")
			}
			prettyNode(buf, p, indent)
		}
		buf.WriteString(") -> ")
		prettyNode(buf, n.result, indent)
		buf.WriteString(")")
	case *unitType:
		buf.WriteString("()")
	case *rangeType:
		buf.WriteString("Range")
	case *pidType:
		buf.WriteString("Pid")
	case *refType:
		buf.WriteString("Ref")
	case *listType:
		buf.WriteString("(List ")
		prettyNode(buf, n.element, indent)
		buf.WriteString(")")
	case *vectorType:
		buf.WriteString("(Vector ")
		prettyNode(buf, n.element, indent)
		buf.WriteString(")")
	case *mapType:
		buf.WriteString("(Map ")
		prettyNode(buf, n.key, indent)
		buf.WriteString(" ")
		prettyNode(buf, n.value, indent)
		buf.WriteString(")")
	case *setType:
		buf.WriteString("(Set ")
		prettyNode(buf, n.element, indent)
		buf.WriteString(")")
	case *tupleType:
		buf.WriteString("(Tuple")
		for _, f := range n.fields {
			buf.WriteString(" ")
			prettyNode(buf, f, indent)
		}
		buf.WriteString(")")
	case *nominalType:
		buf.WriteString("(type " + n.name)
		for i := range n.fields {
			buf.WriteString(" " + n.fields[i].name + ": ")
			prettyNode(buf, n.fields[i].typ, indent)
		}
		buf.WriteString(")")
	case *anonymousType:
		buf.WriteString("{")
		for i := range n.fields {
			if i > 0 {
				buf.WriteString(" ")
			}
			buf.WriteString(n.fields[i].name + ": ")
			prettyNode(buf, n.fields[i].typ, indent)
		}
		buf.WriteString("}")
	case *optionType:
		buf.WriteString("(Option ")
		prettyNode(buf, n.element, indent)
		buf.WriteString(")")
	case *resultType:
		buf.WriteString("(Result ")
		prettyNode(buf, n.ok, indent)
		buf.WriteString(" ")
		prettyNode(buf, n.err, indent)
		buf.WriteString(")")
	}
}

func prettyDecl(buf *bytes.Buffer, d Decl, indent int) {
	switch n := d.(type) {
	case *importDecl:
		fmt.Fprintf(buf, "(import %s)", n.module)
	case *aliasDecl:
		fmt.Fprintf(buf, "(alias %s as %s)", n.original, n.alias)
	case *typeDecl:
		buf.WriteString("(type " + n.name)
		if len(n.generic) > 0 {
			buf.WriteString(" <")
			for i, g := range n.generic {
				if i > 0 {
					buf.WriteString(" ")
				}
				buf.WriteString(g)
			}
			buf.WriteString(">")
		}
		for i := range n.variants {
			v := &n.variants[i]
			buf.WriteString(" (variant " + v.name)
			for _, f := range v.fields {
				buf.WriteString(" " + f.name + ": ")
				prettyNode(buf, f.typ, indent)
			}
			buf.WriteString(")")
		}
		if n.record != nil {
			buf.WriteString(" (record")
			for _, f := range n.record.fields {
				buf.WriteString(" " + f.name + ": ")
				prettyNode(buf, f.typ, indent)
			}
			buf.WriteString(")")
		}
		if n.alias != nil {
			buf.WriteString(" (alias ")
			prettyNode(buf, n.alias, indent)
			buf.WriteString(")")
		}
		buf.WriteString(")")
	case *funcDecl:
		buf.WriteString("(fn " + n.name)
		for i := range n.clauses {
			cl := &n.clauses[i]
			buf.WriteString(" (clause")
			if cl.guard != "" {
				buf.WriteString(" (when " + cl.guard + ")")
			}
			buf.WriteString(" (params")
			for _, p := range cl.params {
				buf.WriteString(" " + p)
			}
			buf.WriteString(")")
			buf.WriteString(" ")
			prettyNode(buf, cl.body, indent)
			buf.WriteString(")")
		}
		buf.WriteString(")")
	}
}
