package vm_test

import (
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/parser"
	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/vm"
)

// compileFns компилирует src и отдаёт функции образа: один и тот же *Chunk
// можно раздать нескольким VM (так делает и newCallAllocRun).
func compileFns(tb testing.TB, src string) map[string]*vm.Function {
	tb.Helper()
	prog, err := parser.ParseProgram(parser.ModeModule, src)
	if err != nil {
		tb.Fatalf("parse: %v", err)
	}
	img, err := compiler.New().Compile(prog)
	if err != nil {
		tb.Fatalf("compile: %v", err)
	}
	return img.Functions
}

const probeSrc = `module Main
fn main() -> probe()
`

func probeNative(v runtime.Value) runtime.Value {
	return runtime.Func(&runtime.FuncValue{Name: "probe", Arity: 0, IsNative: true,
		Native: func(runtime.Caller, []runtime.Value) (runtime.Value, error) { return v, nil }})
}

// TestGlobalCellsPerVM — кэш ячеек глобалов в чанке (T-276) принадлежит одной
// VM: один образ, исполненный двумя VM, у каждой читает её собственный
// глобал, и порядок прогонов на это не влияет.
func TestGlobalCellsPerVM(t *testing.T) {
	fns := compileFns(t, probeSrc)
	newVM := func(n int64) *vm.VM {
		m := vm.New()
		for name, fn := range fns {
			m.DefineGlobal(name, vm.FuncValue(fn))
		}
		m.DefineGlobal("probe", probeNative(runtime.Int(n)))
		return m
	}
	m1, m2 := newVM(1), newVM(2)
	for i, step := range []struct {
		m    *vm.VM
		want int64
	}{{m1, 1}, {m2, 2}, {m1, 1}, {m2, 2}} {
		got, err := step.m.RunMain(step.m.Global("main"))
		if err != nil {
			t.Fatalf("прогон %d: %v", i, err)
		}
		if !runtime.Equal(got, runtime.Int(step.want)) {
			t.Fatalf("прогон %d: main() = %s, want %d", i, got.Inspect(), step.want)
		}
	}
}

// TestGlobalCellRedefine — переопределение глобала (REPL, §11.4) видно
// чанку, который уже закэшировал ячейку этого имени, а снятие имени
// возвращает чанк к ошибке `undefined`.
func TestGlobalCellRedefine(t *testing.T) {
	fns := compileFns(t, probeSrc)
	m := vm.New()
	for name, fn := range fns {
		m.DefineGlobal(name, vm.FuncValue(fn))
	}
	if err := m.StartSession(); err != nil {
		t.Fatal(err)
	}
	defer m.CloseSession()

	eval := func() (runtime.Value, error) { return m.SessionEval(m.Global("main"), nil, nil) }

	if _, err := eval(); err == nil || !strings.Contains(err.Error(), "undefined: probe") {
		t.Fatalf("без probe: err = %v, want undefined: probe", err)
	}
	if err := m.SessionRedefine(map[string]runtime.Value{"probe": probeNative(runtime.Int(1))}, nil); err != nil {
		t.Fatal(err)
	}
	got, err := eval()
	if err != nil {
		t.Fatalf("после define: %v", err)
	}
	if !runtime.Equal(got, runtime.Int(1)) {
		t.Fatalf("после define: main() = %s, want 1", got.Inspect())
	}
	if err := m.SessionRedefine(map[string]runtime.Value{"probe": probeNative(runtime.Int(2))}, nil); err != nil {
		t.Fatal(err)
	}
	if got, err = eval(); err != nil {
		t.Fatalf("после redefine: %v", err)
	}
	if !runtime.Equal(got, runtime.Int(2)) {
		t.Fatalf("после redefine: main() = %s, want 2", got.Inspect())
	}
	if err := m.SessionRedefine(nil, []string{"probe"}); err != nil {
		t.Fatal(err)
	}
	if _, err = eval(); err == nil || !strings.Contains(err.Error(), "undefined: probe") {
		t.Fatalf("после undef: err = %v, want undefined: probe", err)
	}
	if v := m.Global("probe"); v.Kind != runtime.KindUnit {
		t.Fatalf("Global после undef = %s, want Unit", v.Inspect())
	}
}
