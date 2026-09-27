package compiler_test

import (
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/parser"
	"github.com/it1ro/brig-lang/internal/sema"
	"github.com/it1ro/brig-lang/internal/vm"
)

// src — модуль программы для compileModules: имя и исходник. Первый —
// входной.
type src struct{ name, code string }

// compileModules парсит и компилирует программу из нескольких модулей
// (T-137, §11.1). Путь модуля — имя + ".brig".
func compileModules(t *testing.T, mods ...src) (*compiler.ProgramImage, error) {
	t.Helper()
	in := make([]compiler.Module, 0, len(mods))
	for _, m := range mods {
		prog, err := parser.ParseProgram(parser.ModeModule, m.code)
		if err != nil {
			t.Fatalf("parse %s: %v", m.name, err)
		}
		if r := sema.Check(prog); r.HasErrors() {
			t.Fatalf("sema %s:\n%s", m.name, formatSemaDiagnostics(r))
		}
		in = append(in, compiler.Module{Name: m.name, Path: strings.ToLower(m.name) + ".brig", Prog: prog})
	}
	return compiler.New().CompileProgram(in)
}

// runModules компилирует программу и исполняет main входного модуля.
func runModules(t *testing.T, mods ...src) {
	t.Helper()
	img, err := compileModules(t, mods...)
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
	if _, err := m.RunMain(vm.FuncValue(img.Main)); err != nil {
		t.Fatalf("run: %v", err)
	}
}

// compileModulesErr ждёт ошибку компиляции, содержащую want.
func compileModulesErr(t *testing.T, want string, mods ...src) {
	t.Helper()
	_, err := compileModules(t, mods...)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

// T-137: `Util.f()` из Main вызывает f из util; одноимённые fn двух
// модулей не перезаписывают друг друга; голое имя в модуле — его
// собственная fn, иначе прелюдия (даже если Main её затеняет).
func TestMultiModuleCall(t *testing.T) {
	runModules(t,
		src{"Main", `module Main
import Util
fn f() -> "main"
fn len(xs) -> -1
fn main() ->
    assert(Util.f() == "util")
    assert(f() == "main")
    assert(Util.twice(3) == 6)
    assert(Util.count([1, 2, 3]) == 3)
    assert(len([1]) == -1)
    assert(Util.back() == "main")
`},
		src{"Util", `module Util
import Main
fn f() -> "util"
fn double(x) -> x * 2
fn twice(x) -> double(x)
fn count(xs) -> len(xs)
fn back() -> Main.f()
`},
	)

	t.Run("tail call into another module is TAILCALL", func(t *testing.T) {
		mods := []src{
			{"Main", `module Main
import Util
fn go(n) -> Util.down(n)
fn main() -> assert(go(1000000) == :done)
`},
			{"Util", `module Util
import Main
fn down(n) -> if n == 0 then :done else Main.go(n - 1)
`},
		}
		img, err := compileModules(t, mods...)
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		for _, name := range []string{"go", "Util.down"} {
			fn := img.Functions[name]
			if fn == nil {
				t.Fatalf("no function %q in image", name)
			}
			if !strings.Contains(fn.Disassemble(), "TAILCALL") {
				t.Errorf("%s: no TAILCALL\n%s", name, fn.Disassemble())
			}
		}
		runModules(t, mods...)
	})

	// Модуль программы без import/alias не виден (§11.1). Модуль вне
	// программы — глобал `Mod.f` и ошибка рантайма (ошибка компиляции —
	// T-139).
	t.Run("module without import is not visible", func(t *testing.T) {
		compileModulesErr(t, "3:14: fn main: module Util is not imported",
			src{"Main", "module Main\nimport Other\nfn main() -> Util.f()\n"},
			src{"Other", "module Other\nfn g() -> 1\n"},
			src{"Util", "module Util\nfn f() -> 1\n"},
		)
		img, err := compileModules(t, src{"Main", "module Main\nfn main() -> Nope.f()\n"})
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		if dis := img.Main.Disassemble(); !strings.Contains(dis, "; Nope.f") {
			t.Fatalf("main does not call global Nope.f:\n%s", dis)
		}
	})
}

// T-137, T-122 п.3 (§11.1): записи и варианты другого модуля
// квалифицируются его локальным именем; тип `X` модуля `….X` доступен
// и без квалификации; одноимённые типы двух модулей различны.
func TestMultiModuleTypes(t *testing.T) {
	runModules(t,
		src{"Main", `module Main
import Shapes
import Accounts.User
type Pt { x: Int, y: Int }
type Shape { Circle(Int) }
fn area(s) ->
    match s
        Shapes.Circle(r) -> r * r * 3
        Shapes.Rect(w, h) -> w * h
        Shapes.Empty -> 0
fn main() ->
    assert(area(Shapes.Circle(2)) == 12)
    assert(area(Shapes.Rect(2, 5)) == 10)
    assert(area(Shapes.Empty) == 0)
    assert(area(Shapes.unit()) == 1)
    mine = Circle(2)
    match mine
        Shapes.Circle(_) -> assert(false)
        Circle(r) -> assert(r == 2)
    p = Shapes.Pt{ x: 1, y: 2 }
    assert(p.x == 1)
    assert(Shapes.norm(p) == 3)
    match p
        Pt{ x: _ } -> assert(false)
        Shapes.Pt{ x: x, y: y } -> assert(x + y == 3)
    assert(Pt{ x: 1, y: 2 } != p)
    u = User.new("Ada")
    match u
        User{ name: n } -> assert(n == "Ada")
    v = User{ id: 1, name: "Bob" }
    match v
        Accounts.User.User{ id: i } -> assert(i == 1)
    w = Accounts.User.User{ ..v, id: 2 }
    assert(w.id == 2)
`},
		src{"Shapes", `module Shapes
type Shape { Circle(Int), Rect(Int, Int), Empty }
type Pt { x: Int, y: Int }
fn unit() -> Rect(1, 1)
fn norm(p) ->
    match p
        Pt{ x: x, y: y } -> x + y
`},
		src{"Accounts.User", `module Accounts.User
type User { id: Int, name: Str }
fn new(name) -> User{ id: 0, name: name }
`},
	)

	t.Run("unknown constructor of module", func(t *testing.T) {
		compileModulesErr(t, "Square",
			src{"Main", "module Main\nimport Shapes\nfn main() ->\n    match 1\n        Shapes.Square -> 0\n"},
			src{"Shapes", "module Shapes\ntype Shape { Circle(Int) }\n"},
		)
	})

	t.Run("unknown record type of module", func(t *testing.T) {
		compileModulesErr(t, "неизвестный тип записи Shapes.Box",
			src{"Main", "module Main\nimport Shapes\nfn main() -> Shapes.Box{ x: 1 }\n"},
			src{"Shapes", "module Shapes\ntype Pt { x: Int }\n"},
		)
	})
}

// T-137 (§11.1): `alias Http.Client as Http` — модуль доступен под
// псевдонимом и под полным именем; два одинаковых локальных имени
// модуля в одном файле — ошибка.
func TestMultiModuleAlias(t *testing.T) {
	runModules(t,
		src{"Main", `module Main
alias Http.Client as Http
alias Json as J
fn main() ->
    assert(Http.get("x") == "GET x")
    assert(Http.Client.get("y") == "GET y")
    assert(J.encode([1]) == "[1]")
`},
		src{"Http.Client", `module Http.Client
fn get(url) -> "GET \(url)"
`},
	)

	t.Run("import makes the last segment a local name", func(t *testing.T) {
		runModules(t,
			src{"Main", "module Main\nimport Http.Client\nfn main() -> assert(Client.get(\"z\") == \"GET z\")\n"},
			src{"Http.Client", "module Http.Client\nfn get(url) -> \"GET \\(url)\"\n"},
		)
	})

	t.Run("duplicate local module name", func(t *testing.T) {
		compileModulesErr(t, "3:1: module name Client already refers to A.Client",
			src{"Main", "module Main\nimport A.Client\nimport B.Client\nfn main() -> 0\n"},
			src{"A.Client", "module A.Client\nfn f() -> 1\n"},
			src{"B.Client", "module B.Client\nfn f() -> 2\n"},
		)
	})
}

// T-137, G-10 (§7.5): `xs |> Util.f(a)` — вызов `Util.f(xs, a)`;
// неизвестный модуль в pipe назван в ошибке.
func TestMultiModulePipe(t *testing.T) {
	runModules(t,
		src{"Main", `module Main
import Util
alias Http.Client as Http
fn main() ->
    assert(([1, 2] |> Util.add_all(10)) == [11, 12])
    assert((3 |> Util.inc) == 4)
    assert(("u" |> Http.Client.get) == "GET u")
    assert(("v" |> Http.get) == "GET v")
    assert((5 |> Util.Box) == Util.Box(5))
`},
		src{"Util", `module Util
type Wrap { Box(Int) }
fn add_all(xs, n) -> map(xs, (x) -> x + n)
fn inc(x) -> x + 1
`},
		src{"Http.Client", `module Http.Client
fn get(url) -> "GET \(url)"
`},
	)

	t.Run("unknown module", func(t *testing.T) {
		compileModulesErr(t, "3:21: fn main: unknown module List",
			src{"Main", "module Main\n\nfn main() -> [1] |> List.each(print)\n"},
		)
	})
}
