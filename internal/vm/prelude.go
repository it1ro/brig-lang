package vm

import (
	"bytes"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"
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
			return runtime.Int(int64(a.Len())), nil
		case runtime.KindVector:
			return runtime.Int(int64(a.Len())), nil
		case runtime.KindMap:
			return runtime.Int(int64(a.Len())), nil
		case runtime.KindSet:
			return runtime.Int(int64(a.Len())), nil
		case runtime.KindStr:
			// §4.8: Str — по кодпоинтам.
			return runtime.Int(int64(utf8.RuneCountInString(a.Str))), nil
		case runtime.KindBytes:
			// §3.2: Bytes — по байтам.
			return runtime.Int(int64(len(a.Bytes))), nil
		case runtime.KindTuple:
			return runtime.Int(int64(len(a.Tuple))), nil
		}
		return runtime.Unit, typeErr("len", args[0])
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
	// по KeyEqual.
	def("set", -1, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		return runtime.Set(args...), nil
	})

	// Функции высшего порядка — возобновляемые нативы (G3, T-58): на CALL
	// из байткода состояние обхода живёт в кадре актора, колбэк исполняется
	// обычным кадром и тратит редукции. Через Caller (vm.Call) — синхронно.
	defResumable("map", 2, func(args []runtime.Value) (nativeCont, error) {
		xs, f := args[0], args[1]
		if xs.Kind != runtime.KindList {
			return nil, typeErr("map", xs)
		}
		out := runtime.NewListBuilder(xs.Len())
		return &listCont{
			f: f, xs: xs.Cursor(),
			visit: func(_, r runtime.Value) (bool, runtime.Value, error) {
				out.Add(r)
				return false, runtime.Unit, nil
			},
			final: out.List,
		}, nil
	})

	defResumable("filter", 2, func(args []runtime.Value) (nativeCont, error) {
		xs, f := args[0], args[1]
		if xs.Kind != runtime.KindList {
			return nil, typeErr("filter", xs)
		}
		out := runtime.NewListBuilder(xs.Len())
		return &listCont{
			f: f, xs: xs.Cursor(),
			visit: func(e, r runtime.Value) (bool, runtime.Value, error) {
				if r.Kind != runtime.KindBool {
					return false, runtime.Unit, typeErr("filter_predicate", r)
				}
				if r.Bool {
					out.Add(e)
				}
				return false, runtime.Unit, nil
			},
			final: out.List,
		}, nil
	})

	defResumable("find", 2, func(args []runtime.Value) (nativeCont, error) {
		xs, f := args[0], args[1]
		if xs.Kind != runtime.KindList {
			return nil, typeErr("find", xs)
		}
		return &listCont{
			f: f, xs: xs.Cursor(),
			visit: func(e, r runtime.Value) (bool, runtime.Value, error) {
				if r.Kind != runtime.KindBool {
					return false, runtime.Unit, typeErr("find_predicate", r)
				}
				return r.Bool, runtime.Variant("Some", e), nil
			},
			final: func() runtime.Value { return runtime.Variant("None") },
		}, nil
	})

	defResumable("all", 2, func(args []runtime.Value) (nativeCont, error) {
		xs, f := args[0], args[1]
		if xs.Kind != runtime.KindList {
			return nil, typeErr("all", xs)
		}
		return &listCont{
			f: f, xs: xs.Cursor(),
			visit: func(_, r runtime.Value) (bool, runtime.Value, error) {
				if r.Kind != runtime.KindBool {
					return false, runtime.Unit, typeErr("all_predicate", r)
				}
				return !r.Bool, runtime.Bool(false), nil
			},
			final: func() runtime.Value { return runtime.Bool(true) },
		}, nil
	})

	defResumable("any", 2, func(args []runtime.Value) (nativeCont, error) {
		xs, f := args[0], args[1]
		if xs.Kind != runtime.KindList {
			return nil, typeErr("any", xs)
		}
		return &listCont{
			f: f, xs: xs.Cursor(),
			visit: func(_, r runtime.Value) (bool, runtime.Value, error) {
				if r.Kind != runtime.KindBool {
					return false, runtime.Unit, typeErr("any_predicate", r)
				}
				return r.Bool, runtime.Bool(true), nil
			},
			final: func() runtime.Value { return runtime.Bool(false) },
		}, nil
	})

	defResumable("fold", 3, func(args []runtime.Value) (nativeCont, error) {
		xs, acc, f := args[0], args[1], args[2]
		if xs.Kind != runtime.KindList {
			return nil, typeErr("fold", xs)
		}
		buf := make([]runtime.Value, 2)
		return &listCont{
			f: f, xs: xs.Cursor(),
			args: func(e runtime.Value) []runtime.Value {
				buf[0], buf[1] = acc, e
				return buf
			},
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
			return runtime.Unit, typeErr("Vec.push", args[0])
		}
		return args[0].VecPush(args[1]), nil
	})

	def("Vec.set", 3, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if args[0].Kind != runtime.KindVector {
			return runtime.Unit, typeErr("Vec.set", args[0])
		}
		i, ok := smallIdx(args[1])
		if !ok || i < 0 || i >= int64(args[0].Len()) {
			return runtime.Unit, &ErrRaise{Val: runtime.Tuple(
				runtime.Atom("index_out_of_bounds"),
				runtime.Tuple(args[1], runtime.Int(int64(args[0].Len()))))}
		}
		return args[0].VecSet(int(i), args[2]), nil
	})

	def("Vec.get", 2, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if args[0].Kind != runtime.KindVector {
			return runtime.Unit, typeErr("Vec.get", args[0])
		}
		i, ok := smallIdx(args[1])
		if !ok || i < 0 || i >= int64(args[0].Len()) {
			return runtime.Variant("None"), nil
		}
		return runtime.Variant("Some", args[0].At(int(i))), nil
	})

	def("Vec.len", 1, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if args[0].Kind != runtime.KindVector {
			return runtime.Unit, typeErr("Vec.len", args[0])
		}
		return runtime.Int(int64(args[0].Len())), nil
	})

	// ---- Record module (§4.7) ----

	def("Record.to_anon", 1, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		r := args[0]
		if r.Kind != runtime.KindRecord {
			return runtime.Unit, typeErr("to_anon", r)
		}
		if r.Record.Type == "" {
			return r, nil
		}
		return runtime.Record("", r.Record.Fields), nil
	})

	// ---- Map module (§4.5) ----

	def("Map.put", 3, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if args[0].Kind != runtime.KindMap {
			return runtime.Unit, typeErr("Map.put", args[0])
		}
		return args[0].MapPut(args[1], args[2]), nil
	})

	def("Map.get", 2, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if args[0].Kind != runtime.KindMap {
			return runtime.Unit, typeErr("Map.get", args[0])
		}
		if v, ok := args[0].MapGet(args[1]); ok {
			return runtime.Variant("Some", v), nil
		}
		return runtime.Variant("None"), nil
	})

	// Map.get_or(m, k, default) — значение по ключу или default (L16).
	def("Map.get_or", 3, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if args[0].Kind != runtime.KindMap {
			return runtime.Unit, modTypeErr("map", "get_or", args[0])
		}
		if v, ok := args[0].MapGet(args[1]); ok {
			return v, nil
		}
		return args[2], nil
	})

	def("Map.remove", 2, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if args[0].Kind != runtime.KindMap {
			return runtime.Unit, typeErr("Map.remove", args[0])
		}
		return args[0].MapRemove(args[1]), nil
	})

	def("Map.keys", 1, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if args[0].Kind != runtime.KindMap {
			return runtime.Unit, typeErr("Map.keys", args[0])
		}
		out := make([]runtime.Value, 0, args[0].Len())
		for _, e := range args[0].Entries() {
			out = append(out, e.Key)
		}
		return runtime.List(out...), nil
	})

	// ---- Bytes module (§3.2, A4.2) ----

	// Bytes.to_str(b) — декодирует байты в UTF-8 строку. Невалидный
	// UTF-8 → raise(:invalid_utf8, b) (§C.6).
	def("Bytes.to_str", 1, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if args[0].Kind != runtime.KindBytes {
			return runtime.Unit, typeErr("Bytes.to_str", args[0])
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
			return runtime.Unit, typeErr("Str.to_bytes", args[0])
		}
		return runtime.Bytes([]byte(args[0].Str)), nil
	})

	def("Bytes.slice", 3, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if args[0].Kind != runtime.KindBytes {
			return runtime.Unit, typeErr("Bytes.slice", args[0])
		}
		start, end, err := sliceBounds(args[1], args[2], int64(len(args[0].Bytes)))
		if err != nil {
			return runtime.Unit, err
		}
		out := make([]byte, end-start)
		copy(out, args[0].Bytes[start:end])
		return runtime.Bytes(out), nil
	})

	def("Bytes.find", 2, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if args[0].Kind != runtime.KindBytes {
			return runtime.Unit, typeErr("Bytes.find", args[0])
		}
		if args[1].Kind != runtime.KindBytes {
			return runtime.Unit, typeErr("Bytes.find", args[1])
		}
		i := bytes.Index(args[0].Bytes, args[1].Bytes)
		if i < 0 {
			return runtime.Variant("None"), nil
		}
		return runtime.Variant("Some", runtime.Int(int64(i))), nil
	})

	def("Bytes.split", 2, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if args[0].Kind != runtime.KindBytes {
			return runtime.Unit, typeErr("Bytes.split", args[0])
		}
		if args[1].Kind != runtime.KindBytes {
			return runtime.Unit, typeErr("Bytes.split", args[1])
		}
		var parts [][]byte
		if len(args[1].Bytes) == 0 {
			for _, b := range args[0].Bytes {
				parts = append(parts, []byte{b})
			}
		} else {
			parts = bytes.Split(args[0].Bytes, args[1].Bytes)
		}
		out := make([]runtime.Value, len(parts))
		for i, p := range parts {
			out[i] = runtime.Bytes(p)
		}
		return runtime.List(out...), nil
	})

	def("Bytes.concat", 2, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if args[0].Kind != runtime.KindBytes {
			return runtime.Unit, typeErr("Bytes.concat", args[0])
		}
		if args[1].Kind != runtime.KindBytes {
			return runtime.Unit, typeErr("Bytes.concat", args[1])
		}
		out := make([]byte, 0, len(args[0].Bytes)+len(args[1].Bytes))
		out = append(out, args[0].Bytes...)
		out = append(out, args[1].Bytes...)
		return runtime.Bytes(out), nil
	})

	def("Bytes.at", 2, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if args[0].Kind != runtime.KindBytes {
			return runtime.Unit, typeErr("Bytes.at", args[0])
		}
		i, err := indexToInt(args[1])
		if err != nil {
			return runtime.Unit, err
		}
		if i < 0 || i >= int64(len(args[0].Bytes)) {
			return runtime.Unit, &ErrRaise{Val: runtime.Tuple(
				runtime.Atom("index_out_of_bounds"),
				runtime.Tuple(args[1], runtime.Int(int64(len(args[0].Bytes)))))}
		}
		return runtime.Int(int64(args[0].Bytes[i])), nil
	})

	// ---- Str module (§4.8) ----

	def("Str.split", 2, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if args[0].Kind != runtime.KindStr {
			return runtime.Unit, typeErr("Str.split", args[0])
		}
		if args[1].Kind != runtime.KindStr {
			return runtime.Unit, typeErr("Str.split", args[1])
		}
		var parts []string
		if args[1].Str == "" {
			for _, r := range args[0].Str {
				parts = append(parts, string(r))
			}
		} else {
			parts = strings.Split(args[0].Str, args[1].Str)
		}
		out := make([]runtime.Value, len(parts))
		for i, p := range parts {
			out[i] = runtime.Str(p)
		}
		return runtime.List(out...), nil
	})

	def("Str.join", 2, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if args[0].Kind != runtime.KindList {
			return runtime.Unit, typeErr("Str.join", args[0])
		}
		if args[1].Kind != runtime.KindStr {
			return runtime.Unit, typeErr("Str.join", args[1])
		}
		parts := make([]string, args[0].Len())
		for i, v := range args[0].Elems() {
			if v.Kind != runtime.KindStr {
				return runtime.Unit, typeErr("Str.join", v)
			}
			parts[i] = v.Str
		}
		return runtime.Str(strings.Join(parts, args[1].Str)), nil
	})

	def("Str.trim", 1, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if args[0].Kind != runtime.KindStr {
			return runtime.Unit, typeErr("Str.trim", args[0])
		}
		return runtime.Str(strings.TrimSpace(args[0].Str)), nil
	})

	def("Str.find", 2, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if args[0].Kind != runtime.KindStr {
			return runtime.Unit, typeErr("Str.find", args[0])
		}
		if args[1].Kind != runtime.KindStr {
			return runtime.Unit, typeErr("Str.find", args[1])
		}
		i := strings.Index(args[0].Str, args[1].Str)
		if i < 0 {
			return runtime.Variant("None"), nil
		}
		return runtime.Variant("Some", runtime.Int(int64(utf8.RuneCountInString(args[0].Str[:i])))), nil
	})

	def("Str.replace", 3, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if args[0].Kind != runtime.KindStr {
			return runtime.Unit, typeErr("Str.replace", args[0])
		}
		if args[1].Kind != runtime.KindStr {
			return runtime.Unit, typeErr("Str.replace", args[1])
		}
		if args[2].Kind != runtime.KindStr {
			return runtime.Unit, typeErr("Str.replace", args[2])
		}
		return runtime.Str(strings.ReplaceAll(args[0].Str, args[1].Str, args[2].Str)), nil
	})

	def("Str.starts_with?", 2, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if args[0].Kind != runtime.KindStr {
			return runtime.Unit, typeErr("Str.starts_with?", args[0])
		}
		if args[1].Kind != runtime.KindStr {
			return runtime.Unit, typeErr("Str.starts_with?", args[1])
		}
		return runtime.Bool(strings.HasPrefix(args[0].Str, args[1].Str)), nil
	})

	def("Str.ends_with?", 2, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if args[0].Kind != runtime.KindStr {
			return runtime.Unit, typeErr("Str.ends_with?", args[0])
		}
		if args[1].Kind != runtime.KindStr {
			return runtime.Unit, typeErr("Str.ends_with?", args[1])
		}
		return runtime.Bool(strings.HasSuffix(args[0].Str, args[1].Str)), nil
	})

	def("Str.lower", 1, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if args[0].Kind != runtime.KindStr {
			return runtime.Unit, typeErr("Str.lower", args[0])
		}
		return runtime.Str(strings.ToLower(args[0].Str)), nil
	})

	def("Str.upper", 1, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if args[0].Kind != runtime.KindStr {
			return runtime.Unit, typeErr("Str.upper", args[0])
		}
		return runtime.Str(strings.ToUpper(args[0].Str)), nil
	})

	def("Str.slice", 3, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if args[0].Kind != runtime.KindStr {
			return runtime.Unit, typeErr("Str.slice", args[0])
		}
		runes := []rune(args[0].Str)
		start, end, err := sliceBounds(args[1], args[2], int64(len(runes)))
		if err != nil {
			return runtime.Unit, err
		}
		return runtime.Str(string(runes[start:end])), nil
	})

	def("Str.to_int", 1, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if args[0].Kind != runtime.KindStr {
			return runtime.Unit, typeErr("Str.to_int", args[0])
		}
		n, err := strconv.ParseInt(strings.TrimSpace(args[0].Str), 10, 64)
		if err != nil {
			return runtime.Variant("None"), nil
		}
		return runtime.Variant("Some", runtime.Int(n)), nil
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
				return runtime.Unit, parseErr("to_int", a)
			}
			return runtime.Int(n), nil
		}
		return runtime.Unit, typeErr("to_int", a)
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
				return runtime.Unit, parseErr("to_float", a)
			}
			return runtime.Float(f), nil
		}
		return runtime.Unit, typeErr("to_float", a)
	})

	// ---- Global (§12.11) ----

	installGlobal(def)

	// ---- Timer / Time (§12.11) ----

	def("Timer.send_after", 3, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		return vm.scheduler.sendAfter(args[0], args[1], args[2])
	})
	def("Timer.cancel", 1, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		return vm.scheduler.cancelTimer(args[0])
	})
	def("Time.monotonic_ms", 0, func(_ runtime.Caller, _ []runtime.Value) (runtime.Value, error) {
		return runtime.Int(monotonicMillis()), nil
	})
	def("Time.now", 0, func(_ runtime.Caller, _ []runtime.Value) (runtime.Value, error) {
		return runtime.Int(time.Now().UnixMilli()), nil
	})

	// ---- Port, Signal, Sys.halt (§12.12) ----

	installPorts(def)

	// ---- Sys ----

	def("Sys.args", 0, func(_ runtime.Caller, _ []runtime.Value) (runtime.Value, error) {
		out := make([]runtime.Value, 0, len(vm.args))
		for _, a := range vm.args {
			out = append(out, runtime.Str(a))
		}
		return runtime.List(out...), nil
	})

	// ---- Actor (§12.10, §12.13) ----

	def("Actor.list", 0, func(_ runtime.Caller, _ []runtime.Value) (runtime.Value, error) {
		return vm.scheduler.actorList(), nil
	})

	def("Actor.info", 1, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if args[0].Kind != runtime.KindPid {
			return runtime.Unit, typeErr("Actor.info", args[0])
		}
		return vm.scheduler.actorInfo(args[0].Pid), nil
	})

	// ---- Telemetry (§12.14) ----

	installTelemetry(vm)

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

// sliceBounds проверяет [start, end) на длине length (Str.slice/Bytes.slice,
// T-148). Вне границ — ловимый (:index_out_of_bounds, (idx, length)) с тем
// аргументом, который вышел за пределы (§10.4).
func sliceBounds(startV, endV runtime.Value, length int64) (int64, int64, error) {
	start, err := indexToInt(startV)
	if err != nil {
		return 0, 0, err
	}
	end, err := indexToInt(endV)
	if err != nil {
		return 0, 0, err
	}
	if start < 0 || start > length {
		return 0, 0, &ErrRaise{Val: runtime.Tuple(
			runtime.Atom("index_out_of_bounds"),
			runtime.Tuple(startV, runtime.Int(length)))}
	}
	if end < start || end > length {
		return 0, 0, &ErrRaise{Val: runtime.Tuple(
			runtime.Atom("index_out_of_bounds"),
			runtime.Tuple(endV, runtime.Int(length)))}
	}
	return start, end, nil
}

// ---- возобновляемые нативы (G3, T-58) ----

// nativeStep — ход возобновляемого натива: вызвать fn(args) и вернуться в
// resume с его результатом либо (done) завершиться со значением res.
type nativeStep struct {
	fn   runtime.Value
	args []runtime.Value
	done bool
	res  runtime.Value
	// block — кадр ждёт (служебный актор Telemetry: очередь пуста).
	block bool
	// exit — drain в режиме exit закончил ensure: unwind от exit
	// продолжается (doc 02 §5.1).
	exit bool
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
		// st.args — буфер cont, переписываемый следующим resume; нативный
		// колбэк получает его через Caller как есть и может удержать.
		args := append([]runtime.Value(nil), st.args...)
		if ret, err = c.Call(st.fn, args); err != nil {
			return runtime.Unit, err
		}
	}
}

// listCont обходит xs, вызывая f на каждом элементе (с аргументами
// args(e), по умолчанию — e). visit получает элемент и результат колбэка
// и может завершить обход досрочно со значением res; иначе итог — final().
// Срез аргументов — буфер, переиспользуемый между колбэками (T-103):
// enterCall копирует его в регистры кадра или в свежий срез натива.
type listCont struct {
	f     runtime.Value
	xs    runtime.ListCursor // стоит на элементе, чей колбэк исполняется
	arg   [1]runtime.Value
	args  func(e runtime.Value) []runtime.Value
	visit func(e, r runtime.Value) (stop bool, res runtime.Value, err error)
	final func() runtime.Value
}

func (c *listCont) resume(ret runtime.Value) (nativeStep, error) {
	if c.xs.Started() {
		stop, res, err := c.visit(c.xs.Value(), ret)
		if err != nil {
			return nativeStep{}, err
		}
		if stop {
			return nativeStep{done: true, res: res}, nil
		}
	}
	if !c.xs.Next() {
		return nativeStep{done: true, res: c.final()}, nil
	}
	var args []runtime.Value
	if c.args != nil {
		args = c.args(c.xs.Value())
	} else {
		c.arg[0] = c.xs.Value()
		args = c.arg[:]
	}
	return nativeStep{fn: c.f, args: args}, nil
}
