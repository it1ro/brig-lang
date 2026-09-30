package vm

import (
	"fmt"
	"strings"

	"github.com/it1ro/brig-lang/internal/runtime"
)

// PatternKind — вид скомпилированного паттерна.
type PatternKind int

// Виды скомпилированных паттернов. PatWildcard — паттерн `_` (любой элемент).
const (
	PatWildcard PatternKind = iota
	PatIdent
	PatLiteral
	PatCtor
	PatTuple
	PatList
	PatMap
	PatAs
	PatRecord
	PatStrConcat
)

// MapPatPair — пара ключ-паттерн для PatMap.
type MapPatPair struct {
	Key   runtime.Value
	Value *CompiledPattern
}

// RecordPatField — пара поле-паттерн для PatRecord.
type RecordPatField struct {
	Name  string
	Value *CompiledPattern
}

// CompiledPattern — паттерн, готовый к исполнению OpMatchLocal.
type CompiledPattern struct {
	Kind PatternKind

	Slot int           // PatIdent
	Lit  runtime.Value // PatLiteral
	Tag  string        // PatCtor
	Subs []*CompiledPattern
	// PatCtor: тип пользовательского варианта, "" — встроенный (§14.2)
	VariantType string

	// PatList
	HasRest  bool
	RestSlot int // -1 если rest без имени

	// PatMap
	Pairs []MapPatPair

	// PatRecord: Tag — имя типа, "" для анонимного паттерна (§4.8)
	Fields []RecordPatField

	// PatAs
	AsSlot int
	Inner  *CompiledPattern

	// PatStrConcat: "prefix" <> rest — Inner holds the rest pattern.
	StrPrefix string
}

// Slots возвращает регистры, которые MatchPattern записывает при успешном
// сопоставлении. Используется vm.Verify для рёбер MATCHLOCAL (только ip+2).
func (p *CompiledPattern) Slots() []int {
	if p == nil {
		return nil
	}
	var slots []int
	var walk func(*CompiledPattern)
	walk = func(p *CompiledPattern) {
		if p == nil {
			return
		}
		switch p.Kind {
		case PatIdent:
			if p.Slot >= 0 {
				slots = append(slots, p.Slot)
			}
		case PatAs:
			if p.AsSlot >= 0 {
				slots = append(slots, p.AsSlot)
			}
			walk(p.Inner)
		case PatList:
			if p.HasRest && p.RestSlot >= 0 {
				slots = append(slots, p.RestSlot)
			}
			for _, sub := range p.Subs {
				walk(sub)
			}
		case PatCtor, PatTuple:
			for _, sub := range p.Subs {
				walk(sub)
			}
		case PatMap:
			for _, pair := range p.Pairs {
				walk(pair.Value)
			}
		case PatRecord:
			for _, f := range p.Fields {
				walk(f.Value)
			}
		case PatStrConcat:
			walk(p.Inner)
		case PatWildcard, PatLiteral:
			// nothing
		}
	}
	walk(p)
	return slots
}

// MatchPattern пытается сопоставить v с p, записывая связывания в locals.
func MatchPattern(v runtime.Value, p *CompiledPattern, locals []runtime.Value) bool {
	if p == nil {
		return false
	}
	switch p.Kind {
	case PatWildcard:
		return true

	case PatIdent:
		if p.Slot >= 0 && p.Slot < len(locals) {
			locals[p.Slot] = v
		}
		return true

	case PatLiteral:
		return runtime.MatchEqual(v, p.Lit)

	case PatStrConcat:
		if v.Kind != runtime.KindStr || !strings.HasPrefix(v.Str, p.StrPrefix) {
			return false
		}
		return MatchPattern(runtime.Str(v.Str[len(p.StrPrefix):]), p.Inner, locals)

	case PatAs:
		if !MatchPattern(v, p.Inner, locals) {
			return false
		}
		if p.AsSlot >= 0 && p.AsSlot < len(locals) {
			locals[p.AsSlot] = v
		}
		return true

	case PatCtor:
		if v.Kind != runtime.KindVariant || v.Variant == nil {
			return false
		}
		if v.Variant.Tag != p.Tag || v.Variant.Type != p.VariantType {
			return false
		}
		if len(v.Variant.Args) != len(p.Subs) {
			return false
		}
		for i, sub := range p.Subs {
			if !MatchPattern(v.Variant.Args[i], sub, locals) {
				return false
			}
		}
		return true

	case PatTuple:
		if v.Kind != runtime.KindTuple {
			return false
		}
		if len(v.Tuple) != len(p.Subs) {
			return false
		}
		for i, sub := range p.Subs {
			if !MatchPattern(v.Tuple[i], sub, locals) {
				return false
			}
		}
		return true

	case PatList:
		if v.Kind != runtime.KindList {
			return false
		}
		if p.HasRest {
			if v.Len() < len(p.Subs) {
				return false
			}
		} else if v.Len() != len(p.Subs) {
			return false
		}
		rest := v
		for _, sub := range p.Subs {
			h, t, _ := rest.Uncons()
			if !MatchPattern(h, sub, locals) {
				return false
			}
			rest = t
		}
		if p.HasRest && p.RestSlot >= 0 && p.RestSlot < len(locals) {
			locals[p.RestSlot] = rest
		}
		return true

	case PatMap:
		if v.Kind != runtime.KindMap {
			return false
		}
		for _, pair := range p.Pairs {
			found := false
			for _, entry := range v.Entries() {
				if runtime.MatchEqual(entry.Key, pair.Key) {
					if !MatchPattern(entry.Val, pair.Value, locals) {
						return false
					}
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true

	case PatRecord:
		// §4.8: вид записи должен совпасть; §9.6: поля — частично.
		if v.Kind != runtime.KindRecord || v.Record == nil || v.Record.Type != p.Tag {
			return false
		}
		for _, f := range p.Fields {
			fv, ok := v.Record.Get(f.Name)
			if !ok || !MatchPattern(fv, f.Value, locals) {
				return false
			}
		}
		return true
	}
	return false
}

// FormatCompiledPattern — для отладки и дизассемблера.
func FormatCompiledPattern(p *CompiledPattern) string {
	if p == nil {
		return "nil"
	}
	switch p.Kind {
	case PatWildcard:
		return "_"
	case PatIdent:
		return fmt.Sprintf("r%d", p.Slot)
	case PatLiteral:
		return p.Lit.Inspect()
	case PatStrConcat:
		return fmt.Sprintf("%q <> %s", p.StrPrefix, FormatCompiledPattern(p.Inner))
	case PatAs:
		return FormatCompiledPattern(p.Inner) + fmt.Sprintf(" as r%d", p.AsSlot)
	case PatCtor:
		s := p.Tag
		if len(p.Subs) > 0 {
			s += "("
			for i, sub := range p.Subs {
				if i > 0 {
					s += ", "
				}
				s += FormatCompiledPattern(sub)
			}
			s += ")"
		}
		return s
	case PatTuple:
		s := "("
		for i, sub := range p.Subs {
			if i > 0 {
				s += ", "
			}
			s += FormatCompiledPattern(sub)
		}
		return s + ")"
	case PatList:
		s := "["
		for i, sub := range p.Subs {
			if i > 0 {
				s += ", "
			}
			s += FormatCompiledPattern(sub)
		}
		if p.HasRest {
			if len(p.Subs) > 0 {
				s += ", "
			}
			s += ".."
			if p.RestSlot >= 0 {
				s += fmt.Sprintf("r%d", p.RestSlot)
			}
		}
		return s + "]"
	case PatMap:
		s := "%{"
		for i, pair := range p.Pairs {
			if i > 0 {
				s += ", "
			}
			s += pair.Key.Inspect() + " => " + FormatCompiledPattern(pair.Value)
		}
		return s + "}"
	case PatRecord:
		s := p.Tag + "{"
		for i, f := range p.Fields {
			if i > 0 {
				s += ", "
			}
			s += f.Name + ": " + FormatCompiledPattern(f.Value)
		}
		return s + "}"
	}
	return "?"
}
