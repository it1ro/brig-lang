package vm_test

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/parser"
	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/vm"
)

// TestYieldUntilRunsOtherActors — натив на горутине цикла отпускает
// планировщик: соседний актор крутится, пока YieldUntil ждёт.
func TestYieldUntilRunsOtherActors(t *testing.T) {
	prog, err := parser.ParseProgram(parser.ModeModule, `module Main
fn spin() ->
    tick()
    spin()

fn main() ->
    hold()
`)
	if err != nil {
		t.Fatal(err)
	}
	img, err := compiler.New().Compile(prog)
	if err != nil {
		t.Fatal(err)
	}
	m := vm.New()
	if err := m.StartSession(); err != nil {
		t.Fatal(err)
	}
	defer m.CloseSession()

	var n atomic.Int64
	done := make(chan struct{})
	tick := runtime.Func(&runtime.FuncValue{
		Name: "tick", Arity: 0, IsNative: true,
		Native: func(runtime.Caller, []runtime.Value) (runtime.Value, error) {
			n.Add(1)
			return runtime.Unit, nil
		},
	})
	hold := runtime.Func(&runtime.FuncValue{
		Name: "hold", Arity: 0, IsNative: true,
		Native: func(c runtime.Caller, _ []runtime.Value) (runtime.Value, error) {
			sch := c.(*vm.VM).Scheduler()
			if _, err := sch.Spawn(m.Global("spin"), nil); err != nil {
				return runtime.Unit, err
			}
			return runtime.Unit, sch.YieldUntil(done, nil, nil)
		},
	})
	if err := m.Scheduler().Sync(func() error {
		for name, fn := range img.Functions {
			m.DefineGlobal(name, vm.FuncValue(fn))
		}
		m.DefineGlobal("tick", tick)
		m.DefineGlobal("hold", hold)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	go func() {
		deadline := time.Now().Add(2 * time.Second)
		for n.Load() == 0 {
			if time.Now().After(deadline) {
				close(done)
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		close(done)
	}()
	if _, err := m.SessionEval(m.Global("main"), nil, nil); err != nil {
		t.Fatal(err)
	}
	if n.Load() == 0 {
		t.Fatal("actor did not run while YieldUntil waited")
	}
}
