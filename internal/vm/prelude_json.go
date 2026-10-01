package vm

import (
	"github.com/it1ro/brig-lang/internal/runtime"
)

// InstallJSONPrelude регистрирует Json.encode/Json.decode (§4.7, Must).
//
// Семантика делегируется runtime.JSONEncode/JSONDecode (I-F13): encode
// непредставимого значения — raise((:json, reason)), decode —
// Error((:json, reason)); Float 1.0 round-trip сохраняет Float; Map с ключом
// "$bytes" не коллизирует с маркером Bytes.
func InstallJSONPrelude(vm *VM) {
	def := func(name string, arity int, fn runtime.NativeFunc) {
		vm.DefineGlobal(name, runtime.Func(&runtime.FuncValue{
			Name: name, Arity: arity, IsNative: true, Native: fn,
		}))
	}

	// Json.encode(v) / Json.encode(v, { type_tag: Bool }) (§4.7).
	def("Json.encode", -1, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 && len(args) != 2 {
			return runtime.Unit, functionClause(args)
		}
		var opts runtime.JSONOptions
		if len(args) == 2 {
			o, ok := jsonEncodeOpts(args[1])
			if !ok {
				return runtime.Unit, modTypeErr("json", "encode", args[1])
			}
			opts = o
		}
		s, err := runtime.JSONEncodeOpts(args[0], opts)
		if err != nil {
			return runtime.Unit, jsonRaise(err)
		}
		return runtime.Str(s), nil
	})

	def("Json.decode", 1, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if args[0].Kind != runtime.KindStr {
			return runtime.Unit, modTypeErr("json", "decode", args[0])
		}
		v, err := runtime.JSONDecode(args[0].Str)
		if err != nil {
			return runtime.Variant("Error", jsonReason(err)), nil
		}
		return runtime.Variant("Ok", v), nil
	})

	// Json.at(v, path) — спуск по разобранному JSON (L16): `Str` — ключ
	// `Map`, `Int` — индекс `List` с нуля. Нет ключа, индекс вне списка,
	// шаг другого вида или спуск в скаляр — `None`.
	def("Json.at", 2, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if args[1].Kind != runtime.KindList {
			return runtime.Unit, modTypeErr("json", "at", args[1])
		}
		cur := args[0]
		for _, step := range args[1].Elems() {
			next, ok := jsonStep(cur, step)
			if !ok {
				return runtime.Variant("None"), nil
			}
			cur = next
		}
		return runtime.Variant("Some", cur), nil
	})
}

// jsonReason — `(:json, reason)` из ошибки runtime.JSONEncode/JSONDecode.
func jsonReason(err error) runtime.Value {
	reason := runtime.Atom("syntax")
	if je, ok := err.(*runtime.JSONError); ok {
		reason = je.Reason
	}
	return runtime.Tuple(runtime.Atom("json"), reason)
}

func jsonRaise(err error) error { return &ErrRaise{Val: jsonReason(err)} }

// jsonEncodeOpts разбирает анонимную запись опций Json.encode; известно
// только поле type_tag: Bool.
func jsonEncodeOpts(v runtime.Value) (runtime.JSONOptions, bool) {
	var o runtime.JSONOptions
	if v.Kind != runtime.KindRecord || v.Record.Type != "" {
		return o, false
	}
	for _, f := range v.Record.Fields {
		if f.Name != "type_tag" || f.Val.Kind != runtime.KindBool {
			return o, false
		}
		o.TypeTag = f.Val.Bool
	}
	return o, true
}

// jsonStep — один шаг пути Json.at.
func jsonStep(v, step runtime.Value) (runtime.Value, bool) {
	switch {
	case v.Kind == runtime.KindMap && step.Kind == runtime.KindStr:
		return v.MapGet(step)
	case v.Kind == runtime.KindList && step.Kind == runtime.KindInt && step.IsSmall:
		if i := step.SmallInt; i >= 0 && i < int64(v.Len()) {
			return v.At(int(i)), true
		}
	}
	return runtime.Unit, false
}
