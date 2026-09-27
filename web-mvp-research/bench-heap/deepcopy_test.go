//go:build heapbench

package benchheap

import brt "github.com/it1ro/brig-lang/internal/runtime"

// deepCopy — модель `--copy-on-send`: копия терма без разделения с
// отправителем, как при heap на актора. Строки, Bytes и числа большой
// точности не копируются: в Go они неизменяемы и при heap на актора жили
// бы в общей области (refc-бинарники BEAM, 11 — «Большие Bytes»).
func deepCopy(v brt.Value) brt.Value {
	switch v.Kind {
	case brt.KindTuple:
		v.Tuple = copySlice(v.Tuple)
	case brt.KindList:
		v.List = copySlice(v.List)
	case brt.KindVector:
		v.Vector = copySlice(v.Vector)
	case brt.KindSet:
		v.Set = copySlice(v.Set)
	case brt.KindMap:
		es := make([]brt.MapEntry, len(v.Map))
		for i, e := range v.Map {
			es[i] = brt.MapEntry{Key: deepCopy(e.Key), Val: deepCopy(e.Val)}
		}
		v.Map = es
	case brt.KindVariant:
		vv := *v.Variant
		vv.Args = copySlice(vv.Args)
		v.Variant = &vv
	case brt.KindRecord:
		r := *v.Record
		r.Fields = make([]brt.RecordField, len(v.Record.Fields))
		for i, f := range v.Record.Fields {
			f.Val = deepCopy(f.Val)
			r.Fields[i] = f
		}
		v.Record = &r
	case brt.KindClosure:
		c := *v.ClosureVal
		c.Captures = copySlice(c.Captures)
		v.ClosureVal = &c
	}
	return v
}

func copySlice(vs []brt.Value) []brt.Value {
	if vs == nil {
		return nil
	}
	out := make([]brt.Value, len(vs))
	for i, x := range vs {
		out[i] = deepCopy(x)
	}
	return out
}

// cterm — модель компактного представления значения (16 Б: интерфейс
// Go), чтобы отделить цену семантики копирования от цены нынешнего
// 296-байтного runtime.Value. Кортеж и список — []cterm, атом — catom.
type (
	cterm  any
	catom  string
	ctuple []cterm
	clist  []cterm
)

func cdeepCopy(v cterm) cterm {
	switch x := v.(type) {
	case ctuple:
		out := make(ctuple, len(x))
		for i, e := range x {
			out[i] = cdeepCopy(e)
		}
		return out
	case clist:
		out := make(clist, len(x))
		for i, e := range x {
			out[i] = cdeepCopy(e)
		}
		return out
	}
	return v // int64, string, catom: неизменяемы, копия не нужна
}

// compactOf переводит runtime.Value в cterm (только виды, нужные стенду).
func compactOf(v brt.Value) cterm {
	switch v.Kind {
	case brt.KindTuple:
		out := make(ctuple, len(v.Tuple))
		for i, e := range v.Tuple {
			out[i] = compactOf(e)
		}
		return out
	case brt.KindList:
		out := make(clist, len(v.List))
		for i, e := range v.List {
			out[i] = compactOf(e)
		}
		return out
	case brt.KindAtom:
		return catom(v.Atom)
	case brt.KindStr:
		return v.Str
	case brt.KindPid:
		return int64(v.Pid)
	}
	return v.SmallInt
}
