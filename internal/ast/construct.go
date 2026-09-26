package ast

// ---- Expressions ----

// NewBinaryExpr creates an expression node.
func NewBinaryExpr(op string, left, right Expr, pos, end int) Expr {
	return &binaryExpr{posEnd{pos, end}, op, left, right}
}

// NewUnaryExpr creates an expression node.
func NewUnaryExpr(op string, expr Expr, pos, end int) Expr {
	return &unaryExpr{posEnd{pos, end}, op, expr}
}

// NewGroupingExpr creates an expression node.
func NewGroupingExpr(expr Expr, pos, end int) Expr {
	return &groupingExpr{posEnd{pos, end}, expr}
}

// NewLiteralExpr creates an expression node.
func NewLiteralExpr(value string, pos, end int) Expr {
	return &literalExpr{posEnd{pos, end}, value}
}

// NewInterpExpr creates an interpolated string expression.
// len(parts) must equal len(exprs)+1.
func NewInterpExpr(parts []string, exprs []Expr, pos, end int) Expr {
	ps := make([]string, len(parts))
	copy(ps, parts)
	es := make([]Expr, len(exprs))
	copy(es, exprs)
	return &interpExpr{posEnd{pos, end}, ps, es}
}

// NewVariableExpr creates an expression node.
func NewVariableExpr(name string, pos, end int) Expr {
	return &variableExpr{posEnd{pos, end}, name}
}

// NewAssignExpr creates an expression node.
func NewAssignExpr(name string, value Expr, pos, end int) Expr {
	return &assignExpr{posEnd{pos, end}, name, value}
}

// NewCallExpr creates an expression node.
func NewCallExpr(callee Expr, args []Expr, pos, end int) Expr {
	return &callExpr{posEnd{pos, end}, callee, args}
}

// NewPipeExpr creates an expression node.
func NewPipeExpr(expr, callee Expr, args []Expr, pos, end int) Expr {
	return &pipeExpr{posEnd{pos, end}, expr, callee, args}
}

// NewMemberExpr creates an expression node.
func NewMemberExpr(obj Expr, name string, pos, end int) Expr {
	return &memberExpr{posEnd{pos, end}, obj, name}
}

// NewIndexExpr creates an expression node.
func NewIndexExpr(obj, index Expr, pos, end int) Expr {
	return &indexExpr{posEnd{pos, end}, obj, index}
}

// IfBranch — публичное представление ветки else-if.
// IfBranch is an AST argument node.
type IfBranch struct {
	Cond Expr
	Then Expr
}

// NewIfExpr creates an expression node.
func NewIfExpr(cond, thenBody Expr, elseIf []IfBranch, elseBody Expr, pos, end int) Expr {
	branches := make([]ifExpr, 0, len(elseIf))
	for _, b := range elseIf {
		branches = append(branches, ifExpr{cond: b.Cond, thenBody: b.Then})
	}
	return &ifExpr{posEnd{pos, end}, cond, thenBody, branches, elseBody}
}

// MatchBranchArg — публичное представление ветки match.
// MatchBranchArg is an AST argument node.
type MatchBranchArg struct {
	Pattern Pattern
	Body    Expr
}

// NewMatchExpr creates an expression node.
func NewMatchExpr(expr Expr, branches []MatchBranchArg, pos, end int) Expr {
	brs := make([]matchBranch, 0, len(branches))
	for _, b := range branches {
		brs = append(brs, matchBranch{pattern: b.Pattern, expr: b.Body})
	}
	return &matchExpr{posEnd{pos, end}, expr, brs}
}

// RecvBranchArg — публичное представление ветки recv.
// RecvBranchArg is an AST argument node.
type RecvBranchArg struct {
	Pattern Pattern
	Guard   Expr
	Body    Expr
}

// RecvClauseArg — публичное представление клауз else/after у recv.
//
// Соответствует обновлённой структуре recvExpr: elseName + elseBody —
// клауза else; afterTime + afterBody — клауза after. Поля с нулевым
// значением означают отсутствие клаузы.
// RecvClauseArg is an AST argument node.
type RecvClauseArg struct {
	ElseName  string
	ElseBody  Expr
	AfterTime Expr
	AfterBody Expr
}

// NewRecvExpr creates an expression node.
func NewRecvExpr(branches []RecvBranchArg, clauses RecvClauseArg, pos, end int) Expr {
	brs := make([]recvBranch, 0, len(branches))
	for _, b := range branches {
		brs = append(brs, recvBranch{pattern: b.Pattern, guard: b.Guard, expr: b.Body})
	}
	return &recvExpr{
		posEnd{pos, end},
		brs,
		clauses.ElseName,
		clauses.ElseBody,
		clauses.AfterTime,
		clauses.AfterBody,
	}
}

// WithItemArg — публичное представление привязки with.
// WithItemArg is an AST argument node.
type WithItemArg struct {
	Pattern Pattern
	Expr    Expr
}

// WithElseArg — публичное представление ветки with-else.
// WithElseArg is an AST argument node.
type WithElseArg struct {
	Pattern Pattern
	Body    Expr
}

// NewWithExpr принимает тело with отдельным BlockStmt (может быть nil)
// и список веток else.
// NewWithExpr creates an expression node.
func NewWithExpr(items []WithItemArg, body *BlockStmt,
	elseBranches []WithElseArg, pos, end int,
) Expr {
	its := make([]withItem, 0, len(items))
	for _, it := range items {
		its = append(its, withItem{pattern: it.Pattern, expr: it.Expr})
	}
	ebs := make([]withElseBranch, 0, len(elseBranches))
	for _, eb := range elseBranches {
		ebs = append(ebs, withElseBranch{pattern: eb.Pattern, body: eb.Body})
	}
	return &withExpr{posEnd{pos, end}, its, body, ebs}
}

// EnsureArg — публичное представление ensure-клаузы.
// EnsureArg is an AST argument node.
type EnsureArg struct {
	Expr Expr
}

// NewTrapExpr creates an expression node.
func NewTrapExpr(expr Expr, body Stmt, ensures []EnsureArg, pos, end int) Expr {
	es := make([]ensureClause, 0, len(ensures))
	for _, e := range ensures {
		es = append(es, ensureClause{expr: e.Expr})
	}
	var bodyPtr *bodyClause
	if body != nil {
		bodyPtr = &bodyClause{stmt: body}
	}
	return &trapExpr{posEnd{pos, end}, expr, bodyPtr, es}
}

// NewLambdaShortExpr creates an expression node.
func NewLambdaShortExpr(param string, body Expr, pos, end int) Expr {
	return &lambdaShortExpr{posEnd{pos, end}, param, body}
}

// NewLambdaFullExpr creates an expression node.
func NewLambdaFullExpr(params []string, body *BlockStmt, pos, end int) Expr {
	return &lambdaFullExpr{posEnd{pos, end}, params, body}
}

// NewLambdaEmptyExpr creates an expression node.
func NewLambdaEmptyExpr(body Expr, pos, end int) Expr {
	return &lambdaEmptyExpr{posEnd{pos, end}, body}
}

// NewRangeExpr creates an expression node.
func NewRangeExpr(start, end Expr, pos, endPos int) Expr {
	return &rangeExpr{posEnd{pos, endPos}, start, end}
}

// NewDecimalExpr creates an expression node.
func NewDecimalExpr(value string, pos, end int) Expr {
	return &decimalExpr{posEnd{pos, end}, value}
}

// NewBytesExpr creates an expression node.
func NewBytesExpr(value string, pos, end int) Expr {
	return &bytesExpr{posEnd{pos, end}, value}
}

// NewRegexExpr creates an expression node.
func NewRegexExpr(value string, pos, end int) Expr {
	return &regexExpr{posEnd{pos, end}, value}
}

// NewAtomExpr creates an expression node.
func NewAtomExpr(ident string, pos, end int) Expr {
	return &atomExpr{posEnd{pos, end}, ident}
}

// ---- Statements ----

// NewLetBind creates an expression node.
func NewLetBind(pattern Pattern, value Expr, pos, end int) Stmt {
	return &letBind{posEnd{pos, end}, pattern, value}
}

// NewExprStmt creates an expression node.
func NewExprStmt(expr Expr, pos, end int) Stmt {
	return &exprStmt{posEnd{pos, end}, expr}
}

// LocalFnClauseArg — публичное представление клоза локальной fn.
// LocalFnClauseArg is an AST argument node.
type LocalFnClauseArg struct {
	Guard  string
	Params []string
	Body   *BlockStmt
}

// NewLocalFnDecl принимает имя (используется форматтером) и клозы.
// NewLocalFnDecl creates an expression node.
func NewLocalFnDecl(name string, clauses []LocalFnClauseArg, pos, end int) Stmt {
	cs := make([]localFnClause, 0, len(clauses))
	for _, c := range clauses {
		cs = append(cs, localFnClause{
			guard:  c.Guard,
			params: c.Params,
			body:   c.Body,
		})
	}
	return &localFnDecl{posEnd{pos, end}, name, cs}
}

// NewBlockStmt creates an expression node.
func NewBlockStmt(stmts []Stmt, pos, end int) *BlockStmt {
	return &BlockStmt{posEnd{pos, end}, stmts}
}

// ---- Patterns ----

// NewWildcardPat creates an expression node.
func NewWildcardPat(pos, end int) Pattern {
	return &wildcardPat{posEnd{pos, end}}
}

// NewIdentPat creates an expression node.
func NewIdentPat(name string, pos, end int) Pattern {
	return &identPat{posEnd{pos, end}, name}
}

// NewLiteralPat creates an expression node.
func NewLiteralPat(value string, pos, end int) Pattern {
	return &literalPat{posEnd{pos, end}, value}
}

// ConstructorPatArg is an AST argument node.
type ConstructorPatArg struct {
	Pattern Pattern
}

// NewConstructorPat creates an expression node.
func NewConstructorPat(name string, args []ConstructorPatArg, pos, end int) Pattern {
	fs := make([]patternField, 0, len(args))
	for _, a := range args {
		fs = append(fs, patternField{pattern: a.Pattern})
	}
	return &constructorPat{posEnd{pos, end}, name, fs}
}

// NewAsPat creates an expression node.
func NewAsPat(inner Pattern, ident string, pos, end int) Pattern {
	return &asPat{posEnd{pos, end}, inner, ident}
}

// NewTuplePattern creates an expression node.
func NewTuplePattern(patterns []Pattern, pos, end int) Pattern {
	return &tuplePattern{posEnd{pos, end}, patterns}
}

// NewListPattern creates an expression node.
func NewListPattern(patterns []Pattern, hasRest bool, restName string, pos, end int) Pattern {
	return &listPattern{posEnd{pos, end}, patterns, hasRest, restName}
}

// MapPairArg is an AST argument node.
type MapPairArg struct {
	Key Expr
	Pat Pattern
}

// NewMapPattern creates an expression node.
func NewMapPattern(pairs []MapPairArg, pos, end int) Pattern {
	ps := make([]mapPair, 0, len(pairs))
	for _, p := range pairs {
		ps = append(ps, mapPair{key: p.Key, pat: p.Pat})
	}
	return &mapPattern{posEnd{pos, end}, ps}
}

// FieldPatArg is an AST argument node.
type FieldPatArg struct {
	Name string
	Pat  Pattern
}

// NewRecordPattern creates an expression node.
func NewRecordPattern(typ string, fields []FieldPatArg, pos, end int) Pattern {
	fs := make([]fieldPat, 0, len(fields))
	for _, f := range fields {
		fs = append(fs, fieldPat{name: f.Name, pat: f.Pat})
	}
	return &recordPattern{posEnd{pos, end}, typ, fs}
}

// ---- Types ----

// NewIntType creates an expression node.
func NewIntType(pos, end int) Type { return &intType{posEnd{pos, end}} }

// NewFloatType creates an expression node.
func NewFloatType(pos, end int) Type { return &floatType{posEnd{pos, end}} }

// NewDecimalType creates an expression node.
func NewDecimalType(pos, end int) Type { return &decimalType{posEnd{pos, end}} }

// NewBoolType creates an expression node.
func NewBoolType(pos, end int) Type { return &boolType{posEnd{pos, end}} }

// NewStrType creates an expression node.
func NewStrType(pos, end int) Type { return &strType{posEnd{pos, end}} }

// NewAtomType creates an expression node.
func NewAtomType(pos, end int) Type { return &atomType{posEnd{pos, end}} }

// NewUnitType creates an expression node.
func NewUnitType(pos, end int) Type { return &unitType{posEnd{pos, end}} }

// NewRangeType creates an expression node.
func NewRangeType(pos, end int) Type { return &rangeType{posEnd{pos, end}} }

// NewPidType creates an expression node.
func NewPidType(pos, end int) Type { return &pidType{posEnd{pos, end}} }

// NewRefType creates an expression node.
func NewRefType(pos, end int) Type { return &refType{posEnd{pos, end}} }

// NewFunctionType creates an expression node.
func NewFunctionType(params []Type, result Type, pos, end int) Type {
	return &functionType{posEnd{pos, end}, params, result}
}

// NewListType creates an expression node.
func NewListType(element Type, pos, end int) Type {
	return &listType{posEnd{pos, end}, element}
}

// NewVectorType creates an expression node.
func NewVectorType(element Type, pos, end int) Type {
	return &vectorType{posEnd{pos, end}, element}
}

// NewSetType creates an expression node.
func NewSetType(element Type, pos, end int) Type {
	return &setType{posEnd{pos, end}, element}
}

// NewMapType creates an expression node.
func NewMapType(key, value Type, pos, end int) Type {
	return &mapType{posEnd{pos, end}, key, value}
}

// NewTupleType creates an expression node.
func NewTupleType(fields []Type, pos, end int) Type {
	return &tupleType{posEnd{pos, end}, fields}
}

// NewOptionType creates an expression node.
func NewOptionType(element Type, pos, end int) Type {
	return &optionType{posEnd{pos, end}, element}
}

// NewResultType creates an expression node.
func NewResultType(ok, err Type, pos, end int) Type {
	return &resultType{posEnd{pos, end}, ok, err}
}

// FieldTypeArg is an AST argument node.
type FieldTypeArg struct {
	Name  string
	Field Type
}

// NewNominalType creates an expression node.
func NewNominalType(name string, fields []FieldTypeArg, pos, end int) Type {
	fs := make([]fieldType, 0, len(fields))
	for _, f := range fields {
		fs = append(fs, fieldType{name: f.Name, typ: f.Field})
	}
	return &nominalType{posEnd{pos, end}, name, fs}
}

// NewAnonymousType creates an expression node.
func NewAnonymousType(fields []FieldTypeArg, pos, end int) Type {
	fs := make([]fieldType, 0, len(fields))
	for _, f := range fields {
		fs = append(fs, fieldType{name: f.Name, typ: f.Field})
	}
	return &anonymousType{posEnd{pos, end}, fs}
}

// ---- Declarations ----

// NewImportDecl creates an expression node.
func NewImportDecl(module string, pos, end int) Decl {
	return &importDecl{posEnd{pos, end}, module}
}

// NewAliasDecl creates an expression node.
func NewAliasDecl(original, alias string, pos, end int) Decl {
	return &aliasDecl{posEnd{pos, end}, original, alias}
}

// VariantArg is an AST argument node.
type VariantArg struct {
	Name   string
	Fields []Type
}

// NewVariantTypeDecl creates an expression node.
func NewVariantTypeDecl(name string, generic []string, variants []VariantArg, pos, end int) Decl {
	vs := make([]variantInfo, 0, len(variants))
	for _, v := range variants {
		fs := make([]fieldInfo, 0, len(v.Fields))
		for i, ft := range v.Fields {
			fs = append(fs, fieldInfo{name: itoa(i), typ: ft})
		}
		vs = append(vs, variantInfo{name: v.Name, fields: fs})
	}
	return &typeDecl{posEnd{pos, end}, name, generic, vs, nil, nil}
}

// NewRecordTypeDecl creates an expression node.
func NewRecordTypeDecl(name string, generic []string, fields []FieldTypeArg, pos, end int) Decl {
	fs := make([]fieldInfo, 0, len(fields))
	for _, f := range fields {
		fs = append(fs, fieldInfo{name: f.Name, typ: f.Field})
	}
	return &typeDecl{posEnd{pos, end}, name, generic, nil, &recordInfo{fields: fs}, nil}
}

// NewAliasTypeDecl creates an expression node.
func NewAliasTypeDecl(name string, generic []string, target Type, pos, end int) Decl {
	return &typeDecl{posEnd{pos, end}, name, generic, nil, nil, target}
}

// FnClauseArg is an AST argument node.
type FnClauseArg struct {
	Guard  string
	Params []string
	Body   *BlockStmt
}

// NewFuncDecl creates an expression node.
func NewFuncDecl(name string, clauses []FnClauseArg, pos, end int) Decl {
	cs := make([]funcClause, 0, len(clauses))
	for _, c := range clauses {
		cs = append(cs, funcClause{guard: c.Guard, params: c.Params, body: c.Body})
	}
	return &funcDecl{posEnd{pos, end}, name, cs}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	k := len(buf)
	for i > 0 {
		k--
		buf[k] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		k--
		buf[k] = '-'
	}
	return string(buf[k:])
}
