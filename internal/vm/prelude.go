package vm

import (
	"fmt"
	"math/big"
	"os"
	"strings"

	"github.com/it1ro/brig-lang/internal/runtime"
)

// InstallPrelude наполняет глобальную таблицу встроенными функциями (§11.5).
//
// Акторные примитивы (spawn/send/recv/watch/...) реализованы опкодами
// ВМ (см. compileCall в compiler.go) — не как глобалы.
//
// Sprint 5.1–5.3: list материализует Range, добавлены set(), Vec.*, Map.*.
func InstallPrelude(vm *VM) {
	globals := vm.globals
	def := func(name string, arity int, fn runtime.NativeFunc) {
		globals[name] = runtime.Func(&runtime.FuncValue{
			Name: name, Arity: arity, IsNative: true, Native: fn,
		})
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
			return runtime.Int(int64(len(a.Str))), nil
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
				if runtime.Equal(a, e) {
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

	def("map", 2, func(c runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		f, xs := args[0], args[1]
		if xs.Kind != runtime.KindList {
			return runtime.Unit, fmt.Errorf("(:type_error, (:map, %s))", xs.Inspect())
		}
		out := make([]runtime.Value, 0, len(xs.List))
		for _, e := range xs.List {
			r, err := c.Call(f, []runtime.Value{e})
			if err != nil {
				return runtime.Unit, err
			}
			out = append(out, r)
		}
		return runtime.List(out...), nil
	})

	def("filter", 2, func(c runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		f, xs := args[0], args[1]
		if xs.Kind != runtime.KindList {
			return runtime.Unit, fmt.Errorf("(:type_error, (:filter, %s))", xs.Inspect())
		}
		out := make([]runtime.Value, 0, len(xs.List))
		for _, e := range xs.List {
			r, err := c.Call(f, []runtime.Value{e})
			if err != nil {
				return runtime.Unit, err
			}
			if r.Kind != runtime.KindBool {
				return runtime.Unit, fmt.Errorf(
					"(:type_error, (:filter_predicate, %s))", r.Inspect())
			}
			if r.Bool {
				out = append(out, e)
			}
		}
		return runtime.List(out...), nil
	})

	def("find", 2, func(c runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		f, xs := args[0], args[1]
		if xs.Kind != runtime.KindList {
			return runtime.Unit, fmt.Errorf("(:type_error, (:find, %s))", xs.Inspect())
		}
		for _, e := range xs.List {
			r, err := c.Call(f, []runtime.Value{e})
			if err != nil {
				return runtime.Unit, err
			}
			if r.Kind != runtime.KindBool {
				return runtime.Unit, fmt.Errorf(
					"(:type_error, (:find_predicate, %s))", r.Inspect())
			}
			if r.Bool {
				return runtime.Variant("Some", e), nil
			}
		}
		return runtime.Variant("None"), nil
	})

	def("all", 2, func(c runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		f, xs := args[0], args[1]
		if xs.Kind != runtime.KindList {
			return runtime.Unit, fmt.Errorf("(:type_error, (:all, %s))", xs.Inspect())
		}
		for _, e := range xs.List {
			r, err := c.Call(f, []runtime.Value{e})
			if err != nil {
				return runtime.Unit, err
			}
			if r.Kind != runtime.KindBool {
				return runtime.Unit, fmt.Errorf(
					"(:type_error, (:all_predicate, %s))", r.Inspect())
			}
			if !r.Bool {
				return runtime.Bool(false), nil
			}
		}
		return runtime.Bool(true), nil
	})

	def("any", 2, func(c runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		f, xs := args[0], args[1]
		if xs.Kind != runtime.KindList {
			return runtime.Unit, fmt.Errorf("(:type_error, (:any, %s))", xs.Inspect())
		}
		for _, e := range xs.List {
			r, err := c.Call(f, []runtime.Value{e})
			if err != nil {
				return runtime.Unit, err
			}
			if r.Kind != runtime.KindBool {
				return runtime.Unit, fmt.Errorf(
					"(:type_error, (:any_predicate, %s))", r.Inspect())
			}
			if r.Bool {
				return runtime.Bool(true), nil
			}
		}
		return runtime.Bool(false), nil
	})

	def("fold", 3, func(c runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		f, acc, xs := args[0], args[1], args[2]
		if xs.Kind != runtime.KindList {
			return runtime.Unit, fmt.Errorf("(:type_error, (:fold, %s))", xs.Inspect())
		}
		for _, e := range xs.List {
			r, err := c.Call(f, []runtime.Value{acc, e})
			if err != nil {
				return runtime.Unit, err
			}
			acc = r
		}
		return acc, nil
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
			if runtime.Equal(e.Key, args[1]) {
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
			if runtime.Equal(e.Key, args[1]) {
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
			if !runtime.Equal(e.Key, args[1]) {
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

	def("sys_args", 0, func(_ runtime.Caller, _ []runtime.Value) (runtime.Value, error) {
		out := make([]runtime.Value, 0, len(os.Args))
		for _, a := range os.Args {
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
