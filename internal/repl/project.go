package repl

// Загрузка файлов и проекта в сессию (§11.4 «Загрузка», T-209).
// Модуль из файла пользователя виден целиком, включая не-pub.
// Модуль зависимости (каталог deps/) — только pub. Глобалы не-pub fn
// при этом остаются: свой код модуля зовёт их как раньше.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/it1ro/brig-lang/internal/ast"
	"github.com/it1ro/brig-lang/internal/loader"
	"github.com/it1ro/brig-lang/internal/runtime"
)

// LoadFile загружает файл пользователя: модуль — в сессию (main не
// вызывается), script — как вводы. anchor задаёт корень import, если
// он ещё не задан (явный файл -i, не init). Ошибка печатается; привязки
// до неё сохраняются.
func (s *Session) LoadFile(path string, anchor bool) error {
	if anchor && s.modRoot == "" {
		s.modRoot = filepath.Dir(path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return s.present(path, "", err)
	}
	mod, err := sourceIsModule(b)
	if err != nil {
		return s.present(path, string(b), err)
	}
	if mod {
		_, err = s.loadModules(path, false)
		return err
	}
	return s.execScript(path, string(b), false)
}

// LoadProject находит корень по project.brig вверх от start, грузит
// модули (корень модулей — lib/, если каталог есть, иначе сам проект)
// и модули из deps/ как зависимости. main не вызывается.
func (s *Session) LoadProject(start string) error {
	root, err := loader.ProjectRoot(start)
	if err != nil {
		return s.present(start, "", err)
	}
	s.projectRoot = root
	modRoot := root
	if fi, statErr := os.Stat(filepath.Join(root, "lib")); statErr == nil && fi.IsDir() {
		modRoot = filepath.Join(root, "lib")
	}
	s.modRoot = modRoot
	files, err := moduleFiles(modRoot)
	if err != nil {
		return s.present(modRoot, "", err)
	}
	if err := s.loadUserFiles(files); err != nil {
		return err
	}
	depsRoot := filepath.Join(root, "deps")
	fi, statErr := os.Stat(depsRoot)
	if statErr != nil || !fi.IsDir() {
		return nil
	}
	depFiles, err := moduleFiles(depsRoot)
	if err != nil {
		return s.present(depsRoot, "", err)
	}
	for _, path := range depFiles {
		if _, err := s.loadModulesAs(path, false, false); err != nil {
			return err
		}
	}
	return nil
}

func (s *Session) loadUserFiles(files []string) error {
	for _, path := range files {
		b, err := os.ReadFile(path)
		if err != nil {
			return s.present(path, "", err)
		}
		mod, err := sourceIsModule(b)
		if err != nil {
			return s.present(path, string(b), err)
		}
		if !mod {
			continue
		}
		if _, err := s.loadModules(path, false); err != nil {
			return err
		}
	}
	return nil
}

// moduleFiles — *.brig под root, кроме project.brig, *_test.brig и
// каталогов deps, test, .brig, .git. deps обходится отдельно.
func moduleFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && skipProjectDir(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".brig") || name == "project.brig" || strings.HasSuffix(name, "_test.brig") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	sort.Strings(files)
	return files, err
}

func skipProjectDir(name string) bool {
	switch name {
	case "deps", "test", ".brig", ".git":
		return true
	}
	return false
}

// present печатает ошибку загрузки, если она ещё не напечатана.
func (s *Session) present(path, src string, err error) error {
	if err == nil {
		return nil
	}
	var pe *printedError
	if errors.As(err, &pe) {
		return err
	}
	if src == "" {
		f, line, col, msg, located := DescribeCompileError(path, err)
		if !located {
			f, line, col, msg = path, 1, 1, err.Error()
		}
		if _, werr := fmt.Fprintln(s.out, FormatE1("error", f, line, col, msg)); werr != nil {
			return werr
		}
		return err
	}
	if werr := writeEvalError(s.out, path, src, err, Print{Env: s.HighlightEnv(), Pal: s.pal}); werr != nil {
		return werr
	}
	return err
}

func (s *Session) evalDirectives(prog *ast.Program, here bool) (Result, error) {
	for _, d := range prog.Decls {
		switch d := d.(type) {
		case ast.ImportDecl:
			if err := s.importModule(d.ImportedModule(), d.Pos(), d.End(), here); err != nil {
				return Result{}, err
			}
		case ast.AliasDecl:
			if err := s.aliasModule(d.AliasOriginal(), d.AliasName(), d.Pos(), d.End(), here); err != nil {
				return Result{}, err
			}
		}
	}
	return Result{Value: runtime.Unit}, nil
}

func (s *Session) importModule(name string, line, col int, here bool) error {
	if loader.IsBuiltin(name) || s.mods[name] != nil {
		return nil
	}
	root := s.modRoot
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return s.present("<repl>", "", err)
		}
	}
	path, ok := loader.FileFor(root, name)
	if !ok {
		msg := fmt.Sprintf("module %s not found", name)
		if err := writeLocated(s.out, "error", s.diagFile, line, col, msg, "", s.pal, s.HighlightEnv(), false, false); err != nil {
			return err
		}
		return &printedError{err: fmt.Errorf("%s", msg)}
	}
	_, err := s.loadModulesAs(path, here, true)
	return err
}

func (s *Session) aliasModule(original, alias string, line, col int, here bool) error {
	if loader.IsBuiltin(original) {
		return nil
	}
	if err := s.importModule(original, line, col, here); err != nil {
		return err
	}
	m := s.mods[original]
	if m == nil {
		msg := fmt.Sprintf("module %s not found", original)
		if err := writeLocated(s.out, "error", s.diagFile, line, col, msg, "", s.pal, s.HighlightEnv(), false, false); err != nil {
			return err
		}
		return &printedError{err: fmt.Errorf("%s", msg)}
	}
	apply := func() error {
		defs := make(map[string]runtime.Value)
		var globals []string
		for _, g := range m.globals {
			rest, ok := strings.CutPrefix(g, original+".")
			if !ok {
				continue
			}
			name := alias + "." + rest
			defs[name] = s.vm.Global(g)
			globals = append(globals, name)
		}
		s.vm.SessionRedefineHere(defs, nil)
		if s.mods == nil {
			s.mods = map[string]*sessionModule{}
		}
		cp := *m
		cp.name = alias
		cp.globals = globals
		s.mods[alias] = &cp
		if s.deps[original] {
			if s.deps == nil {
				s.deps = map[string]bool{}
			}
			s.deps[alias] = true
		}
		return nil
	}
	if here {
		return apply()
	}
	return s.vm.Scheduler().Sync(apply)
}

// denyPrivate отклоняет ссылку ввода на не-pub функцию зависимости.
func (s *Session) denyPrivate(src string, stmt ast.Stmt) error {
	w := &privWalk{s: s, src: src}
	if err := ast.Walk(w, stmt); err != nil {
		return err
	}
	return w.err
}

type privWalk struct {
	s   *Session
	src string
	err error
}

func (p *privWalk) VisitNode(ast.Node) error       { return nil }
func (p *privWalk) VisitStmt(ast.Stmt) error       { return nil }
func (p *privWalk) VisitPattern(ast.Pattern) error { return nil }
func (p *privWalk) VisitType(ast.Type) error       { return nil }
func (p *privWalk) VisitDecl(ast.Decl) error       { return nil }

func (p *privWalk) VisitExpr(e ast.Expr) error {
	if p.err != nil {
		return p.err
	}
	me, ok := e.(ast.MemberExpr)
	if !ok {
		return nil
	}
	segs, ok := memberSegs(me)
	if !ok {
		return nil
	}
	mod, member, ok := splitMember(segs)
	if !ok || !p.s.deps[mod] {
		return nil
	}
	m := p.s.mods[mod]
	if m == nil || !m.priv[member] || !hasFn(m, mod+"."+member) {
		return nil
	}
	arity := 0
	if m.arity != nil {
		arity = m.arity[member]
	}
	msg := fmt.Sprintf("%s/%d is private to %s", member, arity, mod)
	show := p.src != "" && p.s.diagFile == "<repl>"
	if err := writeLocated(p.s.out, "error", p.s.diagFile, me.Pos(), me.End(), msg, p.src, p.s.pal, p.s.HighlightEnv(), show, false); err != nil {
		return err
	}
	p.err = &printedError{err: fmt.Errorf("%s", msg)}
	return p.err
}

func hasFn(m *sessionModule, name string) bool {
	for _, g := range m.globals {
		if g == name {
			return true
		}
	}
	return false
}

func memberSegs(e ast.Expr) ([]string, bool) {
	switch x := e.(type) {
	case ast.VariableExpr:
		name := x.Name()
		if name == "" || name[0] < 'A' || name[0] > 'Z' {
			return nil, false
		}
		return []string{name}, true
	case ast.MemberExpr:
		segs, ok := memberSegs(x.Obj())
		if !ok {
			return nil, false
		}
		return append(segs, x.MemberName()), true
	}
	return nil, false
}

func splitMember(segs []string) (mod, member string, ok bool) {
	if len(segs) < 2 {
		return "", "", false
	}
	member = segs[len(segs)-1]
	if member == "" || (member[0] >= 'A' && member[0] <= 'Z') {
		return "", "", false
	}
	return strings.Join(segs[:len(segs)-1], "."), member, true
}
