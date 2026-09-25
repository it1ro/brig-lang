// Package runtime — модель значений Brig (§3, §4, таблица равенства §4.8).
package runtime

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

// Kind — тег типа значения.
type Kind int

const (
	// KindUnit — тег типа unit ()
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
	KindClosure
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
	case KindClosure:
		return "Closure"
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
type Caller interface {
	Call(fn Value, args []Value) (Value, error)
}

// NativeFunc — встроенная функция прелюдии.
type NativeFunc func(c Caller, args []Value) (Value, error)

// FuncValue — значение-функция: либо нативная, либо скомпилированная.
type FuncValue struct {
	Name     string
	Arity    int
	IsNative bool
	Native   NativeFunc
	Body     Code
}

// ClosureValue — замыкание: функция + захваченные переменные.
type ClosureValue struct {
	Name     string
	Arity    int
	Func     Code
	Captures []Value
}

// VariantValue — конструктор варианта.
type VariantValue struct {
	Tag  string
	Args []Value
}

// MapEntry — пара ключ/значение в иммутабельной мапе.
type MapEntry struct{ Key, Val Value }

// Value — любое значение Brig.
//
// Small-int fast-path: IsSmall==true означает, что целое представлено
// в SmallInt (int64), а Int == nil. IsSmall==false + Kind==KindInt
// означает, что Int != nil (big.Int). Все конструкторы соблюдают
// этот инвариант.
type Value struct {
	Kind       Kind
	Bool       bool
	SmallInt   int64
	IsSmall    bool
	intBig     *big.Int
	Float      float64
	Str        string
	Atom       string
	Tuple      []Value
	List       []Value
	Vector     []Value
	Map        []MapEntry
	Func       *FuncValue
	ClosureVal *ClosureValue
	Variant    *VariantValue
	Pid        int
	Ref        int
}

// ---- конструкторы ----

// Unit — единственное значение типа Unit.
var Unit = Value{Kind: KindUnit}

// Bool создаёт булево значение.
func Bool(b bool) Value { return Value{Kind: KindBool, Bool: b} }

// Int создаёт целое; влезает в int64 — использует small-int fast-path.
func Int(v int64) Value {
	return Value{Kind: KindInt, SmallInt: v, IsSmall: true}
}

// IntBig создаёт целое из big.Int; при возможности конвертирует в small.
func IntBig(b *big.Int) Value {
	if b.IsInt64() {
		return Int(b.Int64())
	}
	return Value{Kind: KindInt, intBig: b}
}

// AsBig возвращает big.Int представление (аллоцирует, если small).
func (v Value) AsBig() *big.Int {
	if v.Kind != KindInt {
		return nil
	}
	if v.IsSmall {
		return big.NewInt(v.SmallInt)
	}
	return v.intBig
}

// Float создаёт число с плавающей точкой.
func Float(f float64) Value { return Value{Kind: KindFloat, Float: f} }

// Str создаёт строку.
func Str(s string) Value { return Value{Kind: KindStr, Str: s} }

// Atom создаёт атом.
func Atom(a string) Value { return Value{Kind: KindAtom, Atom: a} }

// Tuple создаёт кортеж.
func Tuple(vs ...Value) Value { return Value{Kind: KindTuple, Tuple: vs} }

// List создаёт список.
func List(vs ...Value) Value { return Value{Kind: KindList, List: vs} }

// Vector создаёт вектор.
func Vector(vs ...Value) Value { return Value{Kind: KindVector, Vector: vs} }

// Func создаёт значение-функцию.
func Func(f *FuncValue) Value { return Value{Kind: KindFunction, Func: f} }

// MakeClosure создаёт замыкание.
func MakeClosure(name string, arity int, fn Code, captures []Value) Value {
	return Value{Kind: KindClosure, ClosureVal: &ClosureValue{
		Name: name, Arity: arity, Func: fn, Captures: captures,
	}}
}

// Variant создаёт значение-вариант.
func Variant(tag string, args ...Value) Value {
	return Value{Kind: KindVariant, Variant: &VariantValue{Tag: tag, Args: args}}
}

// Map создаёт мапу.
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
		if v.IsSmall {
			return strconv.FormatInt(v.SmallInt, 10)
		}
		return v.intBig.String()
	case KindFloat:
		return fmt.Sprintf("%v", v.Float)
	case KindStr:
		return v.Str
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
	case KindClosure:
		return fmt.Sprintf("#<closure %s/%d>", v.ClosureVal.Name, v.ClosureVal.Arity)
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
func Equal(a, b Value) bool {
	if a.Kind != b.Kind {
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
		if a.IsSmall && b.IsSmall {
			return a.SmallInt == b.SmallInt
		}
		return a.AsBig().Cmp(b.AsBig()) == 0
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
		return a.Func == b.Func
	case KindClosure:
		return a.ClosureVal == b.ClosureVal
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
	if v.IsSmall {
		return float64(v.SmallInt)
	}
	f, _ := new(big.Float).SetInt(v.intBig).Float64()
	return f
}

// ---- сравнение (§7.4, упрощённо) ----

// Compare возвращает -1/0/+1.
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

// Code — маркер скомпилированного байткода.
type Code interface {
	IsBrigCode()
}

// ---- Сериализация (§14.8, §13.1) ----

// Serialize проверяет сериализуемость значения для передачи между нодами.
func Serialize(v Value) error {
	return serializeValue(v, 0)
}

const maxSerializeDepth = 64

func serializeValue(v Value, depth int) error {
	if depth > maxSerializeDepth {
		return fmt.Errorf("(:serialize_error, :depth_limit)")
	}
	switch v.Kind {
	case KindFunction:
		return fmt.Errorf(
			"(:serialize_error, (:function, %q)) — "+
				"функции не сериализуемы (§14.8); "+
				"используйте MFA-дескриптор { module: M, function: F, args: A } (§13.1)",
			v.Func.Name)
	case KindClosure:
		return fmt.Errorf(
			"(:serialize_error, (:closure, %q)) — "+
				"замыкания не сериализуемы (§14.8); "+
				"используйте MFA-дескриптор (§13.1)",
			v.ClosureVal.Name)
	case KindPid:
		return fmt.Errorf("(:serialize_error, :pid) — Pid не сериализуем (§14.8)")
	case KindRef:
		return fmt.Errorf("(:serialize_error, :ref) — Ref не сериализуем (§14.8)")
	case KindTuple:
		for _, e := range v.Tuple {
			if err := serializeValue(e, depth+1); err != nil {
				return err
			}
		}
	case KindList:
		for _, e := range v.List {
			if err := serializeValue(e, depth+1); err != nil {
				return err
			}
		}
	case KindVector:
		for _, e := range v.Vector {
			if err := serializeValue(e, depth+1); err != nil {
				return err
			}
		}
	case KindMap:
		for _, e := range v.Map {
			if err := serializeValue(e.Key, depth+1); err != nil {
				return err
			}
			if err := serializeValue(e.Val, depth+1); err != nil {
				return err
			}
		}
	case KindVariant:
		for _, e := range v.Variant.Args {
			if err := serializeValue(e, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

// MFA — сериализуемый дескриптор функции для передачи между акторами (§13.1).
func MFA(module, function string, args []Value) Value {
	return Map([]MapEntry{
		{Key: Atom("module"), Val: Str(module)},
		{Key: Atom("function"), Val: Str(function)},
		{Key: Atom("args"), Val: List(args...)},
	})
}
