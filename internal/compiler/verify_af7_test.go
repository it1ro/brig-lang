package compiler_test

import (
	"strings"
	"testing"
)

// TestVerifyAF7RunModuleRejectsTrapInArg — A-F7 закрыт (T-46): runModule /
// runModuleErr вызывают sema.Check и отвергают trap в позиции аргумента.
func TestVerifyAF7RunModuleRejectsTrapInArg(t *testing.T) {
	err := runModuleErr(t, `module Main
fn main() ->
    print(trap(1 + 1))
`)
	if err == nil {
		t.Fatal("expected runModuleErr to reject trap in argument position")
	}
	if !strings.Contains(err.Error(), "trap is not allowed") {
		t.Fatalf("expected diagnostic mentioning trap; got: %v", err)
	}
}

// TestRunModuleAllowsTrapAsLetRHS — trap в легальной позиции (RHS let) проходит.
func TestRunModuleAllowsTrapAsLetRHS(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    x = trap(1 + 1)
    print(x)
`)
}
