package ast

import "strings"

// Format печатает узел в канонический .brig-текст.
// Для *Program и *BlockStmt результат оканчивается '\n'.
func Format(n Node) string {
	var sb strings.Builder
	p := &printer{}
	p.formatNode(&sb, n)
	return sb.String()
}

type printer struct{}

func (p *printer) formatNode(sb *strings.Builder, n Node) {
	switch v := n.(type) {
	case *Program:
		p.formatProgram(sb, v)
	case Expr:
		sb.WriteString(p.exprString(v, 0))
		sb.WriteByte('\n')
	case Stmt:
		p.writeStmt(sb, v, 0)
	case Pattern:
		sb.WriteString(p.patternString(v))
		sb.WriteByte('\n')
	case Type:
		sb.WriteString(p.typeString(v))
		sb.WriteByte('\n')
	case Decl:
		sb.WriteString(p.declString(v))
		sb.WriteByte('\n')
	}
}

func (p *printer) formatProgram(sb *strings.Builder, prog *Program) {
	if prog.Module != "" {
		sb.WriteString("module ")
		sb.WriteString(prog.Module)
		sb.WriteByte('\n')
	}
	for _, d := range prog.Decls {
		sb.WriteString(p.declString(d))
		sb.WriteByte('\n')
	}
	for _, s := range prog.Stmts {
		p.writeStmt(sb, s, 0)
	}
}

// ---- statements ----

func indentStr(n int) string { return strings.Repeat("    ", n) }

func (p *printer) writeStmt(sb *strings.Builder, s Stmt, indent int) {
	pad := indentStr(indent)
	switch v := s.(type) {
	case *letBind:
		sb.WriteString(pad)
		sb.WriteString(p.patternString(v.pattern))
		sb.WriteString(" = ")
		sb.WriteString(p.exprString(v.value, indent))
		sb.WriteByte('\n')

	case *exprStmt:
		sb.WriteString(pad)
		sb.WriteString(p.exprString(v.expr, indent))
		sb.WriteByte('\n')

	case *localFnDecl:
		for i := range v.clauses {
			c := &v.clauses[i]
			sb.WriteString(pad)
			sb.WriteString("fn ")
			sb.WriteString(v.name)
			sb.WriteString("(")
			sb.WriteString(strings.Join(c.params, ", "))
			sb.WriteString(")")
			if c.guard != "" {
				sb.WriteString(" when ")
				sb.WriteString(c.guard)
			}
			sb.WriteString(" ->\n")
			p.writeBlock(sb, c.body, indent+1)
		}

	case *BlockStmt:
		p.writeBlock(sb, v, indent)
	}
}

func (p *printer) writeBlock(sb *strings.Builder, blk *BlockStmt, indent int) {
	if blk == nil {
		return
	}
	for _, s := range blk.Stmts() {
		p.writeStmt(sb, s, indent)
	}
}

// ---- declarations ----

func (p *printer) declString(d Decl) string {
	switch v := d.(type) {
	case *importDecl:
		return "import " + v.module
	case *aliasDecl:
		return "alias " + v.original + " as " + v.alias
	case *typeDecl:
		return p.typeDeclString(v)
	case *funcDecl:
		return p.funcDeclString(v)
	}
	return ""
}

func (p *printer) typeDeclString(v *typeDecl) string {
	var sb strings.Builder
	sb.WriteString("type ")
	sb.WriteString(v.name)
	if len(v.generic) > 0 {
		sb.WriteString("<")
		sb.WriteString(strings.Join(v.generic, ", "))
		sb.WriteString(">")
	}
	switch {
	case v.alias != nil:
		sb.WriteString(" = ")
		sb.WriteString(p.typeString(v.alias))
	case v.record != nil:
		sb.WriteString(" { ")
		parts := make([]string, len(v.record.fields))
		for i, f := range v.record.fields {
			parts[i] = f.name + ": " + p.typeString(f.typ)
		}
		sb.WriteString(strings.Join(parts, ", "))
		sb.WriteString(" }")
	default:
		sb.WriteString(" { ")
		parts := make([]string, len(v.variants))
		for i, vr := range v.variants {
			if len(vr.fields) == 0 {
				parts[i] = vr.name
			} else {
				fs := make([]string, len(vr.fields))
				for j, f := range vr.fields {
					fs[j] = p.typeString(f.typ)
				}
				parts[i] = vr.name + "(" + strings.Join(fs, ", ") + ")"
			}
		}
		sb.WriteString(strings.Join(parts, ", "))
		sb.WriteString(" }")
	}
	return sb.String()
}

func (p *printer) funcDeclString(v *funcDecl) string {
	var sb strings.Builder
	for i := range v.clauses {
		c := &v.clauses[i]
		sb.WriteString("fn ")
		sb.WriteString(v.name)
		sb.WriteString("(")
		sb.WriteString(strings.Join(c.params, ", "))
		sb.WriteString(")")
		if c.guard != "" {
			sb.WriteString(" when ")
			sb.WriteString(c.guard)
		}
		sb.WriteString(" ->\n")
		p.writeBlock(&sb, c.body, 1)
	}
	return strings.TrimSuffix(sb.String(), "\n")
}

// ---- expressions ----

func (p *printer) exprString(e Expr, indent int) string {
	switch v := e.(type) {
	case *literalExpr:
		return v.value
	case *interpExpr:
		var sb strings.Builder
		sb.WriteByte('"')
		for i, part := range v.parts {
			sb.WriteString(part)
			if i < len(v.exprs) {
				sb.WriteString(`\(`)
				sb.WriteString(p.exprString(v.exprs[i], indent))
				sb.WriteByte(')')
			}
		}
		sb.WriteByte('"')
		return sb.String()
	case *variableExpr:
		return v.name
	case *decimalExpr:
		return `dec"` + v.value + `"`
	case *bytesExpr:
		return `b"` + v.value + `"`
	case *regexExpr:
		return `rx"` + v.value + `"`
	case *atomExpr:
		return ":" + v.ident

	case *binaryExpr:
		switch v.op {
		case ":":
			return p.exprString(v.left, indent) + ": " + p.exprString(v.right, indent)
		case "=>":
			return p.exprString(v.left, indent) + " => " + p.exprString(v.right, indent)
		}
		return p.exprString(v.left, indent) + " " + v.op + " " + p.exprString(v.right, indent)

	case *unaryExpr:
		switch v.op {
		case "not":
			return "not " + p.exprString(v.expr, indent)
		default: // "-", ".."
			return v.op + p.exprString(v.expr, indent)
		}

	case *groupingExpr:
		return "(" + p.exprString(v.expr, indent) + ")"

	case *callExpr:
		return p.callString(v, indent)

	case *pipeExpr:
		lhs := p.exprString(v.expr, indent)
		rhs := p.exprString(v.callee, indent)
		if len(v.args) == 0 {
			return lhs + " |> " + rhs
		}
		args := make([]string, len(v.args))
		for i, a := range v.args {
			args[i] = p.exprString(a, indent)
		}
		return lhs + " |> " + rhs + "(" + strings.Join(args, ", ") + ")"

	case *memberExpr:
		lhs := p.exprString(v.obj, indent)
		// 0 .A → "0.A" → lex error '1. requires digit after the dot',
		// потому что scanNumber, прочитав "0", видит '.' и следующий
		// символ не цифру. Для float ("1.5"), hex ("0xFF") и exponent
		// ("1e5") scanNumber корректно завершается на '.', пробел не нужен.
		if endsWithPlainIntLit(v.obj) {
			return lhs + " ." + v.name
		}
		return lhs + "." + v.name

	case *indexExpr:
		return p.exprString(v.obj, indent) + "[" + p.exprString(v.index, indent) + "]"

	case *ifExpr:
		return p.ifString(v, indent)
	case *matchExpr:
		return p.matchString(v, indent)
	case *recvExpr:
		return p.recvString(v, indent)
	case *withExpr:
		return p.withString(v, indent)
	case *trapExpr:
		return p.trapString(v, indent)

	case *lambdaShortExpr:
		return v.param + " -> " + p.exprString(v.body, indent)
	case *lambdaEmptyExpr:
		return "() -> " + p.exprString(v.body, indent)
	case *lambdaFullExpr:
		var sb strings.Builder
		sb.WriteString("fn (")
		sb.WriteString(strings.Join(v.params, ", "))
		sb.WriteString(") ->\n")
		p.writeBlock(&sb, v.body, indent+1)
		return strings.TrimSuffix(sb.String(), "\n")

	case *rangeExpr:
		return p.exprString(v.start, indent) + " to " + p.exprString(v.end, indent)

	case *BlockStmt:
		var sb strings.Builder
		p.writeBlock(&sb, v, indent)
		return strings.TrimSuffix(sb.String(), "\n")
	}
	return ""
}

func (p *printer) callString(v *callExpr, indent int) string {
	if name, ok := specialCallee(v.callee); ok {
		return p.specialCall(name, v.args, indent)
	}
	args := make([]string, len(v.args))
	for i, a := range v.args {
		args[i] = p.exprString(a, indent)
	}
	return p.exprString(v.callee, indent) + "(" + strings.Join(args, ", ") + ")"
}

// specialCallee: `()`, `[]`, `%[]`, `%{}`, `{}`, `Name{}` — синтетические
// «callee», которыми парсер помечает tuple/list/vector/map/record.
func specialCallee(e Expr) (string, bool) {
	v, ok := e.(*variableExpr)
	if !ok {
		return "", false
	}
	switch v.name {
	case "()", "[]", "%[]", "%{}", "{}":
		return v.name, true
	}
	if strings.HasSuffix(v.name, "{}") && len(v.name) > 2 {
		return v.name[:len(v.name)-2], true
	}
	return "", false
}

func (p *printer) specialCall(name string, args []Expr, indent int) string {
	strs := make([]string, len(args))
	for i, a := range args {
		strs[i] = p.exprString(a, indent)
	}
	switch name {
	case "()": // tuple / unit уже как literalExpr
		switch len(strs) {
		case 0:
			return "()"
		case 1:
			return "(" + strs[0] + ",)"
		default:
			return "(" + strings.Join(strs, ", ") + ")"
		}
	case "[]":
		return "[" + strings.Join(strs, ", ") + "]"
	case "%[]":
		return "%[" + strings.Join(strs, ", ") + "]"
	case "%{}":
		if len(strs) == 0 {
			return "%{}"
		}
		return "%{ " + strings.Join(strs, ", ") + " }"
	case "{}":
		if len(strs) == 0 {
			return "{}"
		}
		return "{ " + strings.Join(strs, ", ") + " }"
	}
	// именованный record-литерал: Name{...}
	if len(strs) == 0 {
		return name + "{}"
	}
	return name + "{ " + strings.Join(strs, ", ") + " }"
}

func (p *printer) ifString(v *ifExpr, indent int) string {
	_, thenBlk := v.thenBody.(*BlockStmt)
	_, elseBlk := v.elseBody.(*BlockStmt)
	if !thenBlk && !elseBlk && v.elseBody != nil {
		return "if " + p.exprString(v.cond, indent) + " then " +
			p.exprString(v.thenBody, indent) + " else " +
			p.exprString(v.elseBody, indent)
	}
	var sb strings.Builder
	sb.WriteString("if ")
	sb.WriteString(p.exprString(v.cond, indent))
	sb.WriteByte('\n')
	p.writeBranchBody(&sb, v.thenBody, indent+1)
	if v.elseBody != nil {
		sb.WriteString(indentStr(indent))
		sb.WriteString("else\n")
		p.writeBranchBody(&sb, v.elseBody, indent+1)
	}
	return strings.TrimSuffix(sb.String(), "\n")
}

func (p *printer) matchString(v *matchExpr, indent int) string {
	var sb strings.Builder
	sb.WriteString("match ")
	sb.WriteString(p.exprString(v.expr, indent))
	sb.WriteByte('\n')
	for i := range v.branches {
		br := &v.branches[i]
		sb.WriteString(indentStr(indent + 1))
		sb.WriteString(p.patternString(br.pattern))
		sb.WriteString(" ->")
		p.writeBranchTail(&sb, br.expr, indent+1)
	}
	return strings.TrimSuffix(sb.String(), "\n")
}

func (p *printer) recvString(v *recvExpr, indent int) string {
	var sb strings.Builder
	sb.WriteString("recv\n")
	for i := range v.branches {
		br := &v.branches[i]
		sb.WriteString(indentStr(indent + 1))
		sb.WriteString(p.patternString(br.pattern))
		if br.guard != nil {
			sb.WriteString(" when ")
			sb.WriteString(p.exprString(br.guard, indent+1))
		}
		sb.WriteString(" ->")
		p.writeBranchTail(&sb, br.expr, indent+1)
	}
	if v.elseName != "" {
		sb.WriteString(indentStr(indent))
		sb.WriteString("else ")
		sb.WriteString(v.elseName)
		sb.WriteByte('\n')
		p.writeBranchBody(&sb, v.elseBody, indent+1)
	}
	if v.afterBody != nil {
		sb.WriteString(indentStr(indent))
		sb.WriteString("after ")
		sb.WriteString(p.exprString(v.afterTime, indent))
		sb.WriteString(" ->")
		p.writeBranchTail(&sb, v.afterBody, indent)
	}
	return strings.TrimSuffix(sb.String(), "\n")
}

func (p *printer) withString(v *withExpr, indent int) string {
	var sb strings.Builder
	sb.WriteString("with\n")
	for i := range v.items {
		it := &v.items[i]
		sb.WriteString(indentStr(indent + 1))
		sb.WriteString(p.patternString(it.pattern))
		sb.WriteString(" <- ")
		sb.WriteString(p.exprString(it.expr, indent+1))
		sb.WriteByte('\n')
	}
	p.writeBlock(&sb, v.body, indent+1)
	if len(v.elseBranches) > 0 {
		sb.WriteString(indentStr(indent))
		sb.WriteString("else\n")
		for i := range v.elseBranches {
			eb := &v.elseBranches[i]
			sb.WriteString(indentStr(indent + 1))
			sb.WriteString(p.patternString(eb.pattern))
			sb.WriteString(" ->")
			p.writeBranchTail(&sb, eb.body, indent+1)
		}
	}
	return strings.TrimSuffix(sb.String(), "\n")
}

// trapString: тело trap и ensure-клаузы живут на одном уровне
// (A2: ensure — trap_item внутри INDENT-блока). Форматтер печатает
// сначала все stmt-ы, затем ensure-клаузы в порядке AST (runtime
// выполняет LIFO).
func (p *printer) trapString(v *trapExpr, indent int) string {
	if v.expr != nil {
		return "trap(" + p.exprString(v.expr, indent) + ")"
	}
	var sb strings.Builder
	sb.WriteString("trap\n")
	if v.body != nil {
		switch b := v.body.stmt.(type) {
		case *BlockStmt:
			p.writeBlock(&sb, b, indent+1)
		default:
			p.writeStmt(&sb, v.body.stmt, indent+1)
		}
	}
	for i := range v.ensures {
		e := &v.ensures[i]
		sb.WriteString(indentStr(indent + 1))
		sb.WriteString("ensure ")
		sb.WriteString(p.exprString(e.expr, indent+1))
		sb.WriteByte('\n')
	}
	return strings.TrimSuffix(sb.String(), "\n")
}

// writeBranchBody: тело ветки в блочной форме (BlockStmt) — со сдвигом;
// иначе — inline-выражение с отступом.
func (p *printer) writeBranchBody(sb *strings.Builder, e Expr, indent int) {
	if blk, ok := e.(*BlockStmt); ok {
		p.writeBlock(sb, blk, indent)
		return
	}
	sb.WriteString(indentStr(indent))
	sb.WriteString(p.exprString(e, indent))
	sb.WriteByte('\n')
}

// writeBranchTail: " expr" на той же строке (inline) или "\n" + блок.
func (p *printer) writeBranchTail(sb *strings.Builder, e Expr, indent int) {
	if blk, ok := e.(*BlockStmt); ok {
		sb.WriteByte('\n')
		p.writeBlock(sb, blk, indent+1)
		return
	}
	sb.WriteByte(' ')
	sb.WriteString(p.exprString(e, indent))
	sb.WriteByte('\n')
}

// ---- patterns ----

// patternString: *tuplePattern из 1 элемента печатается с trailing
// запятой — иначе "(x)" перепарсится как grouping/identPat, и
// round-trip потеряет узел tuplePattern.
func (p *printer) patternString(pat Pattern) string {
	switch v := pat.(type) {
	case *wildcardPat:
		return "_"
	case *identPat:
		return v.name
	case *literalPat:
		return v.value
	case *constructorPat:
		if len(v.fields) == 0 {
			return v.name
		}
		parts := make([]string, len(v.fields))
		for i := range v.fields {
			parts[i] = p.patternString(v.fields[i].pattern)
		}
		return v.name + "(" + strings.Join(parts, ", ") + ")"
	case *asPat:
		return p.patternString(v.pattern) + " as " + v.ident
	case *tuplePattern:
		parts := make([]string, len(v.patterns))
		for i, sub := range v.patterns {
			parts[i] = p.patternString(sub)
		}
		if len(parts) == 1 {
			return "(" + parts[0] + ",)"
		}
		return "(" + strings.Join(parts, ", ") + ")"
	case *listPattern:
		parts := make([]string, len(v.patterns))
		for i, sub := range v.patterns {
			parts[i] = p.patternString(sub)
		}
		body := strings.Join(parts, ", ")
		if v.hasRest {
			if body != "" {
				body += ", "
			}
			body += ".."
			if v.restName != "" {
				body += v.restName
			}
		}
		return "[" + body + "]"
	case *mapPattern:
		parts := make([]string, len(v.pairs))
		for i := range v.pairs {
			parts[i] = p.exprString(v.pairs[i].key, 0) + " => " +
				p.patternString(v.pairs[i].pat)
		}
		return "%{" + strings.Join(parts, ", ") + "}"
	case *recordPattern:
		parts := make([]string, len(v.fields))
		for i := range v.fields {
			parts[i] = v.fields[i].name + ": " + p.patternString(v.fields[i].pat)
		}
		return v.typ + "{" + strings.Join(parts, ", ") + "}"
	}
	return ""
}

// ---- types ----

func (p *printer) typeString(t Type) string {
	switch v := t.(type) {
	case *intType:
		return "Int"
	case *floatType:
		return "Float"
	case *decimalType:
		return "Decimal"
	case *boolType:
		return "Bool"
	case *strType:
		return "Str"
	case *atomType:
		return "Atom"
	case *unitType:
		return "()"
	case *rangeType:
		return "Range"
	case *pidType:
		return "Pid"
	case *refType:
		return "Ref"
	case *functionType:
		parts := make([]string, len(v.params))
		for i, pt := range v.params {
			parts[i] = p.typeString(pt)
		}
		return "(" + strings.Join(parts, ", ") + ") -> " + p.typeString(v.result)
	case *listType:
		return "List<" + p.typeString(v.element) + ">"
	case *vectorType:
		return "Vector<" + p.typeString(v.element) + ">"
	case *setType:
		return "Set<" + p.typeString(v.element) + ">"
	case *mapType:
		return "Map<" + p.typeString(v.key) + ", " + p.typeString(v.value) + ">"
	case *tupleType:
		parts := make([]string, len(v.fields))
		for i, ft := range v.fields {
			parts[i] = p.typeString(ft)
		}
		if len(parts) == 1 {
			return "(" + parts[0] + ",)"
		}
		return "(" + strings.Join(parts, ", ") + ")"
	case *optionType:
		return "Option<" + p.typeString(v.element) + ">"
	case *resultType:
		return "Result<" + p.typeString(v.ok) + ", " + p.typeString(v.err) + ">"
	case *nominalType:
		if len(v.fields) == 0 {
			return v.name
		}
		parts := make([]string, len(v.fields))
		for i, f := range v.fields {
			parts[i] = f.name + ": " + p.typeString(f.typ)
		}
		return v.name + "{ " + strings.Join(parts, ", ") + " }"
	case *anonymousType:
		parts := make([]string, len(v.fields))
		for i, f := range v.fields {
			parts[i] = f.name + ": " + p.typeString(f.typ)
		}
		return "{ " + strings.Join(parts, ", ") + " }"
	}
	return ""
}

// endsWithPlainIntLit: узел — decimal integer literal, возможно
// под унарным минусом. Только в этом случае конкатенация с '.' даёт
// ошибку лексера. Радикс-литералы (0x/0b/0o) и float (с '.' или 'e')
// scanNumber заканчивает сам, пробел не нужен.
func endsWithPlainIntLit(e Expr) bool {
	switch x := e.(type) {
	case *literalExpr:
		return isDecimalInt(x.value)
	case *unaryExpr:
		return x.op == "-" && endsWithPlainIntLit(x.expr)
	}
	return false
}

func isDecimalInt(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && c != '_' {
			return false
		}
	}
	return true
}
