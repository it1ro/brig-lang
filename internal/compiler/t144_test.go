package compiler_test

import (
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/parser"
	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/sema"
	"github.com/it1ro/brig-lang/internal/vm"
)

// T-144 (§7.6): `M.f` без вызова — значение-функция модуля: через
// import, alias и полное имя, в аргументе, в связывании и в pipe.
func TestModuleFunctionAsValue(t *testing.T) {
	runModules(t,
		src{"Main", `module Main
import Util
alias Http.Client as Http
fn main() ->
    g = Util.g
    assert(g(1) == 2)
    assert(map([1, 2], Util.g) == [2, 3])
    assert(fold([1, 2, 3], 0, Util.add) == 6)
    assert(Util.apply(Util.g, 5) == 6)
    get = Http.get
    assert(get("x") == "GET x")
    assert(map(["y"], Http.Client.get) == ["GET y"])
    assert((Util.g |> Util.apply(1)) == 2)
`},
		src{"Util", `module Util
pub fn g(x) -> x + 1
pub fn add(a, b) -> a + b
pub fn apply(f, x) -> f(x)
`},
		src{"Http.Client", `module Http.Client
pub fn get(url) -> "GET \(url)"
`},
	)

	t.Run("entry module function by module name", func(t *testing.T) {
		runModules(t,
			src{"Main", `module Main
import Util
fn twice(x) -> x * 2
fn main() -> assert(Util.run() == [6])
`},
			src{"Util", `module Util
import Main
pub fn run() -> Prelude.map([3], Main.twice)
`},
		)
	})

	// sema.Check отвергает это раньше; компилятор — без прохода sema.
	t.Run("actor primitive is not a value", func(t *testing.T) {
		prog, err := parser.ParseProgram(parser.ModeModule, "module Main\n\nfn main() ->\n    f = Prelude.send\n    f\n")
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		_, err = compiler.New().Compile(prog)
		want := "4:9: fn main: actor primitive Prelude.send cannot be used as a value (§12.6)"
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("err = %v, want %q", err, want)
		}
	})

	t.Run("module without import", func(t *testing.T) {
		compileModulesErr(t, "3:14: fn main: module Util is not imported",
			src{"Main", "module Main\nimport Other\nfn main() -> Util.f\n"},
			src{"Other", "module Other\nfn g() -> 1\n"},
			src{"Util", "module Util\nfn f() -> 1\n"},
		)
	})
}

// T-144 (§7.6, §4): identity функции модуля — полное имя и арность, а не
// адрес; одноимённые функции и конструкторы разных модулей различны.
func TestModuleFunctionIdentity(t *testing.T) {
	runModules(t,
		src{"Main", `module Main
import Util
import Other
type T { Box(Int) }
fn g(x) -> x
fn main() ->
    assert(Util.g == Util.g)
    f = Util.g
    assert(f == Util.g)
    assert(Util.g != Util.h)
    assert(Util.g != Other.g)
    assert(Util.g != g)
    assert(g == g)
    assert(Util.Box != Box)
    assert(Util.Box == Util.Box)
    assert(Json.encode == Json.encode)
    assert(Prelude.map == map)
    assert([Util.g, Util.h] == [Util.g, Util.h])
`},
		src{"Util", `module Util
type T { Box(Int) }
pub fn g(x) -> x
pub fn h(x) -> x
`},
		src{"Other", `module Other
pub fn g(x) -> x
`},
	)

	// Два разных значения FuncValue с одним именем и арностью — одна
	// функция (переопределение в REPL, recompile()).
	a := runtime.Func(&runtime.FuncValue{Name: "Util.g", Arity: 1})
	b := runtime.Func(&runtime.FuncValue{Name: "Util.g", Arity: 1})
	c := runtime.Func(&runtime.FuncValue{Name: "Util.g", Arity: 2})
	if !runtime.Equal(a, b) {
		t.Fatal("Util.g/1 != Util.g/1 at different addresses")
	}
	if runtime.Equal(a, c) {
		t.Fatal("Util.g/1 == Util.g/2")
	}

	// Лямбды — замыкания: одинаковое тело не делает их равными.
	runModule(t, `fn main() ->
    f = x -> x
    g = x -> x
    assert(f != g)
    assert(f == f)
`)
}

// T-144: функция встроенного модуля — Go-нативного и stdlib на Brig —
// как значение.
func TestPreludeModuleFunctionAsValue(t *testing.T) {
	src := `module Main
alias Json as J
fn main() ->
    f = Json.encode
    assert(f([1]) == "[1]")
    assert(map([[1], [2]], J.encode) == ["[1]", "[2]"])
    assert(List.map([[3]], Json.encode) == ["[3]"])
    rev = List.reverse
    assert(rev([1, 2]) == [2, 1])
    assert(map([[2, 1]], List.reverse) == [[1, 2]])
    assert(List.reverse == List.reverse)
    assert(len == Prelude.len)
`
	prog, err := parser.ParseProgram(parser.ModeModule, src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if r := sema.CheckNames(prog, nil); r.HasErrors() {
		t.Fatalf("sema:\n%s", formatSemaDiagnostics(r))
	}
	img, err := compiler.New().Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	m := vm.New()
	if err := compiler.InstallStdlib(m); err != nil {
		t.Fatalf("stdlib: %v", err)
	}
	for name, fn := range img.Functions {
		m.DefineGlobal(name, vm.FuncValue(fn))
	}
	if _, err := m.RunMain(m.Global("main")); err != nil {
		t.Fatalf("run: %v", err)
	}
}
