package compiler_test

import (
	"testing"

	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/parser"
	"github.com/it1ro/brig-lang/internal/vm"
)

func runWithArgs(t *testing.T, src string, args []string) {
	t.Helper()
	prog, err := parser.ParseProgram(parser.ModeModule, src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	img, err := compiler.New().Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	m := vm.New()
	m.SetArgs(args)
	for name, fn := range img.Functions {
		m.DefineGlobal(name, vm.FuncValue(fn))
	}
	if _, err := m.RunMain(m.Global("main")); err != nil {
		t.Fatalf("run: %v", err)
	}
}

// T-75, §16 Must: Sys.args() возвращает аргументы программы списком строк.
func TestSysArgs(t *testing.T) {
	runWithArgs(t, `module Main
fn main() ->
    assert(Sys.args() == ["a", "b"])
`, []string{"a", "b"})
	runWithArgs(t, `module Main
fn main() ->
    assert(Sys.args() == [])
`, nil)
}

// T-75, §11.5: Prelude.<name> вызывает функцию прелюдии даже при затенении.
func TestPreludeModuleUnderShadowing(t *testing.T) {
	runModule(t, `module Main
import Prelude
fn len(x) -> 42
fn main() ->
    assert(len([1]) == 42)
    assert(Prelude.len([1]) == 1)
    assert(Prelude.len([1, 2]) == 2)
`)
}

// T-170, §11.5: fn модуля с именем акторного примитива его затеняет,
// сам примитив доступен как Prelude.<name> и компилируется в тот же опкод.
func TestPreludeActorPrimitiveUnderShadowing(t *testing.T) {
	runModule(t, `module Main
fn send(x) -> (:mine, x)
fn main() ->
    assert(send(1) == (:mine, 1))
    assert(Prelude.send(Prelude.self(), :hi) == Ok(()))
    ref = Prelude.make_ref()
    Prelude.reply(self(), ref, 42)
    assert(Prelude.await(ref, 0) == Ok(42))
    recv
        m -> assert(m == :hi)
`)
}
