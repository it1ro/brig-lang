package vm

import (
	"fmt"
	"os"

	"github.com/it1ro/brig-lang/internal/runtime"
)

// testCase — зарегистрированный тест (describe/it).
type testCase struct {
	group string
	name  string
	thunk runtime.Value
}

// InstallTestPrelude устанавливает минимальный тест-фреймворк
// (§16, Must). Семантика:
//
//	Test.describe(name)         — задать текущую группу
//	Test.it(name, () -> ...)    — зарегистрировать тест
//	Test.run() -> Int           — прогнать, вернуть число упавших
//
// Рекомендуемое использование в main:
//
//	Test.it("...", () -> Test.assert_eq(2 + 2, 4))
//	assert(Test.run() == 0)
//
// Exit code — в ведении CLI: `brig test` (cmd/brig/test.go) завершается
// с 1, если упал хоть один тест.
func InstallTestPrelude(vm *VM) {
	def := func(name string, arity int, fn runtime.NativeFunc) {
		vm.globals[name] = runtime.Func(&runtime.FuncValue{
			Name: name, Arity: arity, IsNative: true, Native: fn,
		})
	}

	def("Test.describe", 1, func(c runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		m, ok := c.(*VM)
		if !ok {
			return runtime.Unit, fmt.Errorf("internal: Test.describe without VM")
		}
		if args[0].Kind != runtime.KindStr {
			return runtime.Unit, &ErrRaise{Val: runtime.Tuple(
				runtime.Atom("type_error"),
				runtime.Tuple(runtime.Atom("test_describe_name"), args[0]))}
		}
		m.currentGroup = args[0].Str
		return runtime.Unit, nil
	})

	def("Test.it", 2, func(c runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		m, ok := c.(*VM)
		if !ok {
			return runtime.Unit, fmt.Errorf("internal: Test.it without VM")
		}
		if args[0].Kind != runtime.KindStr {
			return runtime.Unit, &ErrRaise{Val: runtime.Tuple(
				runtime.Atom("type_error"),
				runtime.Tuple(runtime.Atom("test_it_name"), args[0]))}
		}
		if args[1].Kind != runtime.KindFunction && args[1].Kind != runtime.KindClosure {
			return runtime.Unit, &ErrRaise{Val: runtime.Tuple(
				runtime.Atom("type_error"),
				runtime.Tuple(runtime.Atom("test_it_thunk"), args[1]))}
		}
		m.tests = append(m.tests, testCase{
			group: m.currentGroup,
			name:  args[0].Str,
			thunk: args[1],
		})
		return runtime.Unit, nil
	})

	def("Test.run", 0, func(c runtime.Caller, _ []runtime.Value) (runtime.Value, error) {
		m, ok := c.(*VM)
		if !ok {
			return runtime.Unit, fmt.Errorf("internal: Test.run without VM")
		}
		return m.runTests()
	})

	// Assertions ----------------------------------------------------------

	def("Test.assert_eq", 2, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if !runtime.Equal(args[0], args[1]) {
			return runtime.Unit, &ErrRaise{Val: runtime.Tuple(
				runtime.Atom("assert_eq_failed"),
				runtime.Tuple(args[0], args[1]))}
		}
		return runtime.Unit, nil
	})

	def("Test.assert_ne", 2, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		if runtime.Equal(args[0], args[1]) {
			return runtime.Unit, &ErrRaise{Val: runtime.Tuple(
				runtime.Atom("assert_ne_failed"),
				runtime.Tuple(args[0], args[1]))}
		}
		return runtime.Unit, nil
	})

	def("Test.assert", 1, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
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

	def("Test.fail", 1, func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
		return runtime.Unit, &ErrRaise{Val: runtime.Tuple(
			runtime.Atom("assert_fail"),
			args[0])}
	})
}

// runTests — синхронный прогон всех зарегистрированных тестов (Test.run).
// Возвращает Int(n_failed).
func (vm *VM) runTests() (runtime.Value, error) {
	passed, failed := vm.RunTests()
	fmt.Printf("\n%d passed, %d failed\n", passed, failed)
	return runtime.Int(int64(failed)), nil
}

// RunTests прогоняет тесты, зарегистрированные Test.it, и сбрасывает
// реестр: повторный прогон не запускает старые тесты. Каждый тест
// изолирован, падение одного не прерывает остальные. Итог — для
// Test.run и `brig test` (T-147).
func (vm *VM) RunTests() (passed, failed int) {
	for _, tc := range vm.tests {
		full := tc.name
		if tc.group != "" {
			full = tc.group + " > " + tc.name
		}
		_, err := vm.Call(tc.thunk, nil)
		if err != nil {
			failed++
			fmt.Fprintf(os.Stderr, "FAIL %s\n     %v\n", full, err)
		} else {
			passed++
			fmt.Printf("ok   %s\n", full)
		}
	}
	vm.tests = nil
	vm.currentGroup = ""
	return passed, failed
}
