package vm_test

import (
	"testing"
	"time"

	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/parser"
	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/vm"
)

// runModuleResult — parse → compile → RunMain, возвращает значение main.
func runModuleResult(t *testing.T, src string) runtime.Value {
	t.Helper()
	prog, err := parser.ParseProgram(parser.ModeModule, src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	img, err := compiler.New().Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if img.Main == nil {
		t.Fatal("no main")
	}
	m := vm.New()
	for name, fn := range img.Functions {
		m.DefineGlobal(name, vm.FuncValue(fn))
	}
	got, err := m.RunMain(m.Global("main"))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return got
}

// TestVerifyIF14HugeTimerMs confirms I-F14 (RECVTIMER ms overflow).
//
// RECVTIMER does `time.Duration(ms) * time.Millisecond` with no range check
// (scheduler.go). MaxInt64 overflows to −1ms, so
// `after 9223372036854775807` wakes immediately instead of waiting ~292 Myr.
// Follow-up fix: validate ms / reject out-of-range (Wave 3 issue from T-15).
func TestVerifyIF14HugeTimerMs(t *testing.T) {
	const src = `module Main
fn main() ->
    recv
        :never -> :never
    after 9223372036854775807 -> :overflowed
`
	start := time.Now()
	got := runModuleResult(t, src)
	elapsed := time.Since(start)
	if elapsed > time.Second {
		t.Fatalf("after MaxInt64 took %v; expected immediate Duration overflow wake", elapsed)
	}
	if got.Kind != runtime.KindAtom || got.Atom != "overflowed" {
		t.Fatalf("got %s, want :overflowed (silent ms→Duration overflow)", got.Inspect())
	}
}
