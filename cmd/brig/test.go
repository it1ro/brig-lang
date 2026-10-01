package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/it1ro/brig-lang/internal/ast"
	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/examples"
	"github.com/it1ro/brig-lang/internal/loader"
	"github.com/it1ro/brig-lang/internal/repl"
	"github.com/it1ro/brig-lang/internal/vm"
)

// testTally — итог прогона `brig test`.
type testTally struct{ passed, failed int }

func (t *testTally) ok(name string) {
	t.passed++
	fmt.Printf("ok   %s\n", name)
}

func (t *testTally) fail(name string, err error) {
	t.failed++
	fmt.Fprintf(os.Stderr, "FAIL %s\n     %v\n", name, err)
}

// runTest: brig test [path] — тест-раннер (T-125, вариант A; T-147).
//
// Соглашение: под path (файл или каталог, по умолчанию «.») каждый
// `*_test.brig` — тестовый файл, его top-level `fn test_*()` без
// параметров — тесты; тесты, зарегистрированные в них через Test.it,
// прогоняются следом. В каждом `.brig` исполняются доктесты — блоки
// ```brig repl в doc-комментариях `##`. Тест падает на непойманном raise.
// Exit 0 — всё прошло; 1 — упал тест, доктест или файл не скомпилировался.
func runTest(args []string) {
	if len(args) > 1 {
		fmt.Fprintln(os.Stderr, "brig test: at most one path expected")
		os.Exit(exitParse)
	}
	root := "."
	if len(args) == 1 {
		root = args[0]
	}
	files, err := brigFiles(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "brig test: %v\n", err)
		os.Exit(exitParse)
	}
	compiler.Verify = os.Getenv("BRIG_VERIFY") == "1"

	var t testTally
	for _, f := range files {
		testFile(f, &t)
	}
	fmt.Printf("\n%d passed, %d failed\n", t.passed, t.failed)
	if t.failed > 0 {
		os.Exit(1)
	}
}

// brigFiles — `.brig`-файлы под root в лексикографическом порядке.
func brigFiles(root string) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []string{root}, nil
	}
	var files []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".brig") {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}

// testFile прогоняет тесты и доктесты одного файла. Файл без тестов и
// доктестов не компилируется: `brig test` не проверяет остальной код.
func testFile(path string, t *testTally) {
	data, err := os.ReadFile(path)
	if err != nil {
		t.fail(path, err)
		return
	}
	src := string(data)
	isTest := strings.HasSuffix(path, "_test.brig")
	if !isTest && !strings.Contains(src, "##") {
		return
	}

	prog, mods, img, ok := loadTestModule(path)
	if !ok {
		t.fail(path, fmt.Errorf("file did not compile"))
		return
	}
	newVM := func() *vm.VM {
		m := newMachine()
		if err := compiler.InstallStdlib(m); err != nil {
			fail(exitInternal, "brig: %v", err)
		}
		for name, fn := range img.Functions {
			m.DefineGlobal(name, vm.FuncValue(fn))
		}
		return m
	}

	if isTest {
		for _, name := range testFuncs(prog, img) {
			m := newVM()
			full := path + ": " + name
			if _, err := m.RunMain(m.Global(name)); err != nil {
				t.fail(full, err)
			} else {
				t.ok(full)
			}
			p, f := m.RunTests()
			t.passed += p
			t.failed += f
		}
	}

	entry := &repl.Entry{Mods: mods, Image: img}
	for _, r := range examples.Doctests(path, src, newVM, entry) {
		name := fmt.Sprintf("%s:%d: doctest", r.File, r.Line)
		if r.OK {
			t.ok(name)
		} else {
			t.fail(name, fmt.Errorf("%s", r.ErrMsg))
		}
	}
}

// loadTestModule: программа от файла path — загрузчик модулей
// (корень — как у brig check, loader.ModuleRoot), sema по всем модулям
// графа и compiler.CompileProgram. Возвращает AST файла, модули
// программы (входной — первый) и образ. Диагностики — в stderr.
func loadTestModule(path string) (*ast.Program, []compiler.Module, *compiler.ProgramImage, bool) {
	g, err := loader.LoadFrom(loader.ModuleRoot(path), path)
	if err != nil {
		var le *loader.Error
		if errors.As(err, &le) {
			fmt.Fprintf(os.Stderr, "error: %v\n", le)
		} else {
			reportCompileError(path, err)
		}
		return nil, nil, nil, false
	}
	if !checkGraph(g) {
		return nil, nil, nil, false
	}
	mods := compileModules(g)
	img, err := compiler.New().CompileProgram(mods)
	if err != nil {
		reportCompileError(path, err)
		return nil, nil, nil, false
	}
	return g.Entry.Prog, mods, img, true
}

// testFuncs — имена top-level `fn test_*()` без параметров в порядке
// объявления.
func testFuncs(prog *ast.Program, img *compiler.ProgramImage) []string {
	var names []string
	seen := map[string]bool{}
	for _, d := range prog.Decls {
		fd, ok := d.(ast.FuncDecl)
		if !ok || !strings.HasPrefix(fd.FnName(), "test_") || seen[fd.FnName()] {
			continue
		}
		seen[fd.FnName()] = true
		if fn := img.Functions[fd.FnName()]; fn != nil && fn.Arity == 0 {
			names = append(names, fd.FnName())
		}
	}
	return names
}
