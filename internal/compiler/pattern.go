package compiler

import (
	"fmt"
	"slices"
	"strings"

	"github.com/it1ro/brig-lang/internal/ast"
	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/vm"
)

// patternNames — имена, которые связывает паттерн, в порядке обхода
// compilePattern (`_` и безымянный `..` не связывают).
func patternNames(pat ast.Pattern, acc []string) []string {
	switch p := pat.(type) {
	case ast.IdentPattern:
		acc = append(acc, p.IdentName())
	case ast.PatternCtor:
		for _, a := range p.CtorArgs() {
			acc = patternNames(a, acc)
		}
	case ast.PatternTuple:
		for _, a := range p.TupleElems() {
			acc = patternNames(a, acc)
		}
	case ast.PatternList:
		for _, a := range p.ListElems() {
			acc = patternNames(a, acc)
		}
		if p.ListHasRest() && p.ListRestName() != "" {
			acc = append(acc, p.ListRestName())
		}
	case ast.PatternMapAccessor:
		for _, pair := range p.MapPairsAccessor() {
			acc = patternNames(pair.Pat, acc)
		}
	case ast.PatternRecord:
		for _, f := range p.RecordFields() {
			acc = patternNames(f.Pat, acc)
		}
	case ast.PatternAs:
		acc = patternNames(p.AsInner(), acc)
		acc = append(acc, p.AsName())
	case ast.PatternStrConcat:
		acc = patternNames(p.ConcatRest(), acc)
	}
	return acc
}

// patSlot — регистр для имени паттерна: предаллоцированный из
// fc.patSlots (trap с ensure) или новый.
func (fc *funcCompiler) patSlot(name string) int {
	if r, ok := fc.patSlots[name]; ok {
		delete(fc.patSlots, name)
		return r
	}
	return fc.allocReg()
}

func (fc *funcCompiler) compilePattern(pat ast.Pattern) (*vm.CompiledPattern, error) {
	switch p := pat.(type) {
	case ast.IdentPattern:
		slot := fc.patSlot(p.IdentName())
		fc.bindLocal(p.IdentName(), slot)
		return &vm.CompiledPattern{Kind: vm.PatIdent, Slot: slot}, nil

	case ast.LiteralPattern:
		lit, err := parseLiteralValue(p.ValueStr())
		if err != nil {
			return nil, err
		}
		return &vm.CompiledPattern{Kind: vm.PatLiteral, Lit: lit}, nil

	case ast.PatternCtor:
		subs := make([]*vm.CompiledPattern, 0, len(p.CtorArgs()))
		for _, a := range p.CtorArgs() {
			sub, err := fc.compilePattern(a)
			if err != nil {
				return nil, err
			}
			subs = append(subs, sub)
		}
		name, typ := p.CtorName(), fc.compiler.cur.ctors[p.CtorName()].typ
		if strings.Contains(name, ".") {
			at := posOf(p)
			ref, tag, err := fc.resolvePath(strings.Split(name, "."), at)
			if err != nil {
				return nil, err
			}
			ct, err := moduleCtor(ref, tag, at)
			if err != nil {
				return nil, err
			}
			name, typ = tag, ct.typ
		}
		return &vm.CompiledPattern{Kind: vm.PatCtor, Tag: name, Subs: subs, VariantType: typ}, nil

	case ast.PatternTuple:
		subs := make([]*vm.CompiledPattern, 0, len(p.TupleElems()))
		for _, a := range p.TupleElems() {
			sub, err := fc.compilePattern(a)
			if err != nil {
				return nil, err
			}
			subs = append(subs, sub)
		}
		return &vm.CompiledPattern{Kind: vm.PatTuple, Subs: subs}, nil

	case ast.PatternList:
		subs := make([]*vm.CompiledPattern, 0, len(p.ListElems()))
		for _, a := range p.ListElems() {
			sub, err := fc.compilePattern(a)
			if err != nil {
				return nil, err
			}
			subs = append(subs, sub)
		}
		restSlot := -1
		if p.ListHasRest() && p.ListRestName() != "" {
			restSlot = fc.patSlot(p.ListRestName())
			fc.bindLocal(p.ListRestName(), restSlot)
		}
		return &vm.CompiledPattern{
			Kind:     vm.PatList,
			Subs:     subs,
			HasRest:  p.ListHasRest(),
			RestSlot: restSlot,
		}, nil

	case ast.PatternMapAccessor:
		pairs := make([]vm.MapPatPair, 0, len(p.MapPairsAccessor()))
		for _, pair := range p.MapPairsAccessor() {
			key, err := fc.compileConstExpr(pair.Key)
			if err != nil {
				return nil, err
			}
			sub, err := fc.compilePattern(pair.Pat)
			if err != nil {
				return nil, err
			}
			pairs = append(pairs, vm.MapPatPair{Key: key, Value: sub})
		}
		return &vm.CompiledPattern{Kind: vm.PatMap, Pairs: pairs}, nil

	case ast.PatternRecord:
		at := posOf(p)
		var declared []string
		tag := ""
		if t := p.RecordType(); t != "" {
			var err error
			tag, declared, err = fc.recordType(t, at)
			if err != nil {
				return nil, err
			}
		}
		fields := make([]vm.RecordPatField, 0, len(p.RecordFields()))
		seen := map[string]bool{}
		for _, f := range p.RecordFields() {
			if seen[f.Name] {
				return nil, &Error{Line: int(at.Line), Col: int(at.Col), Msg: "повторное поле " + f.Name + " в паттерне записи"}
			}
			seen[f.Name] = true
			if p.RecordType() != "" && !slices.Contains(declared, f.Name) {
				return nil, &Error{Line: int(at.Line), Col: int(at.Col), Msg: "неизвестное поле " + f.Name + " в типе " + p.RecordType()}
			}
			sub, err := fc.compilePattern(f.Pat)
			if err != nil {
				return nil, err
			}
			fields = append(fields, vm.RecordPatField{Name: f.Name, Value: sub})
		}
		return &vm.CompiledPattern{Kind: vm.PatRecord, Tag: tag, Fields: fields}, nil

	case ast.PatternAs:
		inner, err := fc.compilePattern(p.AsInner())
		if err != nil {
			return nil, err
		}
		slot := fc.patSlot(p.AsName())
		fc.bindLocal(p.AsName(), slot)
		return &vm.CompiledPattern{
			Kind: vm.PatAs, Inner: inner, AsSlot: slot,
		}, nil

	case ast.PatternStrConcat:
		lit, err := parseLiteralValue(p.ConcatPrefix())
		if err != nil {
			return nil, err
		}
		if lit.Kind != runtime.KindStr {
			return nil, fmt.Errorf("срез: '<>' паттерн требует строковый литерал слева")
		}
		inner, err := fc.compilePattern(p.ConcatRest())
		if err != nil {
			return nil, err
		}
		return &vm.CompiledPattern{Kind: vm.PatStrConcat, StrPrefix: lit.Str, Inner: inner}, nil

	case ast.PatternWildcard:
		if pat.String() == "_" {
			return &vm.CompiledPattern{Kind: vm.PatWildcard}, nil
		}
		return nil, fmt.Errorf("срез: неподдерживаемый паттерн %T %q", pat, pat.String())
	}
	return nil, fmt.Errorf("срез: неподдерживаемый паттерн %T", pat)
}

func (fc *funcCompiler) compileConstExpr(e ast.Expr) (runtime.Value, error) {
	switch x := e.(type) {
	case ast.LiteralExpr:
		return parseLiteralValue(x.ValueStr())
	case ast.AtomExpr:
		return runtime.Atom(x.AtomName()), nil
	case ast.DecimalExpr:
		r, err := runtime.ParseDecimal(x.ValueStr())
		if err != nil {
			return runtime.Unit, err
		}
		return runtime.Decimal(r), nil
	case ast.GroupingExpr:
		return fc.compileConstExpr(x.Inner())
	}
	return runtime.Unit, fmt.Errorf("map-паттерн: ключ должен быть литералом, got %T", e)
}

// ---- literals ----
