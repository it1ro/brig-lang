package vm_test

import (
	"testing"

	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/vm"
)

// TestArithmetic — smoke-тест: runtime.Equal для Int.
func TestArithmetic(t *testing.T) {
	if got := runtime.Int(2); !runtime.Equal(got, runtime.Int(2)) {
		t.Fatal("Equal(2,2) = false")
	}
}

// TestPreludePrint — прелюдия установлена, print вызывается без падения.
func TestPreludePrint(t *testing.T) {
	m := vm.New()
	printFn := m.Global("print")
	if printFn.Kind != runtime.KindFunction {
		t.Fatal("print не функция")
	}
	if _, err := m.Call(printFn, []runtime.Value{runtime.Str("ok")}); err != nil {
		t.Fatalf("print: %v", err)
	}
}
