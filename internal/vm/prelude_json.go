package vm

import (
	"fmt"

	"github.com/it1ro/brig-lang/internal/runtime"
)

// InstallJSONPrelude регистрирует Json.encode/Json.decode (§4.7, Must).
func InstallJSONPrelude(vm *VM) {
	def := func(name string, arity int, fn runtime.NativeFunc) {
		vm.globals[name] = runtime.Func(&runtime.FuncValue{
			Name: name, Arity: arity, IsNative: true, Native: fn,
		})
	}

	def("Json.encode", 1, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		s, err := runtime.JSONEncode(args[0])
		if err != nil {
			return runtime.Unit, &ErrRaise{Val: runtime.Tuple(
				runtime.Atom("json_encode_error"),
				runtime.Str(err.Error()))}
		}
		return runtime.Str(s), nil
	})

	def("Json.decode", 1, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if args[0].Kind != runtime.KindStr {
			return runtime.Unit, &ErrRaise{Val: runtime.Tuple(
				runtime.Atom("type_error"),
				runtime.Tuple(runtime.Atom("json_decode_arg"), args[0]))}
		}
		v, err := runtime.JSONDecode(args[0].Str)
		if err != nil {
			return runtime.Variant("Error", runtime.Str(err.Error())), nil
		}
		return runtime.Variant("Ok", v), nil
	})

	// Для отладки — там где CLI хочет вывести ошибку без Result-обёртки.
	_ = fmt.Sprintf
}
