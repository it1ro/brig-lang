package ast

import (
	"bytes"
	"fmt"
)

// importDecl — импорт модуля.
type importDecl struct {
	posEnd
	module string
}

func (e *importDecl) IsExpression() bool { return false }
func (e *importDecl) String() string     { return fmt.Sprintf("import %s", e.module) }
func (e *importDecl) IsTopLevel() bool   { return true }
func (e *importDecl) ModuleName() string { return "" }

// aliasDecl — алиас модуля: alias Http.Client as Http.
type aliasDecl struct {
	posEnd
	original, alias string
}

func (e *aliasDecl) IsExpression() bool { return false }
func (e *aliasDecl) String() string     { return fmt.Sprintf("alias %s as %s", e.original, e.alias) }
func (e *aliasDecl) IsTopLevel() bool   { return true }
func (e *aliasDecl) ModuleName() string { return "" }

// typeDecl — объявление типа: алиас, запись или сумма вариантов.
type typeDecl struct {
	posEnd
	name     string
	generic  []string
	variants []variantInfo
	record   *recordInfo
	alias    Type
}

func (e *typeDecl) IsExpression() bool { return false }
func (e *typeDecl) String() string {
	switch {
	case e.alias != nil:
		return fmt.Sprintf("type %s = %s", e.name, e.alias)
	case e.record != nil:
		return fmt.Sprintf("type %s { %s }", e.name, joinFields(e.record.fields, ", "))
	default:
		var parts []string
		for _, v := range e.variants {
			if len(v.fields) == 0 {
				parts = append(parts, v.name)
			} else {
				parts = append(parts, fmt.Sprintf("%s(%s)", v.name, joinFields(v.fields, ", ")))
			}
		}
		return fmt.Sprintf("type %s { %s }", e.name, join(parts, ", "))
	}
}
func (e *typeDecl) IsTopLevel() bool   { return true }
func (e *typeDecl) ModuleName() string { return e.name }

// funcDecl — объявление функции верхнего уровня.
type funcDecl struct {
	posEnd
	name    string
	clauses []funcClause
}

func (e *funcDecl) IsExpression() bool { return false }
func (e *funcDecl) String() string     { return fmt.Sprintf("fn %s (%d clauses)", e.name, len(e.clauses)) }
func (e *funcDecl) IsTopLevel() bool   { return true }
func (e *funcDecl) ModuleName() string { return "" }

// variantInfo — вариант суммы.
type variantInfo struct {
	name   string
	fields []fieldInfo
}

// fieldInfo — поле варианта/записи.
type fieldInfo struct {
	name string
	typ  Type
}

// recordInfo — поля record-декларации.
type recordInfo struct {
	fields []fieldInfo
}

// funcClause — один клоз верхнеуровневой fn.
type funcClause struct {
	posEnd
	guard  Expr
	params []Pattern
	body   *BlockStmt
}

// ---- helpers ----

func joinFields(fields []fieldInfo, sep string) string {
	if len(fields) == 0 {
		return ""
	}
	buf := bytes.Buffer{}
	for i, f := range fields {
		if i > 0 {
			buf.WriteString(sep)
		}
		fmt.Fprintf(&buf, "%s: %s", f.name, f.typ)
	}
	return buf.String()
}

var (
	_ Decl = (*importDecl)(nil)
	_ Decl = (*aliasDecl)(nil)
	_ Decl = (*typeDecl)(nil)
	_ Decl = (*funcDecl)(nil)
)
