package repl

// Модули пользователя в сессии (§11.4 «Загрузка») и recompile() — решение
// T-208 (#246), вариант D: recompile() заменяет функции изменённых модулей
// атомарно для всех акторов. Вызов по имени (`M.f(…)`, `f(…)` внутри
// модуля — всегда GETGLOBAL) исполняет версию, текущую в момент вызова;
// функция-значение исполняет код, из которого получена. Поднятые
// локальные fn получают поколение в имени (Compiler.SetGeneration): старый
// кадр вызывает свою версию. Записи — по имени типа, без миграции.

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/it1ro/brig-lang/internal/ast"
	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/loader"
	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/sema"
	"github.com/it1ro/brig-lang/internal/vm"
)

// sessionModule — модуль пользователя, загруженный в сессию.
type sessionModule struct {
	name, path string
	mtime      time.Time
	size       int64
	sum        [sha256.Size]byte
	// globals — глобальные имена функций модуля, записанные в ВМ
	// (включая поднятые локальные fn всех поколений).
	globals []string
}

// LoadModules загружает в сессию модуль из файла path и все модули,
// которые он импортирует (§11.1). Функции модулей видны вводам по имени
// `M.f`, включая не-`pub` (§11.4). Возвращает имена загруженных модулей
// в порядке загрузки. Ошибка разбора, sema или компиляции печатается в
// формате E.1 в вывод сессии; сессия остаётся без изменений.
// script-файл (§11.3) — не модуль: его исполняет load.
func (s *Session) LoadModules(path string) ([]string, error) {
	return s.loadModules(path, false)
}

// loadModules — LoadModules; here — зовёт натив на горутине цикла (load).
func (s *Session) loadModules(path string, here bool) ([]string, error) {
	g, err := s.loadGraph(path)
	if err != nil {
		return nil, err
	}
	if g.Entry.Prog.Module == "" {
		return nil, fmt.Errorf("%s: script file is not a module", path)
	}
	roots := append(append([]string(nil), s.roots...), path)
	graphs, err := s.loadGraphs(s.roots)
	if err != nil {
		return nil, err
	}
	mods, err := mergeGraphs(append(graphs, g))
	if err != nil {
		return nil, s.report(err)
	}
	var fresh []*loader.Module
	for _, m := range g.Modules {
		if _, ok := s.mods[m.Name]; !ok {
			fresh = append(fresh, m)
		}
	}
	if err := s.install(mods, fresh, here); err != nil {
		return nil, err
	}
	s.roots = roots
	return moduleNames(fresh), nil
}

// Recompile перечитывает файлы загруженных модулей и, если хоть один
// изменился (mtime и размер, затем хеш содержимого), перекомпилирует граф
// и заменяет функции изменённых модулей (T-208, #246). Возвращает имена
// перекомпилированных модулей; пустой список — изменений нет. Ошибка в
// любом модуле печатается в формате E.1, и ничего не заменяется: сессия
// остаётся на старом коде. script-файлы Recompile не трогает.
func (s *Session) Recompile() ([]string, error) {
	return s.recompile(false)
}

// recompile — Recompile; here — зовёт натив на горутине цикла (recompile()).
func (s *Session) recompile(here bool) ([]string, error) {
	if !s.modulesChanged() {
		return nil, nil
	}
	graphs, err := s.loadGraphs(s.roots)
	if err != nil {
		return nil, err
	}
	mods, err := mergeGraphs(graphs)
	if err != nil {
		return nil, s.report(err)
	}
	var changed []*loader.Module
	for _, m := range mods {
		old, ok := s.mods[m.Name]
		if !ok {
			changed = append(changed, m)
			continue
		}
		st, err := stat(m.Path)
		if err != nil {
			return nil, err
		}
		if st.sum != old.sum || m.Path != old.path {
			changed = append(changed, m)
		}
	}
	if len(changed) == 0 {
		// Файл тронули, но содержимое прежнее: обновить mtime.
		for _, m := range mods {
			if st, err := stat(m.Path); err == nil {
				s.mods[m.Name].mtime, s.mods[m.Name].size = st.mtime, st.size
			}
		}
		return nil, nil
	}
	if err := s.install(mods, changed, here); err != nil {
		return nil, err
	}
	return moduleNames(changed), nil
}

// modulesChanged — хоть один файл загруженного модуля изменился.
func (s *Session) modulesChanged() bool {
	for _, m := range s.mods {
		fi, err := os.Stat(m.path)
		if err != nil || !fi.ModTime().Equal(m.mtime) || fi.Size() != m.size {
			if err != nil {
				return true
			}
			st, err := stat(m.path)
			if err != nil || st.sum != m.sum {
				return true
			}
		}
	}
	return false
}

// install проверяет и компилирует граф mods целиком и заменяет в ВМ
// функции модулей install (новое поколение). Ошибка — ничего не заменено.
// here — вызов из натива на горутине цикла: глобалы меняются сразу.
func (s *Session) install(mods, install []*loader.Module, here bool) error {
	world := make([]sema.Module, len(mods))
	for i, m := range mods {
		world[i] = sema.Module{Name: m.Name, Prog: m.Prog}
	}
	w := sema.NewWorld(world)
	failed := 0
	failedPath := ""
	for _, m := range mods {
		res := sema.CheckNamesSession(m.Prog, w)
		for _, d := range res.Diagnostics {
			sev := "error"
			if d.Severity == sema.SeverityInfo {
				sev = "info"
			}
			if _, err := fmt.Fprintf(s.out, "%s: %s:%d:%d: %s\n", sev, m.Path, d.Line, d.Col, d.Message); err != nil {
				return err
			}
		}
		if n := countErrors(res); n > 0 {
			failed += n
			if failedPath == "" {
				failedPath = m.Path
			}
		}
	}
	if failed > 0 {
		return &fs.PathError{Op: "sema", Path: failedPath, Err: fmt.Errorf("%d error(s)", failed)}
	}

	// Модули сессии — не входные: пустой входной модуль в начале, у
	// остальных префикс `M.` (как у импортированных в программе).
	cms := make([]compiler.Module, 0, len(mods)+1)
	cms = append(cms, compiler.Module{Prog: &ast.Program{}})
	for _, m := range mods {
		cms = append(cms, compiler.Module{Name: m.Name, Path: m.Path, Prog: m.Prog})
	}
	c := compiler.New()
	c.SetGeneration(s.gen + 1)
	img, err := c.CompileProgram(cms)
	if err != nil {
		return s.report(err)
	}

	sums := make(map[string]fileStat, len(install))
	for _, m := range install {
		st, err := stat(m.Path)
		if err != nil {
			return err
		}
		sums[m.Name] = st
	}

	names := moduleNames(mods)
	defs := make(map[string]runtime.Value)
	byMod := make(map[string][]string)
	for name, f := range img.Functions {
		owner := ownerModule(names, name)
		if _, ok := sums[owner]; !ok {
			continue
		}
		defs[name] = vm.FuncValue(f)
		byMod[owner] = append(byMod[owner], name)
	}
	var undef []string
	for _, m := range install {
		if old := s.mods[m.Name]; old != nil {
			for _, g := range old.globals {
				// Поднятые fn прежнего поколения (`M.f@1$g`) остаются:
				// их вызывают старые кадры (решение D).
				if _, ok := defs[g]; !ok && !strings.Contains(g, "$") {
					undef = append(undef, g)
				}
			}
		}
	}
	if here {
		s.vm.SessionRedefineHere(defs, undef)
	} else if err := s.vm.SessionRedefine(defs, undef); err != nil {
		return err
	}

	s.gen++
	if s.mods == nil {
		s.mods = make(map[string]*sessionModule)
	}
	for _, m := range install {
		st := sums[m.Name]
		g := byMod[m.Name]
		if old := s.mods[m.Name]; old != nil {
			for _, name := range old.globals {
				if _, ok := defs[name]; !ok && strings.Contains(name, "$") {
					g = append(g, name)
				}
			}
		}
		sort.Strings(g)
		s.mods[m.Name] = &sessionModule{
			name: m.Name, path: m.Path,
			mtime: st.mtime, size: st.size, sum: st.sum,
			globals: g,
		}
	}
	s.indexLoaded(install)
	return nil
}

// loadGraph загружает граф модулей от path; ошибка разбора печатается.
func (s *Session) loadGraph(path string) (*loader.Graph, error) {
	g, err := loader.Load(path)
	if err != nil {
		return nil, s.report(err)
	}
	return g, nil
}

func (s *Session) loadGraphs(paths []string) ([]*loader.Graph, error) {
	gs := make([]*loader.Graph, 0, len(paths))
	for _, p := range paths {
		g, err := s.loadGraph(p)
		if err != nil {
			return nil, err
		}
		gs = append(gs, g)
	}
	return gs, nil
}

// report печатает ошибку загрузки или компиляции в формате E.1 и
// возвращает её.
func (s *Session) report(err error) error {
	var le *loader.Error
	var ce *compiler.Error
	msg := err.Error()
	switch {
	case errors.As(err, &le):
		msg = le.Error()
	case errors.As(err, &ce):
		msg = fmt.Sprintf("%s:%d:%d: %s", ce.File, ce.Line, ce.Col, ce.Msg)
	}
	if _, werr := fmt.Fprintf(s.out, "error: %s\n", msg); werr != nil {
		return werr
	}
	return err
}

// mergeGraphs объединяет модули графов: каждый модуль один раз, в порядке
// загрузки. Одно имя у двух разных файлов — ошибка.
func mergeGraphs(gs []*loader.Graph) ([]*loader.Module, error) {
	seen := make(map[string]*loader.Module)
	var out []*loader.Module
	for _, g := range gs {
		for _, m := range g.Modules {
			if prev, ok := seen[m.Name]; ok {
				if prev.Path != m.Path {
					return nil, fmt.Errorf("module %s is loaded from %s and %s", m.Name, prev.Path, m.Path)
				}
				continue
			}
			seen[m.Name] = m
			out = append(out, m)
		}
	}
	return out, nil
}

// ownerModule — модуль из names, которому принадлежит глобал функции
// name (`Http.Client.get$loop@2` → `Http.Client`): самый длинный префикс.
func ownerModule(names []string, name string) string {
	owner := ""
	for _, m := range names {
		if strings.HasPrefix(name, m+".") && len(m) > len(owner) {
			owner = m
		}
	}
	return owner
}

func moduleNames(mods []*loader.Module) []string {
	out := make([]string, len(mods))
	for i, m := range mods {
		out[i] = m.Name
	}
	return out
}

type fileStat struct {
	mtime time.Time
	size  int64
	sum   [sha256.Size]byte
}

func stat(path string) (fileStat, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return fileStat{}, err
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return fileStat{}, err
	}
	return fileStat{mtime: fi.ModTime(), size: int64(len(src)), sum: sha256.Sum256(src)}, nil
}
