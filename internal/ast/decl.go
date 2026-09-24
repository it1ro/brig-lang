package ast

import (
	"bytes"
	"fmt"
)

// Decl — интерфейс для деклараций верхнего уровня.
// corresponds to grammar: decl ::= import_decl | alias_decl | type_decl | fn_decl

// importDecl — импорт модуля.
type importDecl struct {
	posEnd
	module string // имя модуля
}

func (e *importDecl) IsExpression() bool    { return false }
func (e *importDecl) String() string         { return fmt.Sprintf("import %s", e.module) }
func (e *importDecl) IsTopLevel() bool      { return true }
func (e *importDecl) ModuleName() string    { return "" } // imports don't have a module name per se

// aliasDecl — алиас модуля.
type aliasDecl struct {
	posEnd
	original, alias string // например, "alias Http.Client as Http"
}

func (e *aliasDecl) IsExpression() bool    { return false }
func (e *aliasDecl) String() string         { return fmt.Sprintf("alias %s as %s", e.original, e.alias) }
func (e *aliasDecl) IsTopLevel() bool      { return true }
func (e *aliasDecl) ModuleName() string    { return "" }

// typeDecl — объявление типа.
// corresponds to: type_decl ::= "type" UPPER_IDENT [ "<" generic_params ">" ] type_body
type typeDecl struct {
	posEnd
	name    string           // имя типа (UPPER_IDENT)
	generic []string         // типовые параметры, nil если нет
	variant *variantInfo   // если type X { Ok(T), Error(E) }
	record  *recordInfo    // если type X { id: Int, name: Str }
}

func (e *typeDecl) IsExpression() bool    { return false }
func (e *typeDecl) String() string         { return fmt.Sprintf("type %s %s", e.name, variantOrRecord(e.variant, e.record)) }
func (e *typeDecl) IsTopLevel() bool      { return true }
func (e *typeDecl) ModuleName() string    { return e.name }

// funcDecl — объявление функции на модульном уровне.
// corresponds to: fn_decl ::= fn_clause+
type funcDecl struct {
	posEnd
	name    string      // имя функции (LOWER_IDENT)
	clauses []funcClause
}

func (e *funcDecl) IsExpression() bool    { return false }
func (e *funcDecl) String() string         { return fmt.Sprintf("fn %s -> ...", e.name) }
func (e *funcDecl) IsTopLevel() bool      { return true }
func (e *funcDecl) ModuleName() string    { return "" }

// variantInfo — вспомогательная структура для variant в typeDecl.
type variantInfo struct {
	name   string         // имя варианта (Ok, Error)
	fields []fieldInfo    // список полей, nil если конструктор без полей
}

type fieldInfo struct {
	name string       // имя поля
	type_ Type       // тип поля
}

// recordInfo — вспомогательная структура для record в typeDecl.
type recordInfo struct {
	fields []fieldInfo // список полей записи
}

// funcClause —Clause функции.
type funcClause struct {
	posEnd
	recv   string // имя receiver (пусто для обычных fn)
	guard  string // guard expression (опционально)
	params []string // список параметров
	body   BlockStmt // тело функции
}

// BlockStmt — блок стейтментов (INDENT ... DEDENT), используется внутри fn.
type BlockStmt struct {
	posEnd
	stmts []Stmt
}

func (b *BlockStmt) IsExpression() bool    { return false }
func (b *BlockStmt) String() string         { return fmt.Sprintf("block(%d stmts)", len(b.stmts)) }
func (b *BlockStmt) IsStatement() bool      { return true }

// Ensure Decl interface compliance
var _ Decl = (*importDecl)(nil)
var _ Decl = (*aliasDecl)(nil)
var _ Decl = (*typeDecl)(nil)
var _ Decl = (*funcDecl)(nil)

// helpers
func variantOrRecord(v *variantInfo, r *recordInfo) string {
	if v != nil {
		return fmt.Sprintf("{ %s }", joinFields(v.fields, ", "))
	}
	if r != nil {
		return fmt.Sprintf("{ %s }", joinFields(r.fields, ", "))
	}
	return ""
}

func joinFields(fields []fieldInfo, sep string) string {
	if len(fields) == 0 {
		return ""
	}
	buf := bytes.Buffer{}
	for i, f := range fields {
		if i > 0 {
			buf.WriteString(sep)
		}
		buf.WriteString(fmt.Sprintf("%s: %s", f.name, f.type_))
	}
	return buf.String()
}

func paramsToString(params []string) string {
	return join(params, ", ")
}

func elementTypeArgs(t Type) []Type {
	// Возвращает [T] для List<T>, Vector<T> и Set<T>
	switch tt := t.(type) {
	case *listType:
		return []Type{tt.element}
	case *vectorType:
		return []Type{tt.element}
	case *setType:
		return []Type{tt.element}
	}
	return nil
}

func joinTypes(types []Type, sep string) string {
	if len(types) == 0 {
		return ""
	}
	buf := bytes.Buffer{}
	for i, t := range types {
		if i > 0 {
			buf.WriteString(sep)
		}
		buf.WriteString(t.String())
	}
	return buf.String()
}