package vm

import (
	"fmt"

	"github.com/it1ro/brig-lang/internal/runtime"
)

// PatternKind — вид скомпилированного паттерна.
type PatternKind int

const (
	PatWildcard PatternKind = iota
	PatIdent
	PatLiteral
	PatCtor
	PatTuple
	PatAs
)

// CompiledPattern — паттерн, готовый к исполнению OpMatchLocal.
//
// FailAddr — адрес перехода в байткоде при неудаче. Для MVP паттерны
// не шарятся между точками матчинга, поэтому FailAddr можно
// зашивать в сам паттерн.
type CompiledPattern struct {
	Kind PatternKind

	// PatIdent: слот в locals для связывания.
	Slot int

	// PatLiteral: значение для сравнения.
	Lit runtime.Value

	// PatCtor / PatTuple: тег (пустой для tuple), подпаттерны.
	Tag  string
	Subs []*CompiledPattern

	// PatAs: слот для целого + подпаттерн.
	AsSlot int
	Inner  *CompiledPattern

	// Адрес перехода при неудаче.
	FailAddr int
}

// MatchPattern пытается сопоставить v с p, записывая связывания в locals.
// При неудаче возвращает false; locals могут содержать частичные
// связывания — не полагайтесь на них.
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
		return fmt.Sprintf("$%d", p.Slot)
	case PatLiteral:
		return p.Lit.Inspect()
	case PatAs:
		return FormatCompiledPattern(p.Inner) + fmt.Sprintf(" as $%d", p.AsSlot)
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
	}
	return "?"
}
