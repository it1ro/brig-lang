package compiler_test

import (
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/parser"
	"github.com/it1ro/brig-lang/internal/sema"
)

// TestVerifyAF7RunModuleSkipsSema confirms A-F7 point (3): helper runModule
// (compiler_test.go) goes parse→compile→RunMain and never calls sema.Check,
// so a program that CLI `brig check`/`brig run` rejects still executes here.
//
// Verified by T-14 (#12). Follow-up: wire sema into runModule (separate issue).
func TestVerifyAF7RunModuleSkipsSema(t *testing.T) {
	const src = `module Main
fn main() ->
    print(trap(1 + 1))
`

	prog, err := parser.ParseProgram(parser.ModeModule, src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	semaRes := sema.Check(prog)
	if !semaRes.HasErrors() {
		t.Fatal("expected sema to reject trap in argument position; got no errors")
	}
	found := false
	for _, d := range semaRes.Diagnostics {
		if strings.Contains(d.Message, "trap is not allowed") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected diagnostic mentioning trap; got: %v", semaRes.Diagnostics)
	}

	// Same source compiles and runs without sema — the A-F7 gap.
	runModule(t, src)
}
