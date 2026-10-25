package stdlib_test

import (
	"io"
	"os"
	"testing"

	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/parser"
	"github.com/it1ro/brig-lang/internal/sema"
	"github.com/it1ro/brig-lang/internal/vm"
)

// TestLogDefaultHandlerStderr — T-174: обработчик по умолчанию печатает
// `level: msg key=value` в stderr; set_level глушит уровни ниже;
// Telemetry.detach(:log_default) снимает печать (§11.5).
func TestLogDefaultHandlerStderr(t *testing.T) {
	got := captureStderr(t, func() {
		runWithStdlib(t, `module Main
fn main() ->
    Log.info("started", { port: 4000 })
    Log.debug("hidden")
    Log.set_level(:warn)
    Log.info("hidden")
    Log.warn("disk", { free: "1G", pct: 3 })
    Telemetry.detach(:log_default)
    Log.error("hidden")
`)
	})
	want := "info: started port=4000\nwarn: disk free=1G pct=3\n"
	if got != want {
		t.Fatalf("stderr %q, want %q", got, want)
	}
}

func runWithStdlib(t *testing.T, src string) {
	t.Helper()
	prog, err := parser.ParseProgram(parser.ModeModule, src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if r := sema.CheckNames(prog, nil); r.HasErrors() {
		t.Fatalf("sema: %v", r.Diagnostics)
	}
	img, err := compiler.New().Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	m := vm.New()
	if err := compiler.InstallStdlib(m); err != nil {
		t.Fatal(err)
	}
	for name, fn := range img.Functions {
		m.DefineGlobal(name, vm.FuncValue(fn))
	}
	if _, err := m.RunMain(m.Global("main")); err != nil {
		t.Fatalf("run: %v", err)
	}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan []byte)
	go func() {
		b, _ := io.ReadAll(r)
		done <- b
	}()
	old := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = old }()
	fn()
	_ = w.Close()
	b := <-done
	_ = r.Close()
	return string(b)
}
