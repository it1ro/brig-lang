package vm

import (
	"fmt"
	"math/big"
	"strings"
	"unicode/utf8"

	"github.com/it1ro/brig-lang/internal/runtime"
)

// InstallPrelude наполняет глобальную таблицу встроенными функциями (§11.5).
//
// Акторные примитивы (spawn/send/recv/watch/...) реализованы опкодами
// ВМ (см. compileCall в compiler.go) — не как глобалы.
//
// Sprint 5.1–5.4: list материализует Range, добавлены set(), Vec.*, Map.*,
// Bytes.to_str, Str.to_bytes.
func InstallPrelude(vm *VM) {
	globals := vm.globals
	def := func(name string, arity int, fn runtime.NativeFunc) {
		globals[name] = runtime.Func(&runtime.FuncValue{
			Name: name, Arity: arity, IsNative: true, Native: fn,
		})
	}
	defResumable := func(name string, arity int, start resumableFunc) {
		fv := &runtime.FuncValue{
			Name: name, Arity: arity, IsNative: true,
			Native: func(c runtime.Caller, args []runtime.Value) (runtime.Value, error) {
				k, err := start(args)
				if err != nil {
					return runtime.Unit, err
				}
				return runSync(c, k)
			},
		}
		globals[name] = runtime.Func(fv)
		vm.resumable[fv] = start
	}

	// ---- I/O ----

	def("print", -1, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		line := ""
		for i, a := range args {
			if i > 0 {
				line += " "
			}
			line += a.Inspect()
		}
		fmt.Println(line)
		return runtime.Unit, nil
	})
	def("eprint", -1, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		for i, a := range args {
			if i > 0 {
				fmt.Print(" ")
			}
			fmt.Print(a.Inspect())
		}
		fmt.Println()
		return runtime.Unit, nil
	})
	def("log", -1, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		line := ""
		for i, a := range args {
			if i > 0 {
				line += " "
			}
			line += a.Inspect()
		}
		fmt.Println("log:", line)
		return runtime.Unit, nil
	})

	// ---- Коллекции ----

	def("len", 1, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		switch a := args[0]; a.Kind {
		case runtime.KindList:
			return runtime.Int(int64(len(a.List))), nil
		case runtime.KindVector:
			return runtime.Int(int64(len(a.Vector))), nil
		case runtime.KindMap:
			return runtime.Int(int64(len(a.Map))), nil
		case runtime.KindSet:
			return runtime.Int(int64(len(a.Set))), nil
		case runtime.KindStr:
			// §4.8: Str — по кодпоинтам.
			return runtime.Int(int64(utf8.RuneCountInString(a.Str))), nil
		case runtime.KindBytes:
			// §3.2: Bytes — по байтам.
			return runtime.Int(int64(len(a.Bytes))), nil
		case runtime.KindTuple:
			return runtime.Int(int64(len(a.Tuple))), nil
		}
		return runtime.Unit, fmt.Errorf("(:type_error, (:len, %s))", args[0].Inspect())
	})

	// list(...) — вариадический конструктор. Особый случай: единственный
	// аргумент-диапазон материализуется в список (§4.3).
	def("list", -1, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if len(args) == 1 && args[0].Kind == runtime.KindRange {
			return materializeRange(args[0])
		}
		return runtime.List(args...), nil
	})

	// set(...) — конструктор множества (Sprint 5.2, §4.6). Дедуплицирует
	// по структурному равенству, сохраняя порядок первого появления.
	def("set", -1, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		out := make([]runtime.Value, 0, len(args))
		for _, a := range args {
			found := false
			for _, e := range out {
				if runtime.KeyEqual(a, e) {
					found = true
					break
				}
			}
			if !found {
				out = append(out, a)
			}
		}
		return runtime.Set(out...), nil
	})

	// Функции высшего порядка — возобновляемые нативы (G3, T-58): на CALL
	// из байткода состояние обхода живёт в кадре актора, колбэк исполняется
	// обычным кадром и тратит редукции. Через Caller (vm.Call) — синхронно.
	defResumable("map", 2, func(args []runtime.Value) (nativeCont, error) {
		f, xs := args[0], args[1]
		if xs.Kind != runtime.KindList {
			return nil, fmt.Errorf("(:type_error, (:map, %s))", xs.Inspect())
		}
		out := make([]runtime.Value, 0, len(xs.List))
		return &listCont{
			f: f, xs: xs.List,
			visit: func(_, r runtime.Value) (bool, runtime.Value, error) {
				out = append(out, r)
				return false, runtime.Unit, nil
			},
			final: func() runtime.Value { return runtime.List(out...) },
		}, nil
	})

	defResumable("filter", 2, func(args []runtime.Value) (nativeCont, error) {
		f, xs := args[0], args[1]
		if xs.Kind != runtime.KindList {
			return nil, fmt.Errorf("(:type_error, (:filter, %s))", xs.Inspect())
		}
		out := make([]runtime.Value, 0, len(xs.List))
		return &listCont{
			f: f, xs: xs.List,
			visit: func(e, r runtime.Value) (bool, runtime.Value, error) {
				if r.Kind != runtime.KindBool {
					return false, runtime.Unit, fmt.Errorf(
						"(:type_error, (:filter_predicate, %s))", r.Inspect())
				}
				if r.Bool {
					out = append(out, e)
				}
				return false, runtime.Unit, nil
			},
			final: func() runtime.Value { return runtime.List(out...) },
		}, nil
	})

	defResumable("find", 2, func(args []runtime.Value) (nativeCont, error) {
		f, xs := args[0], args[1]
		if xs.Kind != runtime.KindList {
			return nil, fmt.Errorf("(:type_error, (:find, %s))", xs.Inspect())
		}
		return &listCont{
			f: f, xs: xs.List,
			visit: func(e, r runtime.Value) (bool, runtime.Value, error) {
				if r.Kind != runtime.KindBool {
					return false, runtime.Unit, fmt.Errorf(
						"(:type_error, (:find_predicate, %s))", r.Inspect())
				}
				return r.Bool, runtime.Variant("Some", e), nil
			},
			final: func() runtime.Value { return runtime.Variant("None") },
		}, nil
	})

	defResumable("all", 2, func(args []runtime.Value) (nativeCont, error) {
		f, xs := args[0], args[1]
		if xs.Kind != runtime.KindList {
			return nil, fmt.Errorf("(:type_error, (:all, %s))", xs.Inspect())
		}
		return &listCont{
			f: f, xs: xs.List,
			visit: func(_, r runtime.Value) (bool, runtime.Value, error) {
				if r.Kind != runtime.KindBool {
					return false, runtime.Unit, fmt.Errorf(
						"(:type_error, (:all_predicate, %s))", r.Inspect())
				}
				return !r.Bool, runtime.Bool(false), nil
			},
			final: func() runtime.Value { return runtime.Bool(true) },
		}, nil
	})

	defResumable("any", 2, func(args []runtime.Value) (nativeCont, error) {
		f, xs := args[0], args[1]
		if xs.Kind != runtime.KindList {
			return nil, fmt.Errorf("(:type_error, (:any, %s))", xs.Inspect())
		}
		return &listCont{
			f: f, xs: xs.List,
			visit: func(_, r runtime.Value) (bool, runtime.Value, error) {
				if r.Kind != runtime.KindBool {
					return false, runtime.Unit, fmt.Errorf(
						"(:type_error, (:any_predicate, %s))", r.Inspect())
				}
				return r.Bool, runtime.Bool(true), nil
			},
			final: func() runtime.Value { return runtime.Bool(false) },
		}, nil
	})

	defResumable("fold", 3, func(args []runtime.Value) (nativeCont, error) {
		f, acc, xs := args[0], args[1], args[2]
		if xs.Kind != runtime.KindList {
			return nil, fmt.Errorf("(:type_error, (:fold, %s))", xs.Inspect())
		}
		return &listCont{
			f: f, xs: xs.List,
			args: func(e runtime.Value) []runtime.Value { return []runtime.Value{acc, e} },
			visit: func(_, r runtime.Value) (bool, runtime.Value, error) {
				acc = r
				return false, runtime.Unit, nil
			},
			final: func() runtime.Value { return acc },
		}, nil
	})

	// ---- Vec module (§4.4) ----

	def("Vec.push", 2, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if args[0].Kind != runtime.KindVector {
			return runtime.Unit, fmt.Errorf("(:type_error, (:Vec.push, %s))", args[0].Inspect())
		}
		out := make([]runtime.Value, 0, len(args[0].Vector)+1)
		out = append(out, args[0].Vector...)
		out = append(out, args[1])
		return runtime.Vector(out...), nil
	})

	def("Vec.set", 3, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if args[0].Kind != runtime.KindVector {
			return runtime.Unit, fmt.Errorf("(:type_error, (:Vec.set, %s))", args[0].Inspect())
		}
		i, ok := smallIdx(args[1])
		if !ok || i < 0 || i >= int64(len(args[0].Vector)) {
			return runtime.Unit, &ErrRaise{Val: runtime.Tuple(
				runtime.Atom("index_out_of_bounds"),
				runtime.Tuple(args[1], runtime.Int(int64(len(args[0].Vector)))))}
		}
		out := make([]runtime.Value, len(args[0].Vector))
		copy(out, args[0].Vector)
		out[i] = args[2]
		return runtime.Vector(out...), nil
	})

	def("Vec.get", 2, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if args[0].Kind != runtime.KindVector {
			return runtime.Unit, fmt.Errorf("(:type_error, (:Vec.get, %s))", args[0].Inspect())
		}
		i, ok := smallIdx(args[1])
		if !ok || i < 0 || i >= int64(len(args[0].Vector)) {
			return runtime.Variant("None"), nil
		}
		return runtime.Variant("Some", args[0].Vector[i]), nil
	})

	def("Vec.len", 1, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if args[0].Kind != runtime.KindVector {
			return runtime.Unit, fmt.Errorf("(:type_error, (:Vec.len, %s))", args[0].Inspect())
		}
		return runtime.Int(int64(len(args[0].Vector))), nil
	})

	// ---- Map module (§4.5) ----

	def("Map.put", 3, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if args[0].Kind != runtime.KindMap {
			return runtime.Unit, fmt.Errorf("(:type_error, (:Map.put, %s))", args[0].Inspect())
		}
		out := make([]runtime.MapEntry, 0, len(args[0].Map)+1)
		found := false
		for _, e := range args[0].Map {
			if runtime.KeyEqual(e.Key, args[1]) {
				out = append(out, runtime.MapEntry{Key: args[1], Val: args[2]})
				found = true
			} else {
				out = append(out, e)
			}
		}
		if !found {
			out = append(out, runtime.MapEntry{Key: args[1], Val: args[2]})
		}
		return runtime.Map(out), nil
	})

	def("Map.get", 2, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if args[0].Kind != runtime.KindMap {
			return runtime.Unit, fmt.Errorf("(:type_error, (:Map.get, %s))", args[0].Inspect())
		}
		for _, e := range args[0].Map {
			if runtime.KeyEqual(e.Key, args[1]) {
				return runtime.Variant("Some", e.Val), nil
			}
		}
		return runtime.Variant("None"), nil
	})

	def("Map.remove", 2, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if args[0].Kind != runtime.KindMap {
			return runtime.Unit, fmt.Errorf("(:type_error, (:Map.remove, %s))", args[0].Inspect())
		}
		out := make([]runtime.MapEntry, 0, len(args[0].Map))
		for _, e := range args[0].Map {
			if !runtime.KeyEqual(e.Key, args[1]) {
				out = append(out, e)
			}
		}
		return runtime.Map(out), nil
	})

	def("Map.keys", 1, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if args[0].Kind != runtime.KindMap {
			return runtime.Unit, fmt.Errorf("(:type_error, (:Map.keys, %s))", args[0].Inspect())
		}
		out := make([]runtime.Value, 0, len(args[0].Map))
		for _, e := range args[0].Map {
			out = append(out, e.Key)
		}
		return runtime.List(out...), nil
	})

	// ---- Bytes module (§3.2, A4.2) ----

	// Bytes.to_str(b) — декодирует байты в UTF-8 строку. Невалидный
	// UTF-8 → raise(:invalid_utf8, b) (§C.6).
	def("Bytes.to_str", 1, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if args[0].Kind != runtime.KindBytes {
			return runtime.Unit, fmt.Errorf("(:type_error, (:Bytes.to_str, %s))", args[0].Inspect())
		}
		s := string(args[0].Bytes)
		if !utf8.ValidString(s) {
			return runtime.Unit, &ErrRaise{Val: runtime.Tuple(
				runtime.Atom("invalid_utf8"), args[0])}
		}
		return runtime.Str(s), nil
	})

	// Str.to_bytes(s) — всегда успешно, возвращает UTF-8-байты (§C.6).
	def("Str.to_bytes", 1, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if args[0].Kind != runtime.KindStr {
			return runtime.Unit, fmt.Errorf("(:type_error, (:Str.to_bytes, %s))", args[0].Inspect())
		}
		return runtime.Bytes([]byte(args[0].Str)), nil
	})

	// ---- Конверсии ----

	def("to_str", 1, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		return runtime.Str(args[0].Inspect()), nil
	})

	def("to_int", 1, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		a := args[0]
		switch a.Kind {
		case runtime.KindInt:
			return a, nil
		case runtime.KindFloat:
			return runtime.Int(int64(a.Float)), nil
		case runtime.KindStr:
			s := strings.TrimSpace(a.Str)
			var n int64
			if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
				return runtime.Unit, &ErrRaise{Val: runtime.Tuple(
					runtime.Atom("badarg"), a)}
			}
			return runtime.Int(n), nil
		}
		return runtime.Unit, &ErrRaise{Val: runtime.Tuple(
			runtime.Atom("badarg"), a)}
	})

	def("to_float", 1, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		a := args[0]
		switch a.Kind {
		case runtime.KindFloat:
			return a, nil
		case runtime.KindInt:
			if a.IsSmall {
				return runtime.Float(float64(a.SmallInt)), nil
			}
			f, _ := new(big.Float).SetInt(a.AsBig()).Float64()
			return runtime.Float(f), nil
		case runtime.KindStr:
			s := strings.TrimSpace(a.Str)
			var f float64
			if _, err := fmt.Sscanf(s, "%f", &f); err != nil {
				return runtime.Unit, &ErrRaise{Val: runtime.Tuple(
					runtime.Atom("badarg"), a)}
			}
			return runtime.Float(f), nil
		}
		return runtime.Unit, &ErrRaise{Val: runtime.Tuple(
			runtime.Atom("badarg"), a)}
	})

	// ---- Sys ----

	def("Sys.args", 0, func(_ runtime.Caller, _ []runtime.Value) (runtime.Value, error) {
		out := make([]runtime.Value, 0, len(vm.args))
		for _, a := range vm.args {
			out = append(out, runtime.Str(a))
		}
		return runtime.List(out...), nil
	})

	// ---- Встроенные варианты (§10.1) ----

	globals["None"] = runtime.Variant("None")
	def("Some", 1, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		return runtime.Variant("Some", args...), nil
	})
	def("Ok", 1, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		return runtime.Variant("Ok", args...), nil
	})
	def("Error", 1, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		return runtime.Variant("Error", args...), nil
	})

	// ---- Эффекты ----

	def("raise", 1, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		return runtime.Unit, &ErrRaise{Val: args[0]}
	})
	def("assert", 1, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if args[0].Kind != runtime.KindBool {
			return runtime.Unit, &ErrRaise{Val: runtime.Tuple(
				runtime.Atom("type_error"),
				runtime.Tuple(runtime.Atom("assert_expected_bool"), args[0]))}
		}
		if !args[0].Bool {
			return runtime.Unit, &ErrRaise{Val: runtime.Tuple(
				runtime.Atom("assertion_failed"), runtime.Unit)}
		}
		return runtime.Unit, nil
	})
}

// materializeRange превращает Range в List (§4.3). Убывающий диапазон
// (в т.ч. вычисленный в рантайме) → raise((:range_error, (start, end))).
func materializeRange(r runtime.Value) (runtime.Value, error) {
	s, e := r.RangeStart, r.RangeEnd
	if s > e {
		return runtime.Unit, &ErrRaise{Val: runtime.Tuple(
			runtime.Atom("range_error"),
			runtime.Tuple(runtime.Int(s), runtime.Int(e)))}
	}
	out := make([]runtime.Value, 0, e-s+1)
	for i := s; i <= e; i++ {
		out = append(out, runtime.Int(i))
	}
	return runtime.List(out...), nil
}

// smallIdx извлекает int64 из small-int значения. Для big-int возвращает false.
func smallIdx(v runtime.Value) (int64, bool) {
	if v.Kind != runtime.KindInt || !v.IsSmall {
		return 0, false
	}
	return v.SmallInt, true
}

// ---- возобновляемые нативы (G3, T-58) ----

// nativeStep — ход возобновляемого натива: вызвать fn(args) и вернуться в
// resume с его результатом либо (done) завершиться со значением res.
type nativeStep struct {
	fn   runtime.Value
	args []runtime.Value
	done bool
	res  runtime.Value
}

// nativeCont — состояние нативной функции высшего порядка между вызовами
// колбэка. resume получает результат предыдущего колбэка (в первый раз —
// Unit).
type nativeCont interface {
	resume(ret runtime.Value) (nativeStep, error)
}

// resumableFunc проверяет аргументы вызова и строит nativeCont.
type resumableFunc func(args []runtime.Value) (nativeCont, error)

// runSync исполняет nativeCont синхронно, вызывая колбэки через Caller.
func runSync(c runtime.Caller, k nativeCont) (runtime.Value, error) {
	ret := runtime.Unit
	for {
		st, err := k.resume(ret)
		if err != nil {
			return runtime.Unit, err
		}
		if st.done {
			return st.res, nil
		}
		if ret, err = c.Call(st.fn, st.args); err != nil {
			return runtime.Unit, err
		}
	}
}

// listCont обходит xs, вызывая f на каждом элементе (с аргументами
// args(e), по умолчанию — e). visit получает элемент и результат колбэка
// и может завершить обход досрочно со значением res; иначе итог — final().
type listCont struct {
	f     runtime.Value
	xs    []runtime.Value
	i     int
	args  func(e runtime.Value) []runtime.Value
	visit func(e, r runtime.Value) (stop bool, res runtime.Value, err error)
	final func() runtime.Value
}

func (c *listCont) resume(ret runtime.Value) (nativeStep, error) {
	if c.i > 0 {
		stop, res, err := c.visit(c.xs[c.i-1], ret)
		if err != nil {
			return nativeStep{}, err
		}
		if stop {
			return nativeStep{done: true, res: res}, nil
		}
	}
	if c.i == len(c.xs) {
		return nativeStep{done: true, res: c.final()}, nil
	}
	e := c.xs[c.i]
	c.i++
	args := []runtime.Value{e}
	if c.args != nil {
		args = c.args(e)
	}
	return nativeStep{fn: c.f, args: args}, nil
}
