package ast

// ---- Expressions ----

func NewBinaryExpr(op string, left, right Expr, pos, end int) Expr {
	return &binaryExpr{posEnd{pos, end}, op, left, right}
}

func NewUnaryExpr(op string, expr Expr, pos, end int) Expr {
	return &unaryExpr{posEnd{pos, end}, op, expr}
}

func NewGroupingExpr(expr Expr, pos, end int) Expr {
	return &groupingExpr{posEnd{pos, end}, expr}
}

func NewLiteralExpr(value string, pos, end int) Expr {
	return &literalExpr{posEnd{pos, end}, value}
}

func NewVariableExpr(name string, pos, end int) Expr {
	return &variableExpr{posEnd{pos, end}, name}
}

func NewAssignExpr(name string, value Expr, pos, end int) Expr {
	return &assignExpr{posEnd{pos, end}, name, value}
}

func NewCallExpr(callee Expr, args []Expr, pos, end int) Expr {
	return &callExpr{posEnd{pos, end}, callee, args}
}

func NewPipeExpr(expr, callee Expr, args []Expr, pos, end int) Expr {
	return &pipeExpr{posEnd{pos, end}, expr, callee, args}
}

func NewMemberExpr(obj Expr, name string, pos, end int) Expr {
	return &memberExpr{posEnd{pos, end}, obj, name}
}

func NewIndexExpr(obj, index Expr, pos, end int) Expr {
	return &indexExpr{posEnd{pos, end}, obj, index}
}

// IfBranch — публичное представление ветки else-if.
type IfBranch struct {
	Cond Expr
	Then Expr
}

func NewIfExpr(cond, thenBody Expr, elseIf []IfBranch, elseBody Expr, pos, end int) Expr {
	branches := make([]ifExpr, 0, len(elseIf))
	for _, b := range elseIf {
		branches = append(branches, ifExpr{cond: b.Cond, thenBody: b.Then})
	}
	return &ifExpr{posEnd{pos, end}, cond, thenBody, branches, elseBody}
}

// MatchBranchArg — публичное представление ветки match.
type MatchBranchArg struct {
	Pattern Pattern
	Body    Expr
}

func NewMatchExpr(expr Expr, branches []MatchBranchArg, pos, end int) Expr {
	brs := make([]matchBranch, 0, len(branches))
	for _, b := range branches {
		brs = append(brs, matchBranch{pattern: b.Pattern, expr: b.Body})
	}
	return &matchExpr{posEnd{pos, end}, expr, brs}
}

// RecvBranchArg — публичное представление ветки recv.
type RecvBranchArg struct {
	Pattern Pattern
	Body    Expr
}

// RecvClauseArg — публичное представление клауз else/after у recv.
//
// Соответствует обновлённой структуре recvExpr: elseName + elseBody —
// клауза else; afterTime + afterBody — клауза after. Поля с нулевым
// значением означают отсутствие клаузы.
type RecvClauseArg struct {
	ElseName  string
	ElseBody  Expr
	AfterTime Expr
	AfterBody Expr
}

func NewRecvExpr(branches []RecvBranchArg, clauses RecvClauseArg, pos, end int) Expr {
	brs := make([]recvBranch, 0, len(branches))
	for _, b := range branches {
		brs = append(brs, recvBranch{pattern: b.Pattern, expr: b.Body})
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
type WithItemArg struct {
	Pattern Pattern
	Expr    Expr
}

// WithElseArg — публичное представление ветки with-else.
type WithElseArg struct {
	Pattern Pattern
	Body    Expr
}

// NewWithExpr принимает тело with отдельным BlockStmt (может быть nil)
// и список веток else.
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
type EnsureArg struct {
	Expr Expr
}

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

func NewLambdaShortExpr(param string, body Expr, pos, end int) Expr {
	return &lambdaShortExpr{posEnd{pos, end}, param, body}
}

func NewLambdaFullExpr(params []string, body *BlockStmt, pos, end int) Expr {
	return &lambdaFullExpr{posEnd{pos, end}, params, body}
}

func NewLambdaEmptyExpr(body Expr, pos, end int) Expr {
	return &lambdaEmptyExpr{posEnd{pos, end}, body}
}

func NewRangeExpr(start, end Expr, pos, endPos int) Expr {
	return &rangeExpr{posEnd{pos, endPos}, start, end}
}

func NewDecimalExpr(value string, pos, end int) Expr {
	return &decimalExpr{posEnd{pos, end}, value}
}

func NewBytesExpr(value string, pos, end int) Expr {
	return &bytesExpr{posEnd{pos, end}, value}
}

func NewRegexExpr(value string, pos, end int) Expr {
	return &regexExpr{posEnd{pos, end}, value}
}

func NewAtomExpr(ident string, pos, end int) Expr {
	return &atomExpr{posEnd{pos, end}, ident}
}

// ---- Statements ----

func NewLetBind(pattern Pattern, value Expr, pos, end int) Stmt {
	return &letBind{posEnd{pos, end}, pattern, value}
}

func NewExprStmt(expr Expr, pos, end int) Stmt {
	return &exprStmt{posEnd{pos, end}, expr}
}

// LocalFnClauseArg — публичное представление клоза локальной fn.
type LocalFnClauseArg struct {
	Guard  string
	Params []string
	Body   *BlockStmt
}

// NewLocalFnDecl принимает имя (используется форматтером) и клозы.
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

func NewBlockStmt(stmts []Stmt, pos, end int) *BlockStmt {
	return &BlockStmt{posEnd{pos, end}, stmts}
}

// ---- Patterns ----

func NewWildcardPat(pos, end int) Pattern {
	return &wildcardPat{posEnd{pos, end}}
}

func NewIdentPat(name string, pos, end int) Pattern {
	return &identPat{posEnd{pos, end}, name}
}

func NewLiteralPat(value string, pos, end int) Pattern {
	return &literalPat{posEnd{pos, end}, value}
}

type ConstructorPatArg struct {
	Pattern Pattern
}

func NewConstructorPat(name string, args []ConstructorPatArg, pos, end int) Pattern {
	fs := make([]patternField, 0, len(args))
	for _, a := range args {
		fs = append(fs, patternField{pattern: a.Pattern})
	}
	return &constructorPat{posEnd{pos, end}, name, fs}
}

func NewAsPat(inner Pattern, ident string, pos, end int) Pattern {
	return &asPat{posEnd{pos, end}, inner, ident}
}

func NewTuplePattern(patterns []Pattern, pos, end int) Pattern {
	return &tuplePattern{posEnd{pos, end}, patterns}
}

func NewListPattern(patterns []Pattern, hasRest bool, restName string, pos, end int) Pattern {
	return &listPattern{posEnd{pos, end}, patterns, hasRest, restName}
}

type MapPairArg struct {
	Key Expr
	Pat Pattern
}

func NewMapPattern(pairs []MapPairArg, pos, end int) Pattern {
	ps := make([]mapPair, 0, len(pairs))
	for _, p := range pairs {
		ps = append(ps, mapPair{key: p.Key, pat: p.Pat})
	}
	return &mapPattern{posEnd{pos, end}, ps}
}

type FieldPatArg struct {
	Name string
	Pat  Pattern
}

func NewRecordPattern(typ string, fields []FieldPatArg, pos, end int) Pattern {
	fs := make([]fieldPat, 0, len(fields))
	for _, f := range fields {
		fs = append(fs, fieldPat{name: f.Name, pat: f.Pat})
	}
	return &recordPattern{posEnd{pos, end}, typ, fs}
}

// ---- Types ----

func NewIntType(pos, end int) Type     { return &intType{posEnd{pos, end}} }
func NewFloatType(pos, end int) Type   { return &floatType{posEnd{pos, end}} }
func NewDecimalType(pos, end int) Type { return &decimalType{posEnd{pos, end}} }
func NewBoolType(pos, end int) Type    { return &boolType{posEnd{pos, end}} }
func NewStrType(pos, end int) Type     { return &strType{posEnd{pos, end}} }
func NewAtomType(pos, end int) Type    { return &atomType{posEnd{pos, end}} }
func NewUnitType(pos, end int) Type    { return &unitType{posEnd{pos, end}} }
func NewRangeType(pos, end int) Type   { return &rangeType{posEnd{pos, end}} }
func NewPidType(pos, end int) Type     { return &pidType{posEnd{pos, end}} }
func NewRefType(pos, end int) Type     { return &refType{posEnd{pos, end}} }

func NewFunctionType(params []Type, result Type, pos, end int) Type {
	return &functionType{posEnd{pos, end}, params, result}
}

func NewListType(element Type, pos, end int) Type {
	return &listType{posEnd{pos, end}, element}
}

func NewVectorType(element Type, pos, end int) Type {
	return &vectorType{posEnd{pos, end}, element}
}

func NewSetType(element Type, pos, end int) Type {
	return &setType{posEnd{pos, end}, element}
}

func NewMapType(key, value Type, pos, end int) Type {
	return &mapType{posEnd{pos, end}, key, value}
}

func NewTupleType(fields []Type, pos, end int) Type {
	return &tupleType{posEnd{pos, end}, fields}
}

func NewOptionType(element Type, pos, end int) Type {
	return &optionType{posEnd{pos, end}, element}
}

func NewResultType(ok, err Type, pos, end int) Type {
	return &resultType{posEnd{pos, end}, ok, err}
}

type FieldTypeArg struct {
	Name  string
	Field Type
}

func NewNominalType(name string, fields []FieldTypeArg, pos, end int) Type {
	fs := make([]fieldType, 0, len(fields))
	for _, f := range fields {
		fs = append(fs, fieldType{name: f.Name, typ: f.Field})
	}
	return &nominalType{posEnd{pos, end}, name, fs}
}

func NewAnonymousType(fields []FieldTypeArg, pos, end int) Type {
	fs := make([]fieldType, 0, len(fields))
	for _, f := range fields {
		fs = append(fs, fieldType{name: f.Name, typ: f.Field})
	}
	return &anonymousType{posEnd{pos, end}, fs}
}

// ---- Declarations ----

func NewImportDecl(module string, pos, end int) Decl {
	return &importDecl{posEnd{pos, end}, module}
}

func NewAliasDecl(original, alias string, pos, end int) Decl {
	return &aliasDecl{posEnd{pos, end}, original, alias}
}

type VariantArg struct {
	Name   string
	Fields []Type
}

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

func NewRecordTypeDecl(name string, generic []string, fields []FieldTypeArg, pos, end int) Decl {
	fs := make([]fieldInfo, 0, len(fields))
	for _, f := range fields {
		fs = append(fs, fieldInfo{name: f.Name, typ: f.Field})
	}
	return &typeDecl{posEnd{pos, end}, name, generic, nil, &recordInfo{fields: fs}, nil}
}

func NewAliasTypeDecl(name string, generic []string, target Type, pos, end int) Decl {
	return &typeDecl{posEnd{pos, end}, name, generic, nil, nil, target}
}

type FnClauseArg struct {
	Guard  string
	Params []string
	Body   *BlockStmt
}

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
