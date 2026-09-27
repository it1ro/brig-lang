//go:build heapbench

// Package benchheap — стенд замеров Z1–Z4 для решения о модели heap
// (web-mvp-research/11-memory.md, 23-heap-measurements.md). Не входит в
// обычный `go test ./...`: запуск — `go test -tags heapbench -v ./web-mvp-research/bench-heap/`.
package benchheap

import (
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"testing"
	"time"

	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/parser"
	brt "github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/sema"
	"github.com/it1ro/brig-lang/internal/vm"
)

// natives — функции стенда, видимые программе как глобальные имена.
type natives map[string]brt.NativeFunc

// runBrig компилирует и запускает программу из testdata/<name>.brig с
// дополнительными нативными глобалами и аргументами main.
func runBrig(t testing.TB, name string, ns natives, args ...string) brt.Value {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("testdata", name+".brig"))
	if err != nil {
		t.Fatal(err)
	}
	prog, err := parser.ParseProgram(parser.ModeModule, string(src))
	if err != nil {
		t.Fatalf("%s: parse: %v", name, err)
	}
	if res := sema.Check(prog); res.HasErrors() {
		t.Fatalf("%s: sema: %v", name, res.Diagnostics)
	}
	img, err := compiler.New().Compile(prog)
	if err != nil {
		t.Fatalf("%s: compile: %v", name, err)
	}
	m := vm.New()
	m.SetArgs(args)
	for n, fn := range img.Functions {
		m.DefineGlobal(n, vm.FuncValue(fn))
	}
	for n, f := range ns {
		m.DefineGlobal(n, brt.Func(&brt.FuncValue{Name: n, Arity: -1, IsNative: true, Native: f}))
	}
	v, err := m.RunMain(m.Global("main"))
	if err != nil {
		t.Fatalf("%s: run: %v", name, err)
	}
	return v
}

// liveHeap — байты живого heap после двух полных GC.
func liveHeap() uint64 {
	runtime.GC()
	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return ms.HeapAlloc
}

// clockNative — clock_ns(): монотонное время в наносекундах.
func clockNative(start time.Time) brt.NativeFunc {
	return func(_ brt.Caller, _ []brt.Value) (brt.Value, error) {
		return brt.Int(int64(time.Since(start))), nil
	}
}

// profDir — куда писать pprof-профили (`-args -prof /path`); пусто — не писать.
var profDir = flag.String("prof", "", "каталог для pprof-профилей")

func writeHeapProfile(t testing.TB, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := pprof.WriteHeapProfile(f); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}
