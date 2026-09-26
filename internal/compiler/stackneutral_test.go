package compiler

import (
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/ast"
)

// TestCompileExprStackNeutral — white-box якорь A-F2 / T-32:
// выражение, забывшее releaseToMark, обязано дать ошибку с «I-1».
func TestCompileExprStackNeutral(t *testing.T) {
	c := New()
	fc := c.newFuncCompiler(nil)
	dst := fc.allocReg()

	prev := i1TestLeak
	i1TestLeak = func(fc *funcCompiler) {
		fc.allocReg() // утечка: регистр не освобождён
	}
	t.Cleanup(func() { i1TestLeak = prev })

	err := catchCompile(func() error {
		return fc.compileExpr(ast.NewLiteralExpr("1", 1, 1), val(dst))
	})
	if err == nil {
		t.Fatal("expected compile error for nextReg leak (I-1)")
	}
	if !strings.Contains(err.Error(), "I-1") {
		t.Fatalf("error %q: want mention of I-1", err)
	}
}
