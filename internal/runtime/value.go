// Package runtime — модель значений Brig (§3, §4, таблица равенства §4.8).
// Этап 4.1: примитивы + коллекции + варианты. Иммутабельность (#13).
package runtime

import (
	"fmt"
	"math/big"
	"strings"
)

// Kind — тег типа значения.
type Kind int

const (
	KindUnit Kind = iota
	KindBool
	KindInt
	KindFloat
	KindStr
	KindAtom
	KindTuple
	KindList
	KindVector
	KindMap
	KindFunction
	KindVariant
	KindPid
	KindRef
)

func (k Kind) String() string {
	switch k {
	case KindUnit:
		return "()"
	case KindBool:
		return "Bool"
	case KindInt:
		return "Int"
	case KindFloat:
		return "Float"
	case KindStr:
		return "Str"
	case KindAtom:
		return "Atom"
	case KindTuple:
		return "Tuple"
	case KindList:
		return "List"
	case KindVector:
		return "Vector"
	case KindMap:
		return "Map"
	case KindFunction:
		return "Function"
	case KindVariant:
		return "Variant"
	case KindPid:
		return "Pid"
	case KindRef:
		return "Ref"
	}
	return "unknown"
}

// Caller — способность вызвать значение-функцию. Реализуется *vm.VM.
// Разрывает цикл импорта: прелюдия в этом пакете может вызывать функции,
// не зная о конкретной машине.
type Caller interface {
	Call(fn Value, args []Value) (Value, error)
}

// NativeFunc — встроенная функция прелюдии.
type NativeFunc func(c Caller, args []Value) (Value, error)

// FuncValue — значение-функция: либо нативная, либо скомпилированная.
//
// Для скомпилированных функций компилятор кладёт в Body *vm.Chunk
// (через any, чтобы runtime не импортировал vm и не было цикла).
// Арность: >=0 фиксированная; -1 — вариадическая (..args).
type FuncValue struct {
	Name     string
	Arity    int
	IsNative bool
	Native   NativeFunc
	Body     any // *vm.Chunk для байткод-функций
}

// VariantValue — конструктор варианта: Ok/Error/Some/None и пользовательские.
type VariantValue struct {
	Tag  string
	Args []Value
}

// MapEntry — пара ключ/значение в иммутабельной мапе.
type MapEntry struct{ Key, Val Value }

// Value — любое значение Брига.
//
// Храним всё в одной структуре для простоты MVP. Оптимизация
// (small-int fast path, union-теги) — позже (§15, наблюдаемая
// семантика не меняется).
type Value struct {
	Kind    Kind
	Bool    bool
	Int     *big.Int
	Float   float64
	Str     string
	Atom    string
	Tuple   []Value
	List    []Value
	Vector  []Value
	Map     []MapEntry
	Func    *FuncValue
	Variant *VariantValue
	Pid     int
	Ref     int
}

// ---- конструкторы ----

var Unit = Value{Kind: KindUnit}

func Bool(b bool) Value        { return Value{Kind: KindBool, Bool: b} }
func Int(v int64) Value        { return Value{Kind: KindInt, Int: big.NewInt(v)} }
func IntBig(v *big.Int) Value  { return Value{Kind: KindInt, Int: v} }
func Float(f float64) Value    { return Value{Kind: KindFloat, Float: f} }
func Str(s string) Value       { return Value{Kind: KindStr, Str: s} }
func Atom(a string) Value      { return Value{Kind: KindAtom, Atom: a} }
func Tuple(vs ...Value) Value  { return Value{Kind: KindTuple, Tuple: vs} }
func List(vs ...Value) Value   { return Value{Kind: KindList, List: vs} }
func Vector(vs ...Value) Value { return Value{Kind: KindVector, Vector: vs} }
func Func(f *FuncValue) Value  { return Value{Kind: KindFunction, Func: f} }
func Variant(tag string, args ...Value) Value {
	return Value{Kind: KindVariant, Variant: &VariantValue{Tag: tag, Args: args}}
}

func Map(entries []MapEntry) Value { return Value{Kind: KindMap, Map: entries} }

// ---- печать ----

// Inspect — каноническое текстовое представление значения.
func (v Value) Inspect() string {
	switch v.Kind {
	case KindUnit:
		return "()"
	case KindBool:
		if v.Bool {
			return "true"
		}
		return "false"
	case KindInt:
		return v.Int.String()
	case KindFloat:
		return fmt.Sprintf("%v", v.Float)
	case KindStr:
		return v.Str // print печатает без кавычек
	case KindAtom:
		return ":" + v.Atom
	case KindTuple:
		parts := make([]string, len(v.Tuple))
		for i, e := range v.Tuple {
			parts[i] = inspectLit(e)
		}
		return "(" + strings.Join(parts, ", ") + ")"
	case KindList:
		return "[" + inspectJoin(v.List) + "]"
	case KindVector:
		return "%[" + inspectJoin(v.Vector) + "]"
	case KindMap:
		parts := make([]string, len(v.Map))
		for i, e := range v.Map {
			parts[i] = inspectLit(e.Key) + " => " + inspectLit(e.Val)
		}
		return "%{" + strings.Join(parts, ", ") + "}"
	case KindFunction:
		return fmt.Sprintf("#<function %s/%d>", v.Func.Name, v.Func.Arity)
	case KindVariant:
		if len(v.Variant.Args) == 0 {
			return v.Variant.Tag
		}
		return v.Variant.Tag + "(" + inspectJoin(v.Variant.Args) + ")"
	case KindPid:
		return fmt.Sprintf("#<pid %d>", v.Pid)
	case KindRef:
		return fmt.Sprintf("#<ref %d>", v.Ref)
	}
	return fmt.Sprintf("<%v>", v.Kind)
}

func inspectLit(v Value) string {
	if v.Kind == KindStr {
		return fmt.Sprintf("%q", v.Str)
	}
	return v.Inspect()
}

func inspectJoin(vs []Value) string {
	parts := make([]string, len(vs))
	for i, e := range vs {
		parts[i] = inspectLit(e)
	}
	return strings.Join(parts, ", ")
}

// ---- равенство (§4.8) ----

// Equal — структурное равенство.
//
// Упрощение среза: смешанное сравнение чисел и полный структурный
// проход по коллекциям; Decimal пока нет.
func Equal(a, b Value) bool {
	if a.Kind != b.Kind {
		// числа: Int == Float по значению (§4.8)
		if isNum(a) && isNum(b) {
			return numToFloat(a) == numToFloat(b)
		}
		return false
	}
	switch a.Kind {
	case KindUnit:
		return true
	case KindBool:
		return a.Bool == b.Bool
	case KindInt:
		return a.Int.Cmp(b.Int) == 0
	case KindFloat:
		return a.Float == b.Float
	case KindStr:
		return a.Str == b.Str
	case KindAtom:
		return a.Atom == b.Atom
	case KindTuple:
		if len(a.Tuple) != len(b.Tuple) {
			return false
		}
		for i := range a.Tuple {
			if !Equal(a.Tuple[i], b.Tuple[i]) {
				return false
			}
		}
		return true
	case KindList:
		return equalSlice(a.List, b.List)
	case KindVector:
		return equalSlice(a.Vector, b.Vector)
	case KindMap:
		if len(a.Map) != len(b.Map) {
			return false
		}
		for _, ae := range a.Map {
			found := false
			for _, be := range b.Map {
				if Equal(ae.Key, be.Key) && Equal(ae.Val, be.Val) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	case KindVariant:
		if a.Variant.Tag != b.Variant.Tag ||
			len(a.Variant.Args) != len(b.Variant.Args) {
			return false
		}
		return equalSlice(a.Variant.Args, b.Variant.Args)
	case KindFunction:
		// identity (§4.8): сравниваем указатели на FuncValue.
		return a.Func == b.Func
	case KindPid:
		return a.Pid == b.Pid
	case KindRef:
		return a.Ref == b.Ref
	}
	return false
}

func equalSlice(a, b []Value) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !Equal(a[i], b[i]) {
			return false
		}
	}
	return true
}

func isNum(v Value) bool { return v.Kind == KindInt || v.Kind == KindFloat }

func numToFloat(v Value) float64 {
	if v.Kind == KindFloat {
		return v.Float
	}
	f, _ := new(big.Float).SetInt(v.Int).Float64()
	return f
}

// ---- сравнение для < > <= >= (§7.4 терм-порядок, упрощённо) ----

// Compare возвращает -1/0/+1. Смешанные числа сравниваются по значению.
// Не-числа в этом срезе сравниваются только внутри своего вида;
// полная терм-сортировка — подэтап 4.4+.
func Compare(a, b Value) (int, error) {
	if isNum(a) && isNum(b) {
		af, bf := numToFloat(a), numToFloat(b)
		switch {
		case af < bf:
			return -1, nil
		case af > bf:
			return 1, nil
		default:
			return 0, nil
		}
	}
	switch a.Kind {
	case KindStr:
		if b.Kind != KindStr {
			return 0, cmpErr(a, b)
		}
		return strings.Compare(a.Str, b.Str), nil
	case KindAtom:
		if b.Kind != KindAtom {
			return 0, cmpErr(a, b)
		}
		return strings.Compare(a.Atom, b.Atom), nil
	case KindBool:
		if b.Kind != KindBool {
			return 0, cmpErr(a, b)
		}
		ai, bi := 0, 0
		if a.Bool {
			ai = 1
		}
		if b.Bool {
			bi = 1
		}
		return ai - bi, nil
	}
	return 0, cmpErr(a, b)
}

func cmpErr(a, b Value) error {
	return fmt.Errorf("(:type_error, (:compare, (%s, %s)))", a.Kind, b.Kind)
}
