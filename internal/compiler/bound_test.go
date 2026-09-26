package compiler

import (
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/vm"
)

// TestEmitRejectsWriteToBoundReg — white-box якорь A-F2 / T-33 / I-3:
// emit обязан fail'ить запись в bound-регистр через операнд A
// (MATCHLOCAL — исключение: пишет слоты паттерна неявно).
func TestEmitRejectsWriteToBoundReg(t *testing.T) {
	c := New()
	fc := c.newFuncCompiler(nil)
	r := fc.allocReg()
	fc.bindLocal("x", r)

	t.Run("MOVE into bound", func(t *testing.T) {
		src := fc.allocReg()
		err := catchCompile(func() error {
			fc.emit(vm.ABC(vm.MOVE, r, src, 0))
			return nil
		})
		if err == nil {
			t.Fatal("expected compile error for write to bound register")
		}
		if !strings.Contains(err.Error(), "I-3") {
			t.Fatalf("error %q: want mention of I-3", err)
		}
	})

	t.Run("MATCHLOCAL on bound allowed", func(t *testing.T) {
		err := catchCompile(func() error {
			fc.emit(vm.ABx(vm.MATCHLOCAL, r, 0))
			return nil
		})
		if err != nil {
			t.Fatalf("MATCHLOCAL on bound reg must be allowed: %v", err)
		}
	})

	t.Run("write to unbound allowed", func(t *testing.T) {
		tmp := fc.allocReg()
		err := catchCompile(func() error {
			fc.emit(vm.ABx(vm.LOADK, tmp, 0))
			return nil
		})
		if err != nil {
			t.Fatalf("write to unbound reg must be allowed: %v", err)
		}
	})
}
