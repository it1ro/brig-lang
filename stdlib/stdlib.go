// Package stdlib — встроенные модули на Brig (T-146): `List`, `Option`,
// `Result`; `Server` и `Supervisor` (T-170). Исходники `*.brig` лежат
// рядом и встраиваются в бинарник через go:embed, поэтому `brig`
// работает без них на диске. Тесты модулей — `*_test.brig` рядом, в
// бинарник они не встраиваются.
//
// Модули — встроенные (§11.1): доступны без `import`, видны только их
// `pub fn` (§11.2). Разрешение имён (sema), компилятор и загрузчик
// спрашивают IsModule; скомпилированный образ ставит в ВМ
// compiler.InstallStdlib.
package stdlib

import (
	"embed"
	"fmt"
	"sync"

	"github.com/it1ro/brig-lang/internal/ast"
	"github.com/it1ro/brig-lang/internal/parser"
)

//go:embed list.brig option.brig result.brig server.brig supervisor.brig
var files embed.FS

// Module — встроенный модуль на Brig.
type Module struct {
	Name string
	// Path — путь исходника от корня репозитория: stack trace и
	// диагностика указывают на него.
	Path string
	Src  string
	Prog *ast.Program
}

// names — модули в порядке компиляции; имя файла — по правилу
// путь → имя (§11.1).
var names = []struct{ name, file string }{
	{"List", "list.brig"},
	{"Option", "option.brig"},
	{"Result", "result.brig"},
	{"Server", "server.brig"},
	{"Supervisor", "supervisor.brig"},
}

// IsModule сообщает, что name — встроенный модуль на Brig.
func IsModule(name string) bool {
	for _, n := range names {
		if n.name == name {
			return true
		}
	}
	return false
}

// Names — имена встроенных модулей на Brig.
func Names() []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = n.name
	}
	return out
}

var (
	once    sync.Once
	modules []Module
	loadErr error
)

// Modules — разобранные модули. Разбор — один раз на процесс; AST не
// меняется, его разделяют все потребители.
func Modules() ([]Module, error) {
	once.Do(func() { modules, loadErr = load() })
	return modules, loadErr
}

// MustModules — Modules; ошибка разбора встроенного исходника — ошибка
// сборки бинарника, её ловят тесты пакета.
func MustModules() []Module {
	ms, err := Modules()
	if err != nil {
		panic(err)
	}
	return ms
}

func load() ([]Module, error) {
	out := make([]Module, 0, len(names))
	for _, n := range names {
		src, err := files.ReadFile(n.file)
		if err != nil {
			return nil, err
		}
		path := "stdlib/" + n.file
		prog, err := parser.ParseProgram(parser.ModeModule, string(src))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if prog.Module != n.name {
			return nil, fmt.Errorf("%s: declares module %q, want %s", path, prog.Module, n.name)
		}
		out = append(out, Module{Name: n.name, Path: path, Src: string(src), Prog: prog})
	}
	return out, nil
}
