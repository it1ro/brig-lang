package ast

import "fmt"

// Pattern — интерфейс реализован конретными типами ниже.
// corresponds to grammar: pattern ::= pattern_atom [ "as" lower_ident ]

// wildcardPat — WILDCARD _.
type wildcardPat struct {
	posEnd
}

func (w *wildcardPat) IsExpression() bool    { return false }
func (w *wildcardPat) String() string         { return "_" }
func (w *wildcardPat) AsIdent() string       { return "" }
func (w *wildcardPat) IsStatement() bool      { return false }

// identPat — LOWER_IDENT (простой идентификатор).
type identPat struct {
	posEnd
	name string // имя идентификатора
}

func (i *identPat) IsExpression() bool    { return false }
func (i *identPat) String() string         { return i.name }
func (i *identPat) AsIdent() string       { return i.name }
func (i *identPat) IsStatement() bool      { return false }

// literalPat — литерал в паттерене (число, строка, атом).
type literalPat struct {
	posEnd
	value string // текстовое представление
}

func (l *literalPat) IsExpression() bool    { return false }
func (l *literalPat) String() string         { return l.value }
func (l *literalPat) AsIdent() string       { return "" }
func (l *literalPat) IsStatement() bool      { return false }

// constructorPat — конструктор варианта: Some(x), Ok(v), Error(e), None, Red, Green.
// corresponds to: constructor_pattern ::= UPPER_IDENT [ "(" [ pattern { "," pattern } [ "," ] ] ")" ]
type constructorPat struct {
	posEnd
	name   string       // имя конструктора (Some, Ok, Error, None, Red, Green)
	fields []patternField // список аргументов/полей
}

type patternField struct {
	pattern Pattern
}

// asPat — as-паттерн: pattern as name.
// corresponds to: pattern_atom [ "as" lower_ident ]
type asPat struct {
	posEnd
	pattern Pattern
	ident   string // имя ассоциированного идентификатора
}

func (a *asPat) IsExpression() bool    { return false }
func (a *asPat) String() string         { return fmt.Sprintf("%s as %s", a.pattern, a.ident) }
func (a *asPat) AsIdent() string       { return a.ident }
func (a *asPat) IsStatement() bool      { return false }

// tuplePattern — кортежный паттерн: (1, 2, x).
// corresponds to: tuple_pattern ::= "(" pattern "," [ pattern { "," pattern } ] [ "," ] ")"
type tuplePattern struct {
	posEnd
	patterns []Pattern // список паттернов (минимум 1)
}

func (t *tuplePattern) IsExpression() bool    { return false }
func (t *tuplePattern) String() string         { return fmt.Sprintf("(%s)", joinPatterns(t.patterns, ", ")) }
func (t *tuplePattern) AsIdent() string       { return "" }
func (t *tuplePattern) IsStatement() bool      { return false }

// listPattern — списочный паттерн: [1, ..rest], [1, 2, ..].
// corresponds to: list_pattern ::= "[" [ list_pattern_elem { "," list_pattern_elem } [ "," ] "]"
type listPattern struct {
	posEnd
	patterns []Pattern // список паттернов
	hasRest  bool      // есть ли ..
	restName string    // имя rest-переменной, если есть ..
}

func (l *listPattern) IsExpression() bool    { return false }
func (l *listPattern) String() string         { return fmt.Sprintf("[%s]", joinPatterns(l.patterns, ", ")) }
func (l *listPattern) AsIdent() string       { return "" }
func (l *listPattern) IsStatement() bool      { return false }

// mapPattern — map-паттерн: %{ "a" => a }.
// corresponds to: map_pattern ::= "%{" [ map_pair { "," map_pair } [ "," ] "}"
type mapPattern struct {
	posEnd
	pairs []mapPair // список пар ключ-паттерн
}

type mapPair struct {
	key   Expr   // ключ (выражение)
	pat   Pattern // значение (паттерн)
}

func (m *mapPattern) IsExpression() bool    { return false }
func (m *mapPattern) String() string         { return fmt.Sprintf("%%{%s}", joinMapPairs(m.pairs, ", ")) }
func (m *mapPattern) AsIdent() string       { return "" }
func (m *mapPattern) IsStatement() bool      { return false }

// recordPattern — record-паттерн: User{ id: id, name: name }.
// corresponds to: record_pattern ::= [ UPPER_IDENT ] "{" [ field_pattern { "," field_pattern } [ "," ] "}"
type recordPattern struct {
	posEnd
	typ   string // имя типа (UPPER_IDENT), пусто для анонимной записи
	fields []fieldPat // список field_pattern
}

type fieldPat struct {
	name string // имя поля (LOWER_IDENT)
	pat  Pattern // паттерн значения
}

func (r *recordPattern) IsExpression() bool    { return false }
func (r *recordPattern) String() string         { return fmt.Sprintf("%s{%s}", r.typ, joinFieldPatts(r.fields, ", ")) }
func (r *recordPattern) AsIdent() string       { return "" }
func (r *recordPattern) IsStatement() bool      { return false }

// Helper functions

func joinPatterns(patterns []Pattern, sep string) string {
	if len(patterns) == 0 {
		return ""
	}
	buf := ""
	buf += patterns[0].String()
	for _, p := range patterns[1:] {
		buf += sep + p.String()
	}
	return buf
}

func joinMapPairs(pairs []mapPair, sep string) string {
	if len(pairs) == 0 {
		return ""
	}
	buf := ""
	buf += pairs[0].key.String()
	buf += ": "
	buf += pairs[0].pat.String()
	for _, p := range pairs[1:] {
		buf += sep + p.pat.String()
	}
	return buf
}

func joinFieldPatts(fields []fieldPat, sep string) string {
	if len(fields) == 0 {
		return ""
	}
	buf := ""
	buf += fields[0].name
	for _, f := range fields[1:] {
		buf += sep + f.pat.String()
	}
	return buf
}