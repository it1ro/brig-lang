package vm_test

import (
	"strings"
	"testing"
	"time"

	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/parser"
	"github.com/it1ro/brig-lang/internal/vm"
)

// TestVerifyIF14HugeTimerMs — T-47: MaxInt64 ms must not overflow to −1ms
// and wake immediately; RECVTIMER rejects it as (:type_error, (:after, ...)).
func TestVerifyIF14HugeTimerMs(t *testing.T) {
	const src = `module Main
fn main() ->
    recv
        :never -> :never
    after 9223372036854775807 -> :overflowed
`
	prog, err := parser.ParseProgram(parser.ModeModule, src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	img, err := compiler.New().Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	m := vm.New()
	for name, fn := range img.Functions {
		m.DefineGlobal(name, vm.FuncValue(fn))
	}
	start := time.Now()
	_, err = m.RunMain(m.Global("main"))
	elapsed := time.Since(start)
	if elapsed > time.Second {
		t.Fatalf("after MaxInt64 took %v; expected immediate type_error reject", elapsed)
	}
	if err == nil {
		t.Fatal("want type_error for MaxInt64 ms, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "type_error") || !strings.Contains(msg, "after") {
		t.Fatalf("got %q, want (:type_error, (:after, ...))", msg)
	}
}
