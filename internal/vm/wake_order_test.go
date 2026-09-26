package vm_test

import (
	"testing"

	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/parser"
	"github.com/it1ro/brig-lang/internal/vm"
)

// TestWakeExpiredDeterministicOrder — T-48 (I-F14): актёры с одинаковым
// after просыпаются в детерминированном порядке (deadline, seq), а не в
// порядке обхода map. 20 прогонов → один и тот же порядок id (§15.4).
func TestWakeExpiredDeterministicOrder(t *testing.T) {
	const src = `module Main
fn waiter(parent, i) ->
    recv
        :never -> :never
    after 20 -> send(parent, i)

fn spawn_all(parent, i, n) ->
    if i == n then () else spawn_next(parent, i, n)

fn spawn_next(parent, i, n) ->
    spawn(() -> waiter(parent, i))
    spawn_all(parent, i + 1, n)

fn collect(k, acc) ->
    if k == 0 then acc else collect(k - 1, recv_one(acc))

fn recv_one(acc) ->
    recv
        i -> (i, acc)

fn main() ->
    spawn_all(self(), 0, 16)
    collect(16, ())
`
	prog, err := parser.ParseProgram(parser.ModeModule, src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	img, err := compiler.New().Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	const want = "(15, (14, (13, (12, (11, (10, (9, (8, (7, (6, (5, (4, (3, (2, (1, (0, ()))))))))))))))))"
	for run := 0; run < 20; run++ {
		m := vm.New()
		for name, fn := range img.Functions {
			m.DefineGlobal(name, vm.FuncValue(fn))
		}
		got, err := m.RunMain(m.Global("main"))
		if err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		if s := got.Inspect(); s != want {
			t.Fatalf("run %d: wake order %s, want %s", run, s, want)
		}
	}
}
