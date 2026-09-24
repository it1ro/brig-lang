package vm

import (
	"fmt"

	"github.com/it1ro/brig-lang/internal/runtime"
)

// InstallPrelude наполняет глобальную таблицу встроенными функциями (§11.5).
//
// Срез: I/O, базовые коллекции, конверсии. Акторные примитивы —
// заглушки, бросающие "не реализовано в этом срезе" (подэтап 4.8).
func InstallPrelude(globals map[string]runtime.Value) {
	def := func(name string, arity int, fn runtime.NativeFunc) {
		globals[name] = runtime.Func(&runtime.FuncValue{
			Name: name, Arity: arity, IsNative: true, Native: fn,
		})
	}

	// I/O
	def("print", -1, func(c runtime.Caller, args []runtime.Value) (runtime.Value, error) {
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
	def("eprint", -1, func(c runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		for i, a := range args {
			if i > 0 {
				fmt.Print(" ")
			}
			fmt.Print(a.Inspect())
		}
		fmt.Println()
		return runtime.Unit, nil
	})

	// len
	def("len", 1, func(c runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		switch a := args[0]; a.Kind {
		case runtime.KindList:
			return runtime.Int(int64(len(a.List))), nil
		case runtime.KindVector:
			return runtime.Int(int64(len(a.Vector))), nil
		case runtime.KindMap:
			return runtime.Int(int64(len(a.Map))), nil
		case runtime.KindStr:
			return runtime.Int(int64(len(a.Str))), nil
		case runtime.KindTuple:
			return runtime.Int(int64(len(a.Tuple))), nil
		}
		return runtime.Unit, fmt.Errorf("(:type_error, (:len, %s))", args[0].Inspect())
	})

	// to_str
	def("to_str", 1, func(c runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		return runtime.Str(args[0].Inspect()), nil
	})

	// list(range) материализует 1..n в срезе без полноценного Range-значения.
	// Здесь упрощение: поддерживаем только list(1 to n) через отдельную
	// функцию range_to в срезе НЕ реализованы; оставляем list(..args).
	def("list", -1, func(c runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		return runtime.List(args...), nil
	})

	// map(f, xs)
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

	// fold(f, acc, xs)
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

	// Встроенные варианты: Some/None/Ok/Error (§10.1).
	// Конструктор без аргументов — значение; с аргументами — функция.
	globals["None"] = runtime.Variant("None")
	def("Some", 1, func(c runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		return runtime.Variant("Some", args...), nil
	})
	def("Ok", 1, func(c runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		return runtime.Variant("Ok", args...), nil
	})
	def("Error", 1, func(c runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		return runtime.Variant("Error", args...), nil
	})

	// Эффекты.
	def("raise", 1, func(c runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		return runtime.Unit, &ErrRaise{Val: args[0]}
	})
	def("assert", 1, func(c runtime.Caller, args []runtime.Value) (runtime.Value, error) {
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

	// Акторные примитивы — заглушки среза (подэтап 4.8).
	notImpl := func(name string) runtime.NativeFunc {
		return func(c runtime.Caller, args []runtime.Value) (runtime.Value, error) {
			return runtime.Unit, fmt.Errorf(
				"%s: акторы не реализованы в вертикальном срезе (подэтап 4.8)", name)
		}
	}
	for _, n := range []string{
		"spawn", "spawn_linked", "send", "link",
		"watch", "unwatch", "self", "make_ref", "mailbox_size",
	} {
		def(n, -1, notImpl(n))
	}
}
