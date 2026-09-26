package compiler

import (
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/ast"
)

// TestCompilerRejectsTailCallUnderTrap — white-box якорь A-F2 / T-31:
// при trapDepth>0 и d.tail компилятор обязан fail'ить до эмита TAILCALL.
func TestCompilerRejectsTailCallUnderTrap(t *testing.T) {
	t.Run("compileGenericCall", func(t *testing.T) {
		c := New()
		fc := c.newFuncCompiler(nil)
		fc.trapDepth = 1
		call := ast.NewCallExpr(ast.NewLiteralExpr("1", 1, 1), nil, 1, 1).(ast.CallExpr)

		err := catchCompile(func() error {
			return fc.compileGenericCall(call, dest{reg: 0, tail: true})
		})
		if err == nil {
			t.Fatal("expected compile error for TAILCALL under trapDepth>0")
		}
		if !strings.Contains(err.Error(), "TAILCALL") {
			t.Fatalf("error %q: want mention of TAILCALL", err)
		}
	})

	t.Run("compileGlobalCall", func(t *testing.T) {
		c := New()
		fc := c.newFuncCompiler(nil)
		fc.trapDepth = 1
		pos := ast.NewLiteralExpr("1", 1, 1)

		err := catchCompile(func() error {
			return fc.compileGlobalCall("print", nil, dest{reg: 0, tail: true}, pos)
		})
		if err == nil {
			t.Fatal("expected compile error for TAILCALL under trapDepth>0")
		}
		if !strings.Contains(err.Error(), "TAILCALL") {
			t.Fatalf("error %q: want mention of TAILCALL", err)
		}
	})
}

func catchCompile(fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			if ce, ok := r.(compileError); ok {
				err = ce
				return
			}
			panic(r)
		}
	}()
	return fn()
}
