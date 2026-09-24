package ast

import "fmt"

// Pattern — интерфейс реализован конкретными типами ниже.
// grammar: pattern ::= pattern_atom [ "as" lower_ident ]
//
// Требует Node (Pos/End/String) + AsIdent().
// Дополнительные IsExpression/IsStatement оставлены для совместимости
// с существующими вызовами, но не входят в интерфейс.

// wildcardPat — WILDCARD _.
type wildcardPat struct{ posEnd }

func (w *wildcardPat) IsExpression() bool { return false }
func (w *wildcardPat) IsStatement() bool  { return false }
func (w *wildcardPat) String() string     { return "_" }
func (w *wildcardPat) AsIdent() string    { return "" }

// identPat — LOWER_IDENT (простой идентификатор).
type identPat struct {
	posEnd
	name string
}

func (i *identPat) IsExpression() bool { return false }
func (i *identPat) IsStatement() bool  { return false }
func (i *identPat) String() string     { return i.name }
func (i *identPat) AsIdent() string    { return i.name }

// literalPat — литерал в паттерне.
type literalPat struct {
	posEnd
	value string
}

func (l *literalPat) IsExpression() bool { return false }
func (l *literalPat) IsStatement() bool  { return false }
func (l *literalPat) String() string     { return l.value }
func (l *literalPat) AsIdent() string    { return "" }

// constructorPat — Some(x), Ok(v), Error(e), None, Red, Green.
type constructorPat struct {
	posEnd
	name   string
	fields []patternField
}

// patternField — аргумент конструктора.
type patternField struct {
	pattern Pattern
}

func (c *constructorPat) IsExpression() bool { return false }
func (c *constructorPat) IsStatement() bool  { return false }
func (c *constructorPat) AsIdent() string    { return "" }
func (c *constructorPat) String() string {
	if len(c.fields) == 0 {
		return c.name
	}
	parts := make([]string, 0, len(c.fields))
	for _, f := range c.fields {
		parts = append(parts, f.pattern.String())
	}
	return c.name + "(" + join(parts, ", ") + ")"
}

// asPat — pattern as name.
type asPat struct {
	posEnd
	pattern Pattern
	ident   string
}

func (a *asPat) IsExpression() bool { return false }
func (a *asPat) IsStatement() bool  { return false }
func (a *asPat) String() string     { return fmt.Sprintf("%s as %s", a.pattern, a.ident) }
func (a *asPat) AsIdent() string    { return a.ident }

// tuplePattern — (p1, p2, ...).
type tuplePattern struct {
	posEnd
	patterns []Pattern
}

func (t *tuplePattern) IsExpression() bool { return false }
func (t *tuplePattern) IsStatement() bool  { return false }
func (t *tuplePattern) AsIdent() string    { return "" }
func (t *tuplePattern) String() string {
	return "(" + joinPatterns(t.patterns, ", ") + ")"
}

// listPattern — [p1, ..rest].
type listPattern struct {
	posEnd
	patterns []Pattern
	hasRest  bool
	restName string
}

func (l *listPattern) IsExpression() bool { return false }
func (l *listPattern) IsStatement() bool  { return false }
func (l *listPattern) AsIdent() string    { return "" }
func (l *listPattern) String() string {
	s := "[" + joinPatterns(l.patterns, ", ")
	if l.hasRest {
		if len(l.patterns) > 0 {
			s += ", "
		}
		s += ".."
		if l.restName != "" {
			s += l.restName
		}
	}
	return s + "]"
}

// mapPattern — %{ k => p }.
type mapPattern struct {
	posEnd
	pairs []mapPair
}

// mapPair — пара ключ-паттерн.
type mapPair struct {
	key Expr
	pat Pattern
}

func (m *mapPattern) IsExpression() bool { return false }
func (m *mapPattern) IsStatement() bool  { return false }
func (m *mapPattern) AsIdent() string    { return "" }
func (m *mapPattern) String() string {
	return "%{" + joinMapPairs(m.pairs, ", ") + "}"
}

// recordPattern — User{ f: p } / { f: p }.
type recordPattern struct {
	posEnd
	typ    string
	fields []fieldPat
}

// fieldPat — поле record-паттерна.
type fieldPat struct {
	name string
	pat  Pattern
}

func (r *recordPattern) IsExpression() bool { return false }
func (r *recordPattern) IsStatement() bool  { return false }
func (r *recordPattern) AsIdent() string    { return "" }
func (r *recordPattern) String() string {
	return r.typ + "{" + joinFieldPatts(r.fields, ", ") + "}"
}

// ---- helpers ----

func joinPatterns(patterns []Pattern, sep string) string {
	if len(patterns) == 0 {
		return ""
	}
	out := patterns[0].String()
	for _, p := range patterns[1:] {
		out += sep + p.String()
	}
	return out
}

func joinMapPairs(pairs []mapPair, sep string) string {
	if len(pairs) == 0 {
		return ""
	}
	out := pairs[0].key.String() + " => " + pairs[0].pat.String()
	for _, p := range pairs[1:] {
		out += sep + p.key.String() + " => " + p.pat.String()
	}
	return out
}

func joinFieldPatts(fields []fieldPat, sep string) string {
	if len(fields) == 0 {
		return ""
	}
	out := fields[0].name + ": " + fields[0].pat.String()
	for _, f := range fields[1:] {
		out += sep + f.name + ": " + f.pat.String()
	}
	return out
}
