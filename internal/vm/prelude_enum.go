package vm

import (
	"math"
	"math/big"
	"slices"
	"strings"

	"github.com/it1ro/brig-lang/internal/runtime"
)

// Модуль Enum (T-257, DD #410) — функции над перечислимыми коллекциями:
// List, Vector, Range, Set, Map. Элемент Map — пара (k, v), порядок
// обхода Set и Map — term order, как у печати (§7.4). Результат — List,
// кроме свёрток, предикатов и group_by (Map). Функции с колбэком —
// возобновляемые нативы (G3, T-58), результат строится за один проход.
//
// Не коллекция на месте субъекта — (:type_error, ((:enum, :f), v))
// (modTypeErr, T-236); предикат вернул не Bool — (:type_error,
// (:expected_bool, r)), как условие if (K-2).

// enumIter — позиция обхода перечислимой коллекции, которую можно хранить
// между шагами возобновляемого натива.
type enumIter struct {
	kind  runtime.Kind
	list  runtime.ListCursor
	vec   runtime.Value      // Vector
	keys  []runtime.Value    // Set: ключи в term order
	ents  []runtime.MapEntry // Map: пары в term order
	i, n  int                // Vector, Set, Map: следующий индекс и длина
	lo    int64              // Range: следующее значение
	hi    int64              // Range: последнее значение
	done  bool               // Range: обход закончен
	cur   runtime.Value      // текущий элемент
	begun bool               // next() уже вернул true
}

// newEnumIter — обход xs; не коллекция — ((:enum, :fn), xs), убывающий
// Range — (:range_error, (start, end)), как у list (§4.3).
func newEnumIter(fn string, xs runtime.Value) (enumIter, error) {
	it := enumIter{kind: xs.Kind}
	switch xs.Kind {
	case runtime.KindList:
		it.list = xs.Cursor()
	case runtime.KindVector:
		it.vec, it.n = xs, xs.Len()
	case runtime.KindSet:
		it.keys = xs.Elems()
		it.n = len(it.keys)
	case runtime.KindMap:
		it.ents = xs.Entries()
		it.n = len(it.ents)
	case runtime.KindRange:
		if xs.RangeStart > xs.RangeEnd {
			return it, &ErrRaise{Val: runtime.Tuple(
				runtime.Atom("range_error"),
				runtime.Tuple(runtime.Int(xs.RangeStart), runtime.Int(xs.RangeEnd)))}
		}
		it.lo, it.hi = xs.RangeStart, xs.RangeEnd
	default:
		return it, modTypeErr("enum", fn, xs)
	}
	return it, nil
}

// next переходит к следующему элементу; false — элементы кончились.
func (it *enumIter) next() bool {
	switch it.kind {
	case runtime.KindList:
		if !it.list.Next() {
			return false
		}
		it.cur = it.list.Value()
	case runtime.KindRange:
		if it.done {
			return false
		}
		it.cur = runtime.Int(it.lo)
		if it.lo == it.hi {
			it.done = true
		} else {
			it.lo++
		}
	default:
		if it.i >= it.n {
			return false
		}
		switch it.kind {
		case runtime.KindVector:
			it.cur = it.vec.At(it.i)
		case runtime.KindSet:
			it.cur = it.keys[it.i]
		case runtime.KindMap:
			e := it.ents[it.i]
			it.cur = runtime.Tuple(e.Key, e.Val)
		}
		it.i++
	}
	it.begun = true
	return true
}

// enumHint — ёмкость построителя результата: длина коллекции, у Range —
// не больше 1<<16 (длинный диапазон растит построитель сам).
func enumHint(xs runtime.Value) int {
	if xs.Kind == runtime.KindRange {
		n := xs.RangeEnd - xs.RangeStart + 1
		if n < 0 || n > 1<<16 {
			return 1 << 16
		}
		return int(n)
	}
	return xs.Len()
}

// enumSlice — элементы xs по порядку обхода.
func enumSlice(fn string, xs runtime.Value) ([]runtime.Value, error) {
	it, err := newEnumIter(fn, xs)
	if err != nil {
		return nil, err
	}
	out := make([]runtime.Value, 0, enumHint(xs))
	for it.next() {
		out = append(out, it.cur)
	}
	return out, nil
}

// enumCont — enumIter для возобновляемого натива: f вызывается на каждом
// элементе (с аргументами args(e), по умолчанию — e); visit получает
// элемент и результат колбэка и может завершить обход досрочно со
// значением res; иначе итог — final(). Срез аргументов — буфер,
// переиспользуемый между колбэками (T-103).
type enumCont struct {
	f     runtime.Value
	it    enumIter
	arg   [1]runtime.Value
	args  func(e runtime.Value) []runtime.Value
	visit func(e, r runtime.Value) (stop bool, res runtime.Value, err error)
	final func() (runtime.Value, error)
}

func (c *enumCont) resume(ret runtime.Value) (nativeStep, error) {
	if c.it.begun {
		stop, res, err := c.visit(c.it.cur, ret)
		if err != nil {
			return nativeStep{}, err
		}
		if stop {
			return nativeStep{done: true, res: res}, nil
		}
	}
	if !c.it.next() {
		res, err := c.final()
		if err != nil {
			return nativeStep{}, err
		}
		return nativeStep{done: true, res: res}, nil
	}
	var args []runtime.Value
	if c.args != nil {
		args = c.args(c.it.cur)
	} else {
		c.arg[0] = c.it.cur
		args = c.arg[:]
	}
	return nativeStep{fn: c.f, args: args}, nil
}

// doneCont — натив без колбэков: результат готов сразу.
type doneCont struct{ res runtime.Value }

func (c doneCont) resume(runtime.Value) (nativeStep, error) {
	return nativeStep{done: true, res: c.res}, nil
}

// predicate — результат предиката: Bool или ловимый
// (:type_error, (:expected_bool, r)).
func predicate(r runtime.Value) (bool, error) {
	if r.Kind != runtime.KindBool {
		return false, notBoolErr(r)
	}
	return r.Bool, nil
}

// enumCompare — порядок sort/min/max: term order (§7.4) с ошибками
// оператора <: Decimal×Float и несравнимые значения —
// (:type_error, (:compare, (a, b))).
func enumCompare(a, b runtime.Value) (int, error) {
	if err := checkMixedCmp(a, b); err != nil {
		return 0, err
	}
	c, err := runtime.Compare(a, b)
	if err != nil {
		return 0, typeErr("compare", runtime.Tuple(a, b))
	}
	return c, nil
}

// sortStable — устойчивая сортировка по ключам keys (элементы — vals,
// nil — сами ключи); первая ошибка сравнения прерывает результат.
func sortStable(keys, vals []runtime.Value) ([]runtime.Value, error) {
	idx := make([]int, len(keys))
	for i := range idx {
		idx[i] = i
	}
	var cmpErr error
	slices.SortStableFunc(idx, func(i, j int) int {
		if cmpErr != nil {
			return 0
		}
		c, err := enumCompare(keys[i], keys[j])
		if err != nil {
			cmpErr = err
		}
		return c
	})
	if cmpErr != nil {
		return nil, cmpErr
	}
	if vals == nil {
		vals = keys
	}
	out := make([]runtime.Value, len(idx))
	for k, i := range idx {
		out[k] = vals[i]
	}
	return out, nil
}

// enumCount — аргумент-количество take/drop: Int; big.Int — за пределами
// любой длины (отрицательный — 0).
func enumCount(fn string, n runtime.Value) (int64, error) {
	if n.Kind != runtime.KindInt {
		return 0, modTypeErr("enum", fn, n)
	}
	if n.IsSmall {
		return n.SmallInt, nil
	}
	if n.AsBig().Sign() < 0 {
		return 0, nil
	}
	return math.MaxInt64, nil
}

func installEnum(def func(string, int, runtime.NativeFunc), defResumable func(string, int, resumableFunc)) {
	// ---- с колбэком ----

	defResumable("Enum.map", 2, func(args []runtime.Value) (nativeCont, error) {
		it, err := newEnumIter("map", args[0])
		if err != nil {
			return nil, err
		}
		out := runtime.NewListBuilder(enumHint(args[0]))
		return &enumCont{
			f: args[1], it: it,
			visit: func(_, r runtime.Value) (bool, runtime.Value, error) {
				out.Add(r)
				return false, runtime.Unit, nil
			},
			final: func() (runtime.Value, error) { return out.List(), nil },
		}, nil
	})

	filter := func(fn string, keep bool) resumableFunc {
		return func(args []runtime.Value) (nativeCont, error) {
			it, err := newEnumIter(fn, args[0])
			if err != nil {
				return nil, err
			}
			out := runtime.NewListBuilder(enumHint(args[0]))
			return &enumCont{
				f: args[1], it: it,
				visit: func(e, r runtime.Value) (bool, runtime.Value, error) {
					ok, err := predicate(r)
					if err != nil {
						return false, runtime.Unit, err
					}
					if ok == keep {
						out.Add(e)
					}
					return false, runtime.Unit, nil
				},
				final: func() (runtime.Value, error) { return out.List(), nil },
			}, nil
		}
	}
	defResumable("Enum.filter", 2, filter("filter", true))
	defResumable("Enum.reject", 2, filter("reject", false))

	defResumable("Enum.fold", 3, func(args []runtime.Value) (nativeCont, error) {
		it, err := newEnumIter("fold", args[0])
		if err != nil {
			return nil, err
		}
		acc := args[1]
		buf := make([]runtime.Value, 2)
		return &enumCont{
			f: args[2], it: it,
			args: func(e runtime.Value) []runtime.Value {
				buf[0], buf[1] = acc, e
				return buf
			},
			visit: func(_, r runtime.Value) (bool, runtime.Value, error) {
				acc = r
				return false, runtime.Unit, nil
			},
			final: func() (runtime.Value, error) { return acc, nil },
		}, nil
	})

	defResumable("Enum.find", 2, func(args []runtime.Value) (nativeCont, error) {
		it, err := newEnumIter("find", args[0])
		if err != nil {
			return nil, err
		}
		return &enumCont{
			f: args[1], it: it,
			visit: func(e, r runtime.Value) (bool, runtime.Value, error) {
				ok, err := predicate(r)
				return ok, runtime.Variant("Some", e), err
			},
			final: func() (runtime.Value, error) { return runtime.Variant("None"), nil },
		}, nil
	})

	// all?/any?: обход до первого решающего ответа предиката.
	quantifier := func(fn string, stopOn bool) resumableFunc {
		return func(args []runtime.Value) (nativeCont, error) {
			it, err := newEnumIter(fn, args[0])
			if err != nil {
				return nil, err
			}
			return &enumCont{
				f: args[1], it: it,
				visit: func(_, r runtime.Value) (bool, runtime.Value, error) {
					ok, err := predicate(r)
					return ok == stopOn, runtime.Bool(stopOn), err
				},
				final: func() (runtime.Value, error) { return runtime.Bool(!stopOn), nil },
			}, nil
		}
	}
	defResumable("Enum.all?", 2, quantifier("all?", false))
	defResumable("Enum.any?", 2, quantifier("any?", true))

	defResumable("Enum.each", 2, func(args []runtime.Value) (nativeCont, error) {
		it, err := newEnumIter("each", args[0])
		if err != nil {
			return nil, err
		}
		return &enumCont{
			f: args[1], it: it,
			visit: func(_, _ runtime.Value) (bool, runtime.Value, error) {
				return false, runtime.Unit, nil
			},
			final: func() (runtime.Value, error) { return runtime.Unit, nil },
		}, nil
	})

	// count/1 — число элементов, count/2 — число элементов, для которых
	// предикат вернул true.
	defResumable("Enum.count", -1, func(args []runtime.Value) (nativeCont, error) {
		switch len(args) {
		case 1:
			xs := args[0]
			if _, err := newEnumIter("count", xs); err != nil {
				return nil, err
			}
			if xs.Kind == runtime.KindRange {
				n := new(big.Int).Sub(big.NewInt(xs.RangeEnd), big.NewInt(xs.RangeStart))
				return doneCont{runtime.IntBig(n.Add(n, big.NewInt(1)))}, nil
			}
			return doneCont{runtime.Int(int64(xs.Len()))}, nil
		case 2:
			it, err := newEnumIter("count", args[0])
			if err != nil {
				return nil, err
			}
			var n int64
			return &enumCont{
				f: args[1], it: it,
				visit: func(_, r runtime.Value) (bool, runtime.Value, error) {
					ok, err := predicate(r)
					if ok {
						n++
					}
					return false, runtime.Unit, err
				},
				final: func() (runtime.Value, error) { return runtime.Int(n), nil },
			}, nil
		}
		return nil, functionClause(args)
	})

	defResumable("Enum.sort_by", 2, func(args []runtime.Value) (nativeCont, error) {
		it, err := newEnumIter("sort_by", args[0])
		if err != nil {
			return nil, err
		}
		n := enumHint(args[0])
		vals, keys := make([]runtime.Value, 0, n), make([]runtime.Value, 0, n)
		return &enumCont{
			f: args[1], it: it,
			visit: func(e, r runtime.Value) (bool, runtime.Value, error) {
				vals, keys = append(vals, e), append(keys, r)
				return false, runtime.Unit, nil
			},
			final: func() (runtime.Value, error) {
				out, err := sortStable(keys, vals)
				if err != nil {
					return runtime.Unit, err
				}
				return runtime.List(out...), nil
			},
		}, nil
	})

	defResumable("Enum.group_by", 2, func(args []runtime.Value) (nativeCont, error) {
		it, err := newEnumIter("group_by", args[0])
		if err != nil {
			return nil, err
		}
		// index: ключ → номер группы; группы — в порядке первого ключа.
		index := runtime.Map(nil)
		var keys []runtime.Value
		var groups []runtime.ListBuilder
		return &enumCont{
			f: args[1], it: it,
			visit: func(e, k runtime.Value) (bool, runtime.Value, error) {
				if i, ok := index.MapGet(k); ok {
					groups[i.SmallInt].Add(e)
					return false, runtime.Unit, nil
				}
				index = index.MapPut(k, runtime.Int(int64(len(groups))))
				keys = append(keys, k)
				groups = append(groups, runtime.ListBuilder{})
				groups[len(groups)-1].Add(e)
				return false, runtime.Unit, nil
			},
			final: func() (runtime.Value, error) {
				out := runtime.Map(nil)
				for i, k := range keys {
					out = out.MapPut(k, groups[i].List())
				}
				return out, nil
			},
		}, nil
	})

	defResumable("Enum.flat_map", 2, func(args []runtime.Value) (nativeCont, error) {
		it, err := newEnumIter("flat_map", args[0])
		if err != nil {
			return nil, err
		}
		out := runtime.NewListBuilder(enumHint(args[0]))
		return &enumCont{
			f: args[1], it: it,
			visit: func(_, r runtime.Value) (bool, runtime.Value, error) {
				sub, err := newEnumIter("flat_map", r)
				if err != nil {
					return false, runtime.Unit, err
				}
				for sub.next() {
					out.Add(sub.cur)
				}
				return false, runtime.Unit, nil
			},
			final: func() (runtime.Value, error) { return out.List(), nil },
		}, nil
	})

	// ---- без колбэка ----

	def("Enum.member?", 2, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		it, err := newEnumIter("member?", args[0])
		if err != nil {
			return runtime.Unit, err
		}
		for it.next() {
			if runtime.Equal(it.cur, args[1]) {
				return runtime.Bool(true), nil
			}
		}
		return runtime.Bool(false), nil
	})

	def("Enum.reverse", 1, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		xs, err := enumSlice("reverse", args[0])
		if err != nil {
			return runtime.Unit, err
		}
		slices.Reverse(xs)
		return runtime.List(xs...), nil
	})

	def("Enum.take", 2, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		it, err := newEnumIter("take", args[0])
		if err != nil {
			return runtime.Unit, err
		}
		n, err := enumCount("take", args[1])
		if err != nil {
			return runtime.Unit, err
		}
		if args[0].Kind == runtime.KindList && n >= int64(args[0].Len()) {
			return args[0], nil
		}
		out := runtime.NewListBuilder(int(max(0, min(n, int64(enumHint(args[0]))))))
		for i := int64(0); i < n && it.next(); i++ {
			out.Add(it.cur)
		}
		return out.List(), nil
	})

	def("Enum.drop", 2, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		it, err := newEnumIter("drop", args[0])
		if err != nil {
			return runtime.Unit, err
		}
		n, err := enumCount("drop", args[1])
		if err != nil {
			return runtime.Unit, err
		}
		if xs := args[0]; xs.Kind == runtime.KindList {
			// Хвост списка разделяется без копии (§4.2).
			switch {
			case n <= 0:
				return xs, nil
			case n >= int64(xs.Len()):
				return runtime.List(), nil
			}
			return xs.Drop(int(n)), nil
		}
		out := runtime.NewListBuilder(enumHint(args[0]))
		for i := int64(0); it.next(); i++ {
			if i >= n {
				out.Add(it.cur)
			}
		}
		return out.List(), nil
	})

	def("Enum.sort", 1, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		xs, err := enumSlice("sort", args[0])
		if err != nil {
			return runtime.Unit, err
		}
		out, err := sortStable(xs, nil)
		if err != nil {
			return runtime.Unit, err
		}
		return runtime.List(out...), nil
	})

	def("Enum.zip", 2, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		a, err := newEnumIter("zip", args[0])
		if err != nil {
			return runtime.Unit, err
		}
		b, err := newEnumIter("zip", args[1])
		if err != nil {
			return runtime.Unit, err
		}
		out := runtime.NewListBuilder(min(enumHint(args[0]), enumHint(args[1])))
		for a.next() && b.next() {
			out.Add(runtime.Tuple(a.cur, b.cur))
		}
		return out.List(), nil
	})

	def("Enum.with_index", 1, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		it, err := newEnumIter("with_index", args[0])
		if err != nil {
			return runtime.Unit, err
		}
		out := runtime.NewListBuilder(enumHint(args[0]))
		for i := int64(0); it.next(); i++ {
			out.Add(runtime.Tuple(it.cur, runtime.Int(i)))
		}
		return out.List(), nil
	})

	def("Enum.sum", 1, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		it, err := newEnumIter("sum", args[0])
		if err != nil {
			return runtime.Unit, err
		}
		acc := runtime.Int(0)
		for it.next() {
			switch it.cur.Kind {
			case runtime.KindInt, runtime.KindFloat, runtime.KindDecimal:
			default:
				return runtime.Unit, modTypeErr("enum", "sum", it.cur)
			}
			if acc, err = add(acc, it.cur); err != nil {
				return runtime.Unit, err
			}
		}
		return acc, nil
	})

	// min/max — первый наименьший (наибольший) элемент; пустая — None.
	extreme := func(fn string, sign int) runtime.NativeFunc {
		return func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
			it, err := newEnumIter(fn, args[0])
			if err != nil {
				return runtime.Unit, err
			}
			if !it.next() {
				return runtime.Variant("None"), nil
			}
			best := it.cur
			for it.next() {
				c, err := enumCompare(it.cur, best)
				if err != nil {
					return runtime.Unit, err
				}
				if c*sign > 0 {
					best = it.cur
				}
			}
			return runtime.Variant("Some", best), nil
		}
	}
	def("Enum.min", 1, extreme("min", -1))
	def("Enum.max", 1, extreme("max", 1))

	// uniq — первые вхождения по равенству ключей (§4.8), как у set.
	def("Enum.uniq", 1, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		it, err := newEnumIter("uniq", args[0])
		if err != nil {
			return runtime.Unit, err
		}
		seen := runtime.Set()
		out := runtime.NewListBuilder(enumHint(args[0]))
		for it.next() {
			if seen.SetHas(it.cur) {
				continue
			}
			seen = seen.SetAdd(it.cur)
			out.Add(it.cur)
		}
		return out.List(), nil
	})

	def("Enum.join", 2, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		it, err := newEnumIter("join", args[0])
		if err != nil {
			return runtime.Unit, err
		}
		if args[1].Kind != runtime.KindStr {
			return runtime.Unit, modTypeErr("enum", "join", args[1])
		}
		var sb strings.Builder
		for first := true; it.next(); first = false {
			if it.cur.Kind != runtime.KindStr {
				return runtime.Unit, modTypeErr("enum", "join", it.cur)
			}
			if !first {
				sb.WriteString(args[1].Str)
			}
			sb.WriteString(it.cur.Str)
		}
		return runtime.Str(sb.String()), nil
	})
}
