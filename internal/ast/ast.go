// Package ast implements the Abstract Syntax Tree for Brig language.
// Corresponds to Part II of the language specification (A1, §2-14).
package ast

// Node — базовый интерфейс для всех узлов AST.
type Node interface {
	// Pos возвращает позицию начала токена.
	Pos() int
	// End возвращает позицию после последнего токена.
	End() int
	// String возвращает строковое представление узла (для отладки).
	String() string
}

// Visitor — паттерн посещения узлов AST.
type Visitor interface {
	VisitNode(node Node) error
	VisitExpr(expr Expr) error
	VisitStmt(stmt Stmt) error
	VisitPattern(pat Pattern) error
	VisitType(t Type) error
	VisitDecl(d Decl) error
}

// Expr — интерфейс для выражений.
// corresponding to grammar: expr ::= lambda_expr | or_expr
type Expr interface {
	Node
	// IsExpression — маркер, отличающий выражение из стейтмента.
	IsExpression() bool
}

// Stmt — интерфейс для стейтментов.
// corresponds to grammar: stmt ::= let_bind | local_fn_decl | expr_stmt
type Stmt interface {
	Node
	// IsStatement — маркер, отличающий стейтмент из выражения.
	IsStatement() bool
}

// Pattern — интерфейс для паттернов.
// corresponds to grammar: pattern ::= pattern_atom [ "as" lower_ident ]
type Pattern interface {
	Node
	// AsIdent — имя ассоциированного идентификатора (для as-паттернов), nil если нет.
	AsIdent() string
}

// Type — интерфейс для типов.
// corresponds to grammar: type_expr ::= type_primary [ "->" type_expr ]
type Type interface {
	Node
	// IsGeneric — указывает, является ли тип параметризированным.
	IsGeneric() bool
	// TypeArgs — аргументы генерика (если есть), nil otherwise.
	TypeArgs() []Type
}

// Decl — интерфейс для деклараций верхнего уровня.
// corresponds to grammar: decl ::= import_decl | alias_decl | type_decl | fn_decl
type Decl interface {
	Node
	// IsTopLevel — маркер, указывающий, что декларация находится на top-уровне.
	IsTopLevel() bool
	// ModuleName — имя модуля, если применимо (например, для type_decl).
	ModuleName() string
}

// ExprType — константный тип для выражений.
const (
	ExprTypeBinary        = "binary"
	ExprTypeUnary         = "unary"
	ExprTypeGrouping      = "grouping"
	ExprTypeLiteral       = "literal"
	ExprTypeVariable      = "variable"
	ExprTypeAssign        = "assign"
	ExprTypeCall          = "call"
	ExprTypePipe          = "pipe"
	ExprTypeIf            = "if"
	ExprTypeMatch         = "match"
	ExprTypeRecv          = "recv"
	ExprTypeWith          = "with"
	ExprTypeSpread        = "spread"
	ExprTypeLambda        = "lambda"
	ExprTypeInterpolation = "interpolation"
	ExprTypeRange         = "range"
	ExprTypeDecimal       = "decimal"
	ExprTypeBytes         = "bytes"
	ExprTypeRegex         = "regex"
	ExprTypeAtom          = "atom"
	ExprTypeBool          = "bool"
	ExprTypeUnit          = "unit"
	ExprTypeRangeLit      = "range_literal"
)

// StmtType — константные типы для стейтментов.
const (
	StmtTypeLet     = "let"
	StmtTypeExpr    = "expr"
	StmtTypeLocalFn = "local_fn"
	StmtTypeBlock   = "block"
	StmtTypeImport  = "import"
	StmtTypeAlias   = "alias"
	StmtTypeType    = "type"
	StmtTypeFn      = "fn"
)

// PatternType — константные типы паттернов.
const (
	PatternTypeWildcard    = "wildcard"
	PatternTypeIdent       = "ident"
	PatternTypeLiteral     = "literal"
	PatternTypeConstructor = "constructor"
	PatternTypeTuple       = "tuple"
	PatternTypeList        = "list"
	PatternTypeMap         = "map"
	PatternTypeRecord      = "record"
	PatternTypeAs          = "as"
)

// TypeCategory — категории типов для отчетов и проверок.
type TypeCategory int

const (
	// Primitive types
	TypeCat_Int TypeCategory = iota // TypeCat_Int is an integer type
	TypeCat_Float // TypeCat_Float is a floating point type
	TypeCat_Decimal                      // exact decimal
	TypeCat_Bool                         // булево
	TypeCat_Str                          // строка
	TypeCat_Atom                         // атом
	TypeCat_Function                     // функция
	TypeCat_Unit                         // unit ()
	TypeCat_Range                        // range
	TypeCat_Pid                          // PID
	TypeCat_Ref                          // Ref

	// Collection types
	TypeCat_List // TypeCat_List is a list type
	TypeCat_Vector // Vector<T>
	TypeCat_Map    // Map<K, V>
	TypeCat_Set    // Set<T>
	TypeCat_Tuple  // Tuple<...>

	// Algebraic types
	TypeCat_Option // TypeCat_Option is an option type
	TypeCat_Result // Result<T, E>

	// Composite/constructed
	TypeCat_Nominal // TypeCat_Nominal is a nominal type
	TypeCat_Anonymous // анонимная запись {...}
)

// String возвращает категорию типа в читаемом виде.
func (t TypeCategory) String() string {
	switch t {
	case TypeCat_Int:
		return "Int"
	case TypeCat_Float:
		return "Float"
	case TypeCat_Decimal:
		return "Decimal"
	case TypeCat_Bool:
		return "Bool"
	case TypeCat_Str:
		return "Str"
	case TypeCat_Atom:
		return "Atom"
	case TypeCat_Function:
		return "Function"
	case TypeCat_Unit:
		return "()"
	case TypeCat_Range:
		return "Range"
	case TypeCat_Pid:
		return "Pid"
	case TypeCat_Ref:
		return "Ref"
	case TypeCat_List:
		return "List<T>"
	case TypeCat_Vector:
		return "Vector<T>"
	case TypeCat_Map:
		return "Map<K, V>"
	case TypeCat_Set:
		return "Set<T>"
	case TypeCat_Tuple:
		return "Tuple<...>"
	case TypeCat_Option:
		return "Option<T>"
	case TypeCat_Result:
		return "Result<T, E>"
	case TypeCat_Nominal:
		return "type X{...}"
	case TypeCat_Anonymous:
		return "{...}"
	default:
		return "unknown"
	}
}

// PosEnd — реализация Node с позиции (value receivers for Node interface compatibility).
type posEnd struct {
	pos0, pos1 int
}

func (n posEnd) Pos() int       { return n.pos0 }
func (n posEnd) End() int       { return n.pos1 }
func (n posEnd) String() string { return "pos0-" + string(rune(n.pos0)) + "+" + string(rune(n.pos1)) }

// Ensure Node interface compliance
var _ Node = (*posEnd)(nil)

// shallowCopyExpr создает поверхностную копию выражения (нужен Visitor).

// shallowCopyStmt создает поверхностную копию стейтмента.

// shallowCopyPattern создает поверхностную копию паттерна.

// shallowCopyType создает поверхностную копию типа.

// shallowCopyDecl создает поверхностную копию декларации.
