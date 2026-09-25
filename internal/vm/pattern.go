package vm

import (
	"fmt"

	"github.com/it1ro/brig-lang/internal/runtime"
)

// PatternKind — вид скомпилированного паттерна.
type PatternKind int

const (
	// PatWildcard — паттерн `_` (любой элемент).
	PatWildcard PatternKind = iota
	PatIdent
	PatLiteral
	PatCtor
	PatTuple
	PatList
	PatMap
	PatAs
)

// MapPatPair — пара ключ-паттерн для PatMap.
type MapPatPair struct {
	Key   runtime.Value
	Value *CompiledPattern
}

// CompiledPattern — паттерн, готовый к исполнению OpMatchLocal.
type CompiledPattern struct {
	Kind PatternKind

	Slot int           // PatIdent
	Lit  runtime.Value // PatLiteral
	Tag  string        // PatCtor
	Subs []*CompiledPattern

	// PatList
	HasRest  bool
	RestSlot int // -1 если rest без имени

	// PatMap
	Pairs []MapPatPair

	// PatAs
	AsSlot int
	Inner  *CompiledPattern
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
		return runtime.Equal(v, p.Lit)

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
		if v.Variant.Tag != p.Tag {
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
			if len(v.List) < len(p.Subs) {
				return false
			}
		} else if len(v.List) != len(p.Subs) {
			return false
		}
		for i, sub := range p.Subs {
			if !MatchPattern(v.List[i], sub, locals) {
				return false
			}
		}
		if p.HasRest && p.RestSlot >= 0 && p.RestSlot < len(locals) {
			rest := v.List[len(p.Subs):]
			locals[p.RestSlot] = runtime.List(rest...)
		}
		return true

	case PatMap:
		if v.Kind != runtime.KindMap {
			return false
		}
		for _, pair := range p.Pairs {
			found := false
			for _, entry := range v.Map {
				if runtime.Equal(entry.Key, pair.Key) {
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
	}
	return "?"
}
