// Package loader строит граф модулей программы из файлов (§11.1).
//
// Программа — входной файл и все модули, достижимые из него через
// `import` и `alias`. Корень — каталог входного файла. Имя разрешается
// по порядку: встроенный модуль, затем файл по правилу путь → имя
// (`import Http.Client` → `http/client.brig`). Каждый модуль
// загружается один раз, поэтому циклы импорта допустимы (T-122 п.2,
// вариант A).
//
// Loader только парсит: контекстный анализ и компиляция — дело
// вызывающего (cmd/brig). Компиляция нескольких модулей — T-137.
package loader

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/it1ro/brig-lang/internal/ast"
	"github.com/it1ro/brig-lang/internal/lexer"
	"github.com/it1ro/brig-lang/internal/parser"
)

// Module — один загруженный модуль.
type Module struct {
	// Name — имя модуля: явный `module X` или имя по пути файла.
	Name string
	// Path — путь файла: у входного — как передан в Load, у остальных —
	// filepath.Join(Root, <путь по имени>).
	Path string
	Prog *ast.Program
}

// Graph — все модули программы.
type Graph struct {
	// Root — каталог входного файла.
	Root  string
	Entry *Module
	// Modules — в порядке загрузки (обход в глубину от входного файла),
	// каждый модуль ровно один раз; Modules[0] == Entry.
	Modules []*Module
}

// Error — ошибка загрузки с позицией (формат E.1 без префикса `error: `).
type Error struct {
	File      string
	Line, Col int
	Msg       string
	// Err — исходная ошибка лексера/парсера, если есть.
	Err error
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s:%d:%d: %s", e.File, e.Line, e.Col, e.Msg)
}

func (e *Error) Unwrap() error { return e.Err }

// builtinModules — встроенные модули: доступны без файла, не загружаются.
// Список совпадает с isPreludeModule в internal/compiler.
var builtinModules = map[string]bool{
	"Vec": true, "Map": true, "Str": true, "Bytes": true,
	"Json": true, "Test": true, "Sys": true, "Prelude": true,
}

// IsBuiltin сообщает, что name — встроенный модуль.
func IsBuiltin(name string) bool { return builtinModules[name] }

type loader struct {
	root   string
	byPath map[string]*loaded
	graph  *Graph
}

type loaded struct {
	mod *Module
	// declared — имя из явного `module X`, "" если его нет.
	declared string
}

// Load загружает входной файл entry и все модули, достижимые из него.
// Ошибка чтения входного файла возвращается как есть; остальные
// ошибки — *Error.
func Load(entry string) (*Graph, error) {
	src, err := os.ReadFile(entry)
	if err != nil {
		return nil, err
	}
	l := &loader{
		root:   filepath.Dir(entry),
		byPath: map[string]*loaded{},
	}
	l.graph = &Graph{Root: l.root}
	prog, err := parseFile(entry, src)
	if err != nil {
		return nil, err
	}
	name := prog.Module
	if name == "" {
		name = pathName(filepath.Base(entry))
	}
	m := l.add(entry, name, prog)
	l.graph.Entry = m.mod
	if err := l.loadImports(m.mod); err != nil {
		return nil, err
	}
	return l.graph, nil
}

func (l *loader) add(path, name string, prog *ast.Program) *loaded {
	m := &loaded{
		mod:      &Module{Name: name, Path: path, Prog: prog},
		declared: prog.Module,
	}
	l.byPath[filepath.Clean(path)] = m
	l.graph.Modules = append(l.graph.Modules, m.mod)
	return m
}

// loadImports загружает модули, на которые ссылаются директивы m.
func (l *loader) loadImports(m *Module) error {
	for _, d := range m.Prog.Decls {
		var name string
		switch d := d.(type) {
		case ast.ImportDecl:
			name = d.ImportedModule()
		case ast.AliasDecl:
			name = d.AliasOriginal()
		default:
			continue
		}
		if err := l.resolve(m.Path, d.Pos(), d.End(), name); err != nil {
			return err
		}
	}
	return nil
}

// resolve загружает модуль name, на который ссылается директива в файле
// from (позиция line:col), если он ещё не загружен.
func (l *loader) resolve(from string, line, col int, name string) error {
	if IsBuiltin(name) {
		return nil
	}
	errAt := func(format string, args ...any) error {
		return &Error{File: from, Line: line, Col: col, Msg: fmt.Sprintf(format, args...)}
	}
	rel := modulePath(name)
	path := filepath.Join(l.root, rel)
	if m, ok := l.byPath[filepath.Clean(path)]; ok {
		if m.declared != "" && m.declared != name {
			return errAt("module %s: file %s declares module %s", name, m.mod.Path, m.declared)
		}
		return nil
	}
	// Обратная проверка: имя должно получаться из найденного пути
	// (например, `import HTTP` не находит http.brig).
	if pathName(rel) != name {
		return errAt("module %s not found", name)
	}
	src, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || isDir(path) {
			return errAt("module %s not found", name)
		}
		return errAt("module %s: %v", name, err)
	}
	prog, err := parseFile(path, src)
	if err != nil {
		return err
	}
	if prog.Module != "" && prog.Module != name {
		return errAt("module %s: file %s declares module %s", name, path, prog.Module)
	}
	m := l.add(path, name, prog)
	return l.loadImports(m.mod)
}

func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// parseFile парсит модуль; ошибка лексера/парсера — *Error с позицией.
func parseFile(path string, src []byte) (*ast.Program, error) {
	prog, err := parser.ParseProgram(parser.ModeModule, string(src))
	if err == nil {
		return prog, nil
	}
	e := &Error{File: path, Line: 1, Col: 1, Msg: err.Error(), Err: err}
	var le *lexer.Error
	var pe *parser.Error
	switch {
	case errors.As(err, &le):
		e.Line, e.Col, e.Msg = le.Line, le.Col, le.Msg
	case errors.As(err, &pe):
		e.Line, e.Col, e.Msg = pe.Line, pe.Col, pe.Msg
	}
	return nil, e
}

// pathName — имя модуля по относительному пути файла: без `.brig`,
// каждый сегмент → PascalCase (`http/client.brig` → `Http.Client`,
// `http_client.brig` → `HttpClient`).
func pathName(rel string) string {
	rel = strings.TrimSuffix(filepath.ToSlash(rel), ".brig")
	segs := strings.Split(rel, "/")
	for i, s := range segs {
		var b strings.Builder
		for _, part := range strings.Split(s, "_") {
			r := []rune(part)
			if len(r) == 0 {
				continue
			}
			r[0] = unicode.ToUpper(r[0])
			b.WriteString(string(r))
		}
		segs[i] = b.String()
	}
	return strings.Join(segs, ".")
}

// modulePath — относительный путь файла по имени модуля, обратное к
// pathName: `Http.Client` → `http/client.brig`, `HttpClient` →
// `http_client.brig`.
func modulePath(name string) string {
	segs := strings.Split(name, ".")
	for i, s := range segs {
		var b strings.Builder
		r := []rune(s)
		for j, c := range r {
			if unicode.IsUpper(c) {
				if j > 0 && (unicode.IsLower(r[j-1]) || unicode.IsDigit(r[j-1])) {
					b.WriteByte('_')
				}
				c = unicode.ToLower(c)
			}
			b.WriteRune(c)
		}
		segs[i] = b.String()
	}
	return filepath.Join(segs...) + ".brig"
}
