// Package repl — сессия REPL (§11.4, N12) без привязки к транспорту:
// консоль на TTY, ввод из pipe и (позже) сокет работают поверх Session.
//
// Порция ввода — одна или несколько top-level инструкций (repl_input).
// Каждая инструкция — новая top-level лексическая область. Имена из
// предыдущих инструкций видны. Повторное связывание того же имени —
// shadowing между областями, не rebinding (принцип #12 соблюдён).
//
// Реализация: инструкция компилируется как `fn (<видимые имена...>) -> <stmt>`.
// Видимые имена передаются как параметры (локалы), а не как глобалы.
// Лямбды внутри инструкции захватывают их как upvalues — это даёт
// лексический снимок (N12): позже переопределённое имя не влияет на ранее
// созданное замыкание.
package repl

import (
	"fmt"
	"io"
	"strings"

	"github.com/it1ro/brig-lang/internal/ast"
	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/highlight"
	"github.com/it1ro/brig-lang/internal/parser"
	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/sema"
	"github.com/it1ro/brig-lang/internal/vm"
)

// Result — итог одной инструкции ввода.
type Result struct {
	// Input — номер ввода (§11.4 «Нумерация»): его получает ввод, значение
	// которого (значение последней инструкции) не `()`. 0 — номера нет.
	Input int
	// Name — имя связывания, созданного инструкцией; "" — связывания нет.
	Name string
	// Value — значение инструкции: у `name = expr` — значение expr, у
	// локальной fn, import и alias — `()`.
	Value runtime.Value
}

// Binding — видимая привязка сессии.
type Binding struct {
	Name  string
	Value runtime.Value
}

// Session — состояние интерактивной сессии.
type Session struct {
	vm       *vm.VM
	order    []string // порядок появления имён
	env      map[string]runtime.Value
	out      io.Writer
	pal      highlight.Palette
	diagFile string          // имя в диагностике sema; у REPL — `<repl>`
	seq      int             // счётчик инструкций: префикс глобальных имён
	history  []runtime.Value // значения пронумерованных вводов, history[n-1] — ввод n

	// Модули пользователя (modules.go): roots — файлы LoadModules,
	// mods — загруженные модули по имени, gen — поколение кода (recompile).
	roots []string
	mods  map[string]*sessionModule
	gen   int

	// docs — сигнатуры и текст `##` по имени (`len`, `Map.get`, `Map`).
	// extra — голые имена, добавленные RegisterHelpers.
	docs  map[string]*helpDoc
	extra map[string]bool
}

// New создаёт сессию поверх ВМ и запускает её актор (§11.4): планировщик
// работает и между вводами. out — куда писать диагностику и info.
// Сессию закрывает Close.
func New(m *vm.VM, out io.Writer) *Session {
	s := &Session{
		vm:       m,
		env:      make(map[string]runtime.Value),
		out:      out,
		diagFile: "<repl>",
	}
	if err := m.StartSession(); err != nil {
		panic(err)
	}
	s.installHelpers()
	return s
}

// Close останавливает фоновый планировщик. Повторный вызов ничего не делает.
func (s *Session) Close() { s.vm.CloseSession() }

// Interrupt снимает текущий ввод, не завершая актор сессии (§11.4).
func (s *Session) Interrupt() { s.vm.Interrupt() }

// SetOutput меняет, куда пишутся диагностика и info.
func (s *Session) SetOutput(out io.Writer) { s.out = out }

// SetPalette задаёт палитру диагностики. Нулевая палитра — без цвета
// (NO_COLOR, не-TTY).
func (s *Session) SetPalette(p highlight.Palette) { s.pal = p }

// SetDiagFile задаёт имя файла в диагностике sema. Пустое имя оставляет
// текущее. CLI script-режима подставляет путь файла (§E.1); REPL — `<repl>`.
func (s *Session) SetDiagFile(name string) {
	if name != "" {
		s.diagFile = name
	}
}

// Eval исполняет порцию ввода: инструкции по порядку, каждая — своя
// область. Ошибка разбора — ни одна инструкция не исполняется. Ошибка
// инструкции (sema, компиляция, raise) останавливает ввод: результаты и
// привязки инструкций до неё сохраняются и возвращаются вместе с ошибкой.
func (s *Session) Eval(src string) ([]Result, error) {
	lines, err := parser.ParseReplInput(src)
	if err != nil {
		return nil, err
	}
	var results []Result
	for _, prog := range lines {
		res, err := s.evalLine(src, prog)
		if err != nil {
			return results, err
		}
		results = append(results, res)
	}
	if n := len(results); n > 0 && results[n-1].Value.Kind != runtime.KindUnit {
		s.history = append(s.history, results[n-1].Value)
		for i := range results {
			results[i].Input = len(s.history)
		}
	}
	return results, nil
}

// evalLine исполняет одну инструкцию (repl_line). src — вся порция:
// позиции диагностики считаются по ней.
func (s *Session) evalLine(src string, prog *ast.Program) (Result, error) {
	return s.evalLineOpt(src, prog, false)
}

// evalLineHere — то же, но на акторе сессии без сдачи нового ввода.
// load зовёт так script-файл: сам load уже исполняется этим актором.
func (s *Session) evalLineHere(src string, prog *ast.Program) (Result, error) {
	return s.evalLineOpt(src, prog, true)
}

func (s *Session) evalLineOpt(src string, prog *ast.Program, here bool) (Result, error) {
	if len(prog.Stmts) == 0 {
		// import/alias: модули в сессии — T-209.
		return Result{Value: runtime.Unit}, nil
	}
	stmt := prog.Stmts[0]

	// Контекстный анализ (§F.3). info (затенение прелюдии и хелперов) не ошибка.
	semaRes := sema.CheckRepl(prog, s.extraNames())
	if err := WriteDiagnostics(s.out, s.diagFile, src, semaRes.Diagnostics, s.pal, s.HighlightEnv(), s.diagFile == "<repl>"); err != nil {
		return Result{}, err
	}
	if semaRes.HasErrors() {
		return Result{}, &printedError{err: fmt.Errorf("sema: %d error(s)", countErrors(semaRes))}
	}

	s.seq++
	c := compiler.New()
	fn, newName, err := c.CompileReplLine(s.seq, s.order, stmt)
	if err != nil {
		return Result{}, err
	}

	// Вложенные функции (лямбды и локальные fn) — глобалы, чтобы OpGetGlobal
	// их видел. Пишет их горутина планировщика вместе с вводом: карта
	// глобалов с фоновым циклом иначе гоняется.
	defs := make(map[string]runtime.Value, len(c.Image().Functions))
	for name, f := range c.Image().Functions {
		defs[name] = vm.FuncValue(f)
	}

	// Аргументы — текущие значения видимых имён.
	args := make([]runtime.Value, len(s.order))
	for i, n := range s.order {
		args[i] = s.env[n]
	}

	var val runtime.Value
	if here {
		for name, v := range defs {
			s.vm.DefineGlobal(name, v)
		}
		val, err = s.vm.Scheduler().CallNested(vm.FuncValue(fn), args)
	} else {
		val, err = s.vm.SessionEval(vm.FuncValue(fn), args, defs)
	}
	if err != nil {
		return Result{}, err
	}

	if newName != "" {
		if _, exists := s.env[newName]; !exists {
			s.order = append(s.order, newName)
		}
		s.env[newName] = val
	}
	if lfd, ok := stmt.(ast.LocalFnDecl); ok {
		s.noteLocal(lfd, src, s.seq)
		val = runtime.Unit
	}
	return Result{Name: newName, Value: val}, nil
}

// NeedMore сообщает, что ввод src не завершён и REPL ждёт следующей
// строки (§11.4 «Ввод»).
func (s *Session) NeedMore(src string) bool { return NeedMore(src) }

// HighlightEnv — имена для подсветки ввода: привязки сессии поверх
// прелюдии, хелперов, встроенных и загруженных модулей.
func (s *Session) HighlightEnv() highlight.Env {
	env := highlight.REPLEnv()
	for _, b := range s.Bindings() {
		env.Bindings[b.Name] = true
	}
	for n := range s.extra {
		env.Helpers[n] = true
		env.Modules["Repl"][n] = true
	}
	for name, m := range s.mods {
		fns := map[string]bool{}
		for _, g := range m.globals {
			// Поднятые локальные fn (`M.f@1$g`) вводу не видны.
			if f, ok := strings.CutPrefix(g, name+"."); ok && !strings.ContainsAny(f, ".@$") {
				fns[f] = true
			}
		}
		env.Modules[name] = fns
	}
	return env
}

// Bindings возвращает видимые привязки в порядке появления имён.
func (s *Session) Bindings() []Binding {
	out := make([]Binding, len(s.order))
	for i, n := range s.order {
		out[i] = Binding{Name: n, Value: s.env[n]}
	}
	return out
}

// Next — номер, который получит следующий ввод со значением (приглашение
// `brig[n]>`, §11.4).
func (s *Session) Next() int { return len(s.history) + 1 }

// Reset снимает все привязки сессии.
func (s *Session) Reset() {
	s.order = nil
	s.env = make(map[string]runtime.Value)
}

// Complete — варианты дополнения ввода (src, pos): src — текст, pos — позиция курсора.
// Заготовка: T-211.
func (s *Session) Complete(_ string, _ int) []string { return nil }

// Doc — сигнатуры и текст `##` функции `M.f`/`f` или модуля (§11.4).
// false — такого имени в сессии нет. Тот же текст печатает `h`;
// подсказки сигнатур (T-211) берут его отсюда.
func (s *Session) Doc(name string) (string, bool) {
	d, ok := s.docs[name]
	if !ok {
		return "", false
	}
	return d.format(s.pal), true
}

func countErrors(res *sema.Result) int {
	n := 0
	for _, d := range res.Diagnostics {
		if d.Severity == sema.SeverityError {
			n++
		}
	}
	return n
}

// TrimLeadingPrompt удаляет префикс "> " из строки, если он есть.
func TrimLeadingPrompt(line string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), ">"))
}
