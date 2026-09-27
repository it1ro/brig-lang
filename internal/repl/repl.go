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
	diagFile string          // имя в диагностике sema; у REPL — `<repl>`
	seq      int             // счётчик инструкций: префикс глобальных имён
	history  []runtime.Value // значения пронумерованных вводов, history[n-1] — ввод n
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
	return s
}

// Close останавливает фоновый планировщик. Повторный вызов ничего не делает.
func (s *Session) Close() { s.vm.CloseSession() }

// Interrupt снимает текущий ввод, не завершая актор сессии (§11.4).
func (s *Session) Interrupt() { s.vm.Interrupt() }

// SetOutput меняет, куда пишутся диагностика и info.
func (s *Session) SetOutput(out io.Writer) { s.out = out }

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
		res, err := s.evalLine(prog)
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

// evalLine исполняет одну инструкцию (repl_line).
func (s *Session) evalLine(prog *ast.Program) (Result, error) {
	if len(prog.Stmts) == 0 {
		// import/alias: модули в сессии — T-209.
		return Result{Value: runtime.Unit}, nil
	}
	stmt := prog.Stmts[0]

	// Контекстный анализ (§F.3).
	semaRes := sema.Check(prog)
	for _, d := range semaRes.Diagnostics {
		sev := "error"
		if d.Severity == sema.SeverityInfo {
			sev = "info"
		}
		if _, err := fmt.Fprintf(s.out, "%s: %s:%d:%d: %s\n", sev, s.diagFile, d.Line, d.Col, d.Message); err != nil {
			return Result{}, err
		}
	}
	if semaRes.HasErrors() {
		return Result{}, fmt.Errorf("sema: %d error(s)", countErrors(semaRes))
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

	val, err := s.vm.SessionEval(vm.FuncValue(fn), args, defs)
	if err != nil {
		return Result{}, err
	}

	if newName != "" {
		if _, exists := s.env[newName]; !exists {
			s.order = append(s.order, newName)
		}
		s.env[newName] = val
	}
	if _, ok := stmt.(ast.LocalFnDecl); ok {
		val = runtime.Unit
	}
	return Result{Name: newName, Value: val}, nil
}

// NeedMore сообщает, что ввод src не завершён и REPL ждёт следующей
// строки (§11.4 «Ввод»).
func (s *Session) NeedMore(src string) bool { return NeedMore(src) }

// HighlightEnv — имена для подсветки ввода: привязки сессии поверх
// прелюдии, хелперов и встроенных модулей. Загруженные модули
// пользователя добавятся сюда вместе с загрузкой (T-209).
func (s *Session) HighlightEnv() highlight.Env {
	env := highlight.REPLEnv()
	for _, b := range s.Bindings() {
		env.Bindings[b.Name] = true
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

// Doc — документация функции `M.f`/`f` или модуля по имени, для `h/1`.
// Заготовка: T-206.
func (s *Session) Doc(_ string) (string, bool) { return "", false }

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
