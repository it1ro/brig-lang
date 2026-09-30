package repl

import (
	"fmt"

	"github.com/it1ro/brig-lang/internal/ast"
	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/sema"
	"github.com/it1ro/brig-lang/internal/vm"
)

// Entry — программа, в которой исполняются вводы сессии (доктесты файла,
// §11.6, T-245): Mods[0] — входной модуль, его fn, типы записей и
// конструкторы вариантов видны вводам голыми именами, как коду самого
// модуля; Image — образ compiler.CompileProgram(Mods).
type Entry struct {
	Mods  []compiler.Module
	Image *compiler.ProgramImage
}

// UseEntry ставит в сессию программу e (до первого ввода). Функции
// образа — глобалы ВМ; fn входного модуля с именем хелпера Repl затеняет
// хелпер (§11.4 «Затенение»): info-диагностика, как у связывания, хелпер
// остаётся доступен как `Repl.v`. Декларации e видит компилятор каждого
// следующего ввода.
func (s *Session) UseEntry(e *Entry) error {
	if e == nil || len(e.Mods) == 0 {
		return nil
	}
	if err := compiler.New().DeclareProgram(e.Mods); err != nil {
		return err
	}
	entry := e.Mods[0]
	helpers := map[string]bool{}
	for _, n := range sema.ReplHelperNames() {
		helpers[n] = true
	}
	for n := range s.extra {
		helpers[n] = true
	}
	seen := map[string]bool{}
	for _, d := range entry.Prog.Decls {
		fd, ok := d.(ast.FuncDecl)
		if !ok || seen[fd.FnName()] || !helpers[fd.FnName()] {
			continue
		}
		seen[fd.FnName()] = true
		line, col := fd.Pos(), fd.End() // (Line, Col), см. sema.posOf
		name := fd.FnName()
		if _, err := fmt.Fprintf(s.out, "info: %s:%d:%d: `%s` shadows repl helper; use `Repl.%s` if the helper was intended\n",
			entry.Path, line, col, name, name); err != nil {
			return err
		}
	}
	defs := make(map[string]runtime.Value, len(e.Image.Functions))
	for name, f := range e.Image.Functions {
		defs[name] = vm.FuncValue(f)
	}
	if err := s.vm.SessionRedefine(defs, nil); err != nil {
		return err
	}
	s.entry = e.Mods
	return nil
}
