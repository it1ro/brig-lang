package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/it1ro/brig-lang/internal/ast"
	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/examples"
	"github.com/it1ro/brig-lang/internal/parser"
	"github.com/it1ro/brig-lang/internal/sema"
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
		fmt.Fprintln(os.Stderr, "brig test: ожидается не больше одного пути")
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

	prog, img, ok := loadTestModule(path, src)
	if !ok {
		t.fail(path, fmt.Errorf("файл не скомпилирован"))
		return
	}
	newVM := func() *vm.VM {
		m := vm.New()
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

	for _, r := range examples.Doctests(path, src, newVM) {
		name := fmt.Sprintf("%s:%d: doctest", r.File, r.Line)
		if r.OK {
			t.ok(name)
		} else {
			t.fail(name, fmt.Errorf("%s", r.ErrMsg))
		}
	}
}

// loadTestModule: парсер, sema и компилятор; диагностики — в stderr.
func loadTestModule(path, src string) (*ast.Program, *compiler.ProgramImage, bool) {
	prog, err := parser.ParseProgram(parser.ModeModule, src)
	if err != nil {
		reportCompileError(path, err)
		return nil, nil, false
	}
	semaRes := sema.Check(prog)
	reportDiagnostics(path, semaRes)
	if semaRes.HasErrors() {
		return nil, nil, false
	}
	img, err := compiler.New().Compile(prog)
	if err != nil {
		reportCompileError(path, err)
		return nil, nil, false
	}
	return prog, img, true
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
