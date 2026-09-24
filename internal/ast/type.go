package ast

import "fmt"

// Type — интерфейс реализован конкретными типами ниже.
// grammar: type_expr ::= type_primary [ "->" type_expr ]

// intType — Int.
type intType struct{ posEnd }

func (i *intType) IsExpression() bool { return false }
func (i *intType) String() string     { return "Int" }
func (i *intType) IsGeneric() bool    { return false }
func (i *intType) TypeArgs() []Type   { return nil }

// floatType — Float.
type floatType struct{ posEnd }

func (f *floatType) IsExpression() bool { return false }
func (f *floatType) String() string     { return "Float" }
func (f *floatType) IsGeneric() bool    { return false }
func (f *floatType) TypeArgs() []Type   { return nil }

// decimalType — Decimal.
type decimalType struct{ posEnd }

func (d *decimalType) IsExpression() bool { return false }
func (d *decimalType) String() string     { return "Decimal" }
func (d *decimalType) IsGeneric() bool    { return false }
func (d *decimalType) TypeArgs() []Type   { return nil }

// boolType — Bool.
type boolType struct{ posEnd }

func (b *boolType) IsExpression() bool { return false }
func (b *boolType) String() string     { return "Bool" }
func (b *boolType) IsGeneric() bool    { return false }
func (b *boolType) TypeArgs() []Type   { return nil }

// strType — Str.
type strType struct{ posEnd }

func (s *strType) IsExpression() bool { return false }
func (s *strType) String() string     { return "Str" }
func (s *strType) IsGeneric() bool    { return false }
func (s *strType) TypeArgs() []Type   { return nil }

// atomType — Atom.
type atomType struct{ posEnd }

func (a *atomType) IsExpression() bool { return false }
func (a *atomType) String() string     { return "Atom" }
func (a *atomType) IsGeneric() bool    { return false }
func (a *atomType) TypeArgs() []Type   { return nil }

// functionType — (T1, T2) -> R.
type functionType struct {
	posEnd
	params []Type
	result Type
}

func (f *functionType) IsExpression() bool { return false }
func (f *functionType) String() string {
	return fmt.Sprintf("(%s) -> %s", joinTypeList(f.params, ", "), f.result)
}
func (f *functionType) IsGeneric() bool  { return false }
func (f *functionType) TypeArgs() []Type { return nil }

// unitType — ().
type unitType struct{ posEnd }

func (u *unitType) IsExpression() bool { return false }
func (u *unitType) String() string     { return "()" }
func (u *unitType) IsGeneric() bool    { return false }
func (u *unitType) TypeArgs() []Type   { return nil }

// rangeType — Range.
type rangeType struct{ posEnd }

func (r *rangeType) IsExpression() bool { return false }
func (r *rangeType) String() string     { return "Range" }
func (r *rangeType) IsGeneric() bool    { return false }
func (r *rangeType) TypeArgs() []Type   { return nil }

// pidType — Pid.
type pidType struct{ posEnd }

func (p *pidType) IsExpression() bool { return false }
func (p *pidType) String() string     { return "Pid" }
func (p *pidType) IsGeneric() bool    { return false }
func (p *pidType) TypeArgs() []Type   { return nil }

// refType — Ref.
type refType struct{ posEnd }

func (r *refType) IsExpression() bool { return false }
func (r *refType) String() string     { return "Ref" }
func (r *refType) IsGeneric() bool    { return false }
func (r *refType) TypeArgs() []Type   { return nil }

// listType — List<T>.
type listType struct {
	posEnd
	element Type
}

func (l *listType) IsExpression() bool { return false }
func (l *listType) String() string     { return fmt.Sprintf("List<%s>", l.element) }
func (l *listType) IsGeneric() bool    { return l.element != nil }
func (l *listType) TypeArgs() []Type   { return []Type{l.element} }

// vectorType — Vector<T>.
type vectorType struct {
	posEnd
	element Type
}

func (v *vectorType) IsExpression() bool { return false }
func (v *vectorType) String() string     { return fmt.Sprintf("Vector<%s>", v.element) }
func (v *vectorType) IsGeneric() bool    { return v.element != nil }
func (v *vectorType) TypeArgs() []Type   { return []Type{v.element} }

// mapType — Map<K, V>.
type mapType struct {
	posEnd
	key   Type
	value Type
}

func (m *mapType) IsExpression() bool { return false }
func (m *mapType) String() string {
	return fmt.Sprintf("Map<%s, %s>", m.key, m.value)
}
func (m *mapType) IsGeneric() bool  { return m.key != nil && m.value != nil }
func (m *mapType) TypeArgs() []Type { return []Type{m.key, m.value} }

// setType — Set<T>.
type setType struct {
	posEnd
	element Type
}

func (s *setType) IsExpression() bool { return false }
func (s *setType) String() string     { return fmt.Sprintf("Set<%s>", s.element) }
func (s *setType) IsGeneric() bool    { return s.element != nil }
func (s *setType) TypeArgs() []Type   { return []Type{s.element} }

// tupleType — Tuple<T1, T2>.
type tupleType struct {
	posEnd
	fields []Type
}

func (t *tupleType) IsExpression() bool { return false }
func (t *tupleType) String() string {
	return fmt.Sprintf("Tuple<%s>", joinTypeList(t.fields, ", "))
}
func (t *tupleType) IsGeneric() bool  { return false }
func (t *tupleType) TypeArgs() []Type { return nil }

// nominalType — именованный тип (type User { ... }).
type nominalType struct {
	posEnd
	name   string
	fields []fieldType
}

// fieldType — поле nominal/anonymous.
type fieldType struct {
	name string
	typ  Type
}

func (n *nominalType) IsExpression() bool { return false }
func (n *nominalType) String() string {
	return fmt.Sprintf("type %s { %s }", n.name, joinFieldTypeNames(n.fields, ", "))
}
func (n *nominalType) IsGeneric() bool  { return false }
func (n *nominalType) TypeArgs() []Type { return nil }

// anonymousType — анонимная запись {...}.
type anonymousType struct {
	posEnd
	fields []fieldType
}

func (a *anonymousType) IsExpression() bool { return false }
func (a *anonymousType) String() string {
	return fmt.Sprintf("{ %s }", joinFieldTypeNames(a.fields, ", "))
}
func (a *anonymousType) IsGeneric() bool  { return false }
func (a *anonymousType) TypeArgs() []Type { return nil }

// optionType — Option<T>.
type optionType struct {
	posEnd
	element Type
}

func (o *optionType) IsExpression() bool { return false }
func (o *optionType) String() string     { return fmt.Sprintf("Option<%s>", o.element) }
func (o *optionType) IsGeneric() bool    { return o.element != nil }
func (o *optionType) TypeArgs() []Type   { return []Type{o.element} }

// resultType — Result<T, E>.
type resultType struct {
	posEnd
	ok  Type
	err Type
}

func (r *resultType) IsExpression() bool { return false }
func (r *resultType) String() string {
	return fmt.Sprintf("Result<%s, %s>", r.ok, r.err)
}
func (r *resultType) IsGeneric() bool  { return r.ok != nil && r.err != nil }
func (r *resultType) TypeArgs() []Type { return []Type{r.ok, r.err} }

// ---- helpers ----

func joinTypeList(types []Type, sep string) string {
	if len(types) == 0 {
		return ""
	}
	out := types[0].String()
	for _, t := range types[1:] {
		out += sep + t.String()
	}
	return out
}

func joinFieldTypeNames(fields []fieldType, sep string) string {
	if len(fields) == 0 {
		return ""
	}
	out := fields[0].name + ": " + fields[0].typ.String()
	for _, f := range fields[1:] {
		out += sep + f.name + ": " + f.typ.String()
	}
	return out
}
