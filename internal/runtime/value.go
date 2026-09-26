// Package runtime — модель значений Brig (§3, §4, таблица равенства §4.8).
package runtime

import (
	"bytes"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
)

// Kind — тег типа значения.
type Kind int

// Значения Kind — теги вариантов Value (§3, §4). KindUnit — тег типа unit ().
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
	KindClosure
	KindVariant
	KindPid
	KindRef
	KindRange   // Sprint 5.1 (§4.3)
	KindSet     // Sprint 5.2 (§4.6)
	KindBytes   // Sprint 5.4 (§3.2)
	KindDecimal // Sprint 5.4 (§3.1)
	KindRecord  // T-73 (§4.7): номинальная и анонимная запись
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
	case KindRange:
		return "Range"
	case KindSet:
		return "Set"
	case KindBytes:
		return "Bytes"
	case KindDecimal:
		return "Decimal"
	case KindRecord:
		return "Record"
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

// RecordValue — запись (§4.7). Type == "" — анонимная, иначе номинальная
// записи `type Type {...}`. Поля номинальной — в порядке декларации,
// анонимной — в порядке первого появления; имена уникальны.
type RecordValue struct {
	Type   string
	Fields []RecordField
}

// RecordField — поле записи.
type RecordField struct {
	Name string
	Val  Value
}

// Get возвращает значение поля name.
func (r *RecordValue) Get(name string) (Value, bool) {
	for _, f := range r.Fields {
		if f.Name == name {
			return f.Val, true
		}
	}
	return Unit, false
}

// MapEntry — пара ключ/значение в иммутабельной мапе.
type MapEntry struct{ Key, Val Value }

// Value — любое значение Brig.
//
// Small-int fast-path: IsSmall==true означает, что целое представлено
// в SmallInt (int64), а intBig == nil. IsSmall==false + Kind==KindInt
// означает, что intBig != nil. Все конструкторы соблюдают этот инвариант.
//
// Range (Sprint 5.1): границы хранятся как int64; при построении
// диапазона с big-int границами VM возбуждает :range_error.
//
// Bytes (Sprint 5.4, §3.2): иммутабельная последовательность байт;
// escape-последовательности раскодируются компилятором.
//
// Decimal (Sprint 5.4, §3.1): *big.Rat — точная арифметика, равенство
// и сравнение по значению; dec"1.50" == dec"1.5".
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
	Set        []Value
	Func       *FuncValue
	ClosureVal *ClosureValue
	Variant    *VariantValue
	Record     *RecordValue
	Pid        int
	Ref        int
	RangeStart int64
	RangeEnd   int64
	Bytes      []byte
	Dec        *big.Rat
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

// Set создаёт множество (Sprint 5.2, §4.6).
func Set(vs ...Value) Value { return Value{Kind: KindSet, Set: vs} }

// Range создаёт диапазон (Sprint 5.1, §4.3).
func Range(start, end int64) Value {
	return Value{Kind: KindRange, RangeStart: start, RangeEnd: end}
}

// Bytes создаёт байтовую строку (Sprint 5.4, §3.2). Копирует входной
// слайс, чтобы гарантировать иммутабельность значения.
func Bytes(b []byte) Value {
	buf := make([]byte, len(b))
	copy(buf, b)
	return Value{Kind: KindBytes, Bytes: buf}
}

// Decimal создаёт десятичное значение из *big.Rat (Sprint 5.4, §3.1).
// Вызывающий не должен мутировать r после передачи.
func Decimal(r *big.Rat) Value { return Value{Kind: KindDecimal, Dec: r} }

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

// Record создаёт запись; typ == "" — анонимная (§4.7).
func Record(typ string, fields []RecordField) Value {
	return Value{Kind: KindRecord, Record: &RecordValue{Type: typ, Fields: fields}}
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
	case KindSet:
		return "set(" + inspectJoin(v.Set) + ")"
	case KindRange:
		return strconv.FormatInt(v.RangeStart, 10) + " to " +
			strconv.FormatInt(v.RangeEnd, 10)
	case KindBytes:
		return inspectBytes(v.Bytes)
	case KindDecimal:
		return `dec"` + FormatDecimal(v.Dec) + `"`
	case KindFunction:
		return fmt.Sprintf("#<function %s/%d>", v.Func.Name, v.Func.Arity)
	case KindClosure:
		return fmt.Sprintf("#<closure %s/%d>", v.ClosureVal.Name, v.ClosureVal.Arity)
	case KindVariant:
		if len(v.Variant.Args) == 0 {
			return v.Variant.Tag
		}
		return v.Variant.Tag + "(" + inspectJoin(v.Variant.Args) + ")"
	case KindRecord:
		return inspectRecord(v.Record)
	case KindPid:
		return fmt.Sprintf("#<pid %d>", v.Pid)
	case KindRef:
		return fmt.Sprintf("#<ref %d>", v.Ref)
	}
	return fmt.Sprintf("<%v>", v.Kind)
}

// inspectBytes печатает байтовое значение в канонической форме b"..."
// с escape-последовательностями (A4.2).
func inspectBytes(b []byte) string {
	var sb strings.Builder
	sb.WriteString(`b"`)
	for _, c := range b {
		switch c {
		case '\n':
			sb.WriteString(`\n`)
		case '\t':
			sb.WriteString(`\t`)
		case '\r':
			sb.WriteString(`\r`)
		case 0:
			sb.WriteString(`\0`)
		case '\\':
			sb.WriteString(`\\`)
		case '"':
			sb.WriteString(`\"`)
		default:
			if c >= 0x20 && c < 0x7F {
				sb.WriteByte(c)
			} else {
				fmt.Fprintf(&sb, `\x%02x`, c)
			}
		}
	}
	sb.WriteByte('"')
	return sb.String()
}

// inspectRecord печатает запись в форме литерала: `User{ id: 1 }`,
// `{ id: 1 }`, `User{}`, `{}`.
func inspectRecord(r *RecordValue) string {
	if len(r.Fields) == 0 {
		return r.Type + "{}"
	}
	parts := make([]string, len(r.Fields))
	for i, f := range r.Fields {
		parts[i] = f.Name + ": " + inspectLit(f.Val)
	}
	return r.Type + "{ " + strings.Join(parts, ", ") + " }"
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

// MatchEqual — сравнение литерала-паттерна со значением (§4.8, колонка
// «Pattern matching»: точное; решение #43, вариант A). Значения разных
// Kind не равны: паттерн `1` не матчит `1.0` и `dec"1"`, `1.0` не
// матчит `1`. Внутри контейнеров правило то же (поэлементно).
func MatchEqual(a, b Value) bool {
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case KindTuple:
		return matchEqualSlice(a.Tuple, b.Tuple)
	case KindList:
		return matchEqualSlice(a.List, b.List)
	case KindVector:
		return matchEqualSlice(a.Vector, b.Vector)
	}
	return Equal(a, b)
}

func matchEqualSlice(a, b []Value) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !MatchEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}

// Equal — структурное равенство.
//
// Decimal-правила (§4.8, §7.4): Decimal×Decimal и Decimal×Int — по
// значению (1.50 == 1.5, dec"2" == 2); Decimal×Float — false (мягкая
// форма; жёсткая ошибка — на уровне оператора ==, см. vm.checkMixedEq).
func Equal(a, b Value) bool {
	if a.Kind != b.Kind {
		if a.Kind == KindDecimal {
			return equalDecimalOther(a, b)
		}
		if b.Kind == KindDecimal {
			return equalDecimalOther(b, a)
		}
		if isNum(a) && isNum(b) {
			// Int×Float — точно (I-F8, #43): 2^53+1 != 2^53.0.
			if math.IsNaN(floatOf(a, b)) {
				return false
			}
			return cmpIntFloatExact(a, b) == 0
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
	case KindDecimal:
		return a.Dec.Cmp(b.Dec) == 0
	case KindStr:
		return a.Str == b.Str
	case KindAtom:
		return a.Atom == b.Atom
	case KindRange:
		return a.RangeStart == b.RangeStart && a.RangeEnd == b.RangeEnd
	case KindBytes:
		return bytes.Equal(a.Bytes, b.Bytes)
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
	case KindSet:
		if len(a.Set) != len(b.Set) {
			return false
		}
		for _, ae := range a.Set {
			found := false
			for _, be := range b.Set {
				if Equal(ae, be) {
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
	case KindRecord:
		return equalRecords(a.Record, b.Record)
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

// equalRecords (§4.8): номинальная — по тегу и полям, анонимная — по
// полям; порядок полей не важен, номинальная ≠ анонимной.
func equalRecords(a, b *RecordValue) bool {
	if a.Type != b.Type || len(a.Fields) != len(b.Fields) {
		return false
	}
	for _, f := range a.Fields {
		bv, ok := b.Get(f.Name)
		if !ok || !Equal(f.Val, bv) {
			return false
		}
	}
	return true
}

func equalDecimalOther(dec, other Value) bool {
	switch other.Kind {
	case KindDecimal:
		return dec.Dec.Cmp(other.Dec) == 0
	case KindInt:
		return dec.Dec.Cmp(new(big.Rat).SetInt(other.AsBig())) == 0
	}
	// Decimal × Float и прочее — false на уровне Equal.
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

// valueToRat возвращает big.Rat для Int/Decimal; для остальных — false.
func valueToRat(v Value) (*big.Rat, bool) {
	switch v.Kind {
	case KindDecimal:
		return v.Dec, true
	case KindInt:
		return new(big.Rat).SetInt(v.AsBig()), true
	}
	return nil, false
}

// ---- сравнение (§7.4) ----

// Compare возвращает -1/0/+1.
//
// Порядок между видами — term order §7.4 (число < Bool < Range < атом <
// Bytes < Str < кортеж < Vector < List < Map < Set < номинальная запись <
// встроенные варианты < анонимная запись < Pid < Ref); внутри вида — по
// правилам вида. Function вне списка и даёт :type_error.
//
// Decimal-правила: Decimal×Decimal и Decimal×Int — точно, по значению;
// Decimal×Float — ошибка (жёстко, §7.4).
func Compare(a, b Value) (int, error) {
	ra, ok1 := termRank(a)
	rb, ok2 := termRank(b)
	if !ok1 || !ok2 {
		return 0, cmpErr(a, b)
	}
	if ra != rb {
		return cmpInt(ra, rb), nil
	}
	switch ra {
	case rankNumber:
		return compareNumbers(a, b)
	case rankBool:
		ai, bi := 0, 0
		if a.Bool {
			ai = 1
		}
		if b.Bool {
			bi = 1
		}
		return ai - bi, nil
	case rankRange:
		if a.RangeStart != b.RangeStart {
			return cmpInt64(a.RangeStart, b.RangeStart), nil
		}
		return cmpInt64(a.RangeEnd, b.RangeEnd), nil
	case rankAtom:
		return strings.Compare(a.Atom, b.Atom), nil
	case rankBytes:
		return bytes.Compare(a.Bytes, b.Bytes), nil
	case rankStr:
		return strings.Compare(a.Str, b.Str), nil
	case rankTuple:
		return compareSlices(a.Tuple, b.Tuple)
	case rankVector:
		return compareSlices(a.Vector, b.Vector)
	case rankList:
		return compareSlices(a.List, b.List)
	case rankMap:
		return compareMaps(a.Map, b.Map)
	case rankSet:
		return compareSlices(sortedValues(a.Set), sortedValues(b.Set))
	case rankNominal, rankAnon:
		return compareRecords(a.Record, b.Record)
	case rankVariant:
		return compareVariants(a.Variant, b.Variant)
	case rankPid:
		return cmpInt(a.Pid, b.Pid), nil
	case rankRef:
		return cmpInt(a.Ref, b.Ref), nil
	}
	return 0, cmpErr(a, b)
}

// Ранги видов в term order §7.4.
const (
	rankNumber = iota
	rankBool
	rankRange
	rankAtom
	rankBytes
	rankStr
	rankTuple
	rankVector
	rankList
	rankMap
	rankSet
	rankNominal
	rankVariant
	rankAnon
	rankPid
	rankRef
)

func termRank(v Value) (int, bool) {
	switch v.Kind {
	case KindInt, KindFloat, KindDecimal:
		return rankNumber, true
	case KindBool:
		return rankBool, true
	case KindRange:
		return rankRange, true
	case KindAtom:
		return rankAtom, true
	case KindBytes:
		return rankBytes, true
	case KindStr:
		return rankStr, true
	case KindUnit, KindTuple:
		return rankTuple, true
	case KindVector:
		return rankVector, true
	case KindList:
		return rankList, true
	case KindMap:
		return rankMap, true
	case KindSet:
		return rankSet, true
	case KindRecord:
		if v.Record.Type == "" {
			return rankAnon, true
		}
		return rankNominal, true
	case KindVariant:
		return rankVariant, true
	case KindPid:
		return rankPid, true
	case KindRef:
		return rankRef, true
	}
	return 0, false
}

func compareNumbers(a, b Value) (int, error) {
	if a.Kind == KindDecimal || b.Kind == KindDecimal {
		ar, ok1 := valueToRat(a)
		br, ok2 := valueToRat(b)
		if !ok1 || !ok2 {
			return 0, cmpErr(a, b)
		}
		return ar.Cmp(br), nil
	}
	if a.Kind != b.Kind {
		// Int×Float — точное сравнение; NaN — см. cmpNaN.
		if math.IsNaN(floatOf(a, b)) {
			return cmpNaN(a, b), nil
		}
		return cmpIntFloatExact(a, b), nil
	}
	if a.Kind == KindInt {
		// Int×Int — точно (float64 теряет младшие биты выше 2^53).
		if a.IsSmall && b.IsSmall {
			return cmpInt64(a.SmallInt, b.SmallInt), nil
		}
		return a.AsBig().Cmp(b.AsBig()), nil
	}
	af, bf := a.Float, b.Float
	if math.IsNaN(af) || math.IsNaN(bf) {
		return cmpNaN(a, b), nil
	}
	switch {
	case af < bf:
		return -1, nil
	case af > bf:
		return 1, nil
	default:
		return 0, nil
	}
}

// floatOf возвращает Float-операнд пары Int×Float (NaN проверяется на нём).
func floatOf(a, b Value) float64 {
	if a.Kind == KindFloat {
		return a.Float
	}
	return b.Float
}

// IsNaNOperand сообщает, что хотя бы один из операндов — Float NaN.
// Порядковые операторы (<, >, <=, >=) на таких парах дают false (§7.4);
// Compare для них даёт детерминированный порядок (см. cmpNaN).
func IsNaNOperand(a, b Value) bool {
	return (a.Kind == KindFloat && math.IsNaN(a.Float)) ||
		(b.Kind == KindFloat && math.IsNaN(b.Float))
}

// cmpNaN — порядок Compare с участием NaN: NaN после всех чисел и равен
// себе. Это нужен только sort/Set/Map, где требуется полный порядок;
// оператор == по-прежнему считает NaN != NaN (Equal), а <, > — false.
func cmpNaN(a, b Value) int {
	an := a.Kind == KindFloat && math.IsNaN(a.Float)
	bn := b.Kind == KindFloat && math.IsNaN(b.Float)
	switch {
	case an && bn:
		return 0
	case an:
		return 1
	}
	return -1
}

// cmpIntFloatExact точно сравнивает Int с Float (в любом порядке
// аргументов), NaN уже исключён. Быстрый путь: |Int| <= 2^53 переводится
// во float64 без потерь. Иначе Inf решается по знаку, а конечные значения
// сравниваются через big.Float (SetInt и SetFloat64 точны).
func cmpIntFloatExact(a, b Value) int {
	if a.Kind == KindFloat {
		return -cmpIntFloatExact(b, a)
	}
	// a — Int, b — Float
	f := b.Float
	if a.IsSmall && a.SmallInt >= -(1<<53) && a.SmallInt <= 1<<53 {
		x := float64(a.SmallInt)
		switch {
		case x < f:
			return -1
		case x > f:
			return 1
		}
		return 0
	}
	if math.IsInf(f, 1) {
		return -1
	}
	if math.IsInf(f, -1) {
		return 1
	}
	return new(big.Float).SetInt(a.AsBig()).Cmp(new(big.Float).SetFloat64(f))
}

// compareSlices: длина, затем поэлементно.
func compareSlices(a, b []Value) (int, error) {
	if len(a) != len(b) {
		return cmpInt(len(a), len(b)), nil
	}
	for i := range a {
		c, err := Compare(a[i], b[i])
		if err != nil || c != 0 {
			return c, err
		}
	}
	return 0, nil
}

// sortedValues возвращает отсортированную копию; при несравнимых
// элементах порядок остаётся исходным (ошибку отдаст compareSlices).
func sortedValues(vs []Value) []Value {
	out := append([]Value(nil), vs...)
	sort.SliceStable(out, func(i, j int) bool {
		c, err := Compare(out[i], out[j])
		return err == nil && c < 0
	})
	return out
}

// compareMaps: размер, затем по парам, отсортированным по ключу.
func compareMaps(a, b []MapEntry) (int, error) {
	if len(a) != len(b) {
		return cmpInt(len(a), len(b)), nil
	}
	sa, sb := sortedEntries(a), sortedEntries(b)
	for i := range sa {
		if c, err := Compare(sa[i].Key, sb[i].Key); err != nil || c != 0 {
			return c, err
		}
		if c, err := Compare(sa[i].Val, sb[i].Val); err != nil || c != 0 {
			return c, err
		}
	}
	return 0, nil
}

func sortedEntries(es []MapEntry) []MapEntry {
	out := append([]MapEntry(nil), es...)
	sort.SliceStable(out, func(i, j int) bool {
		c, err := Compare(out[i].Key, out[j].Key)
		return err == nil && c < 0
	})
	return out
}

// variantTagOrder: None < Some < Ok < Error. Порядок между группами
// Option и Result спекой не задан — выбран по порядку перечисления.
var variantTagOrder = map[string]int{"None": 0, "Some": 1, "Ok": 2, "Error": 3}

// compareVariants: по тегу, затем по полям.
func compareVariants(a, b *VariantValue) (int, error) {
	ta, oka := variantTagOrder[a.Tag]
	tb, okb := variantTagOrder[b.Tag]
	switch {
	case oka && okb:
		if ta != tb {
			return cmpInt(ta, tb), nil
		}
	case oka != okb:
		return 0, cmpErr(Value{Kind: KindVariant, Variant: a}, Value{Kind: KindVariant, Variant: b})
	default:
		if c := strings.Compare(a.Tag, b.Tag); c != 0 {
			return c, nil
		}
	}
	return compareSlices(a.Args, b.Args)
}

// compareRecords: для номинальных — имя типа; затем число полей и пары
// (имя, значение) в порядке отсортированных имён — не зависит от порядка
// полей в литерале.
func compareRecords(a, b *RecordValue) (int, error) {
	if c := strings.Compare(a.Type, b.Type); c != 0 {
		return c, nil
	}
	if len(a.Fields) != len(b.Fields) {
		return cmpInt(len(a.Fields), len(b.Fields)), nil
	}
	fa, fb := sortedFields(a.Fields), sortedFields(b.Fields)
	for i := range fa {
		if c := strings.Compare(fa[i].Name, fb[i].Name); c != 0 {
			return c, nil
		}
		if c, err := Compare(fa[i].Val, fb[i].Val); err != nil || c != 0 {
			return c, err
		}
	}
	return 0, nil
}

func sortedFields(fs []RecordField) []RecordField {
	out := append([]RecordField(nil), fs...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func cmpInt64(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
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
	case KindSet:
		for _, e := range v.Set {
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
	case KindRecord:
		for _, f := range v.Record.Fields {
			if err := serializeValue(f.Val, depth+1); err != nil {
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
