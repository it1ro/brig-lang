package stdlib_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/it1ro/brig-lang/internal/ast"
	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/examples"
	"github.com/it1ro/brig-lang/internal/parser"
	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/sema"
	"github.com/it1ro/brig-lang/internal/vm"
	"github.com/it1ro/brig-lang/stdlib"
)

// T-146: встроенные модули разбираются, проходят sema и компилируются
// из встроенного в бинарник исходника.
func TestStdlibEmbeddedLoads(t *testing.T) {
	ms, err := stdlib.Modules()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	world := make([]sema.Module, len(ms))
	for i, m := range ms {
		names = append(names, m.Name)
		world[i] = sema.Module{Name: m.Name, Prog: m.Prog}
		if !stdlib.IsModule(m.Name) || m.Src == "" || !strings.HasPrefix(m.Path, "stdlib/") {
			t.Fatalf("module %+v", m)
		}
	}
	if got := strings.Join(names, " "); got != "List Option Result Server Supervisor Observer Behavior" {
		t.Fatalf("modules %q", got)
	}
	w := sema.NewWorld(world)
	for _, m := range ms {
		for _, d := range sema.CheckNames(m.Prog, w).Diagnostics {
			if d.Severity == sema.SeverityError {
				t.Errorf("%s:%d:%d: %s", m.Path, d.Line, d.Col, d.Message)
			}
		}
	}
	img, err := compiler.StdlibImage()
	if err != nil {
		t.Fatal(err)
	}
	for _, fn := range []string{"List.each", "List.sort", "Option.and_then", "Result.all"} {
		if img.Functions[fn] == nil {
			t.Errorf("image has no %s", fn)
		}
	}
	if img.Main != nil {
		t.Error("stdlib image has main")
	}
}

// Каждая pub fn задокументирована `##` с хотя бы одним доктестом.
func TestStdlibPubFnsHaveDoctests(t *testing.T) {
	for _, m := range stdlib.MustModules() {
		lines := strings.Split(m.Src, "\n")
		seen := map[string]bool{}
		for _, d := range m.Prog.Decls {
			fd, ok := d.(ast.FuncDecl)
			if !ok || !fd.IsPub() || seen[fd.FnName()] {
				continue
			}
			seen[fd.FnName()] = true
			line := fd.Pos()
			var doc []string
			for i := line - 2; i >= 0 && strings.HasPrefix(lines[i], "##"); i-- {
				doc = append(doc, lines[i])
			}
			text := strings.Join(doc, "\n")
			if !strings.Contains(text, "```brig repl") || !strings.Contains(text, "## > ") {
				t.Errorf("%s.%s: нет ## с доктестом", m.Name, fd.FnName())
			}
		}
		if len(seen) == 0 {
			t.Errorf("%s: нет pub fn", m.Name)
		}
	}
}

// Доктесты модулей — на встроенном исходнике, без файлов на диске.
func TestStdlibDoctests(t *testing.T) {
	newVM := func() *vm.VM {
		m := vm.New()
		if err := compiler.InstallStdlib(m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	for _, m := range stdlib.MustModules() {
		rs := examples.Doctests(m.Path, m.Src, newVM, nil)
		if len(rs) == 0 {
			t.Errorf("%s: нет доктестов", m.Path)
		}
		for _, r := range rs {
			if !r.OK {
				t.Errorf("%s:%d: %s", r.File, r.Line, r.ErrMsg)
			}
		}
	}
}

// Образ stdlib один на процесс: несколько ВМ исполняют его чанки
// одновременно (под -race — проверка, что исполнение их не меняет).
func TestStdlibSharedAcrossVMs(t *testing.T) {
	const src = `module Main

fn main() ->
    xs = List.sort([5, 3, 9, 1, 7])
    List.each(xs, x -> x)
    [Ok(List.reverse(xs)), Ok(List.take(xs, 2))] |> Result.all()
`
	prog, err := parser.ParseProgram(parser.ModeModule, src)
	if err != nil {
		t.Fatal(err)
	}
	img, err := compiler.New().Compile(prog)
	if err != nil {
		t.Fatal(err)
	}
	want := runtime.Variant("Ok", runtime.List(
		runtime.List(runtime.Int(9), runtime.Int(7), runtime.Int(5), runtime.Int(3), runtime.Int(1)),
		runtime.List(runtime.Int(1), runtime.Int(3))))
	var wg sync.WaitGroup
	errs := make(chan string, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m := vm.New()
			if err := compiler.InstallStdlib(m); err != nil {
				errs <- err.Error()
				return
			}
			for name, fn := range img.Functions {
				m.DefineGlobal(name, vm.FuncValue(fn))
			}
			got, err := m.RunMain(m.Global("main"))
			if err != nil {
				errs <- err.Error()
				return
			}
			if !runtime.Equal(got, want) {
				errs <- got.Inspect()
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}
