// Package repl — persistent REPL (§11.4, N12, Sprint 6.2).
//
// Каждая введённая строка — новая top-level лексическая область.
// Имена из предыдущих строк видны. Повторное связывание того же имени —
// shadowing между областями, не rebinding (принцип #12 соблюдён).
//
// Реализация: строка компилируется как `fn (<видимые имена...>) -> <stmt>`.
// Видимые имена передаются как параметры (локалы), а не как глобалы.
// Лямбды внутри строки захватывают их как upvalues — это даёт лексический
// снимок (N12): позже переопределённое имя не влияет на ранее созданное
// замыкание.
package repl

import (
	"fmt"
	"io"
	"strings"

	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/parser"
	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/sema"
	"github.com/it1ro/brig-lang/internal/vm"
)

// REPL — persistent-состояние интерактивной сессии.
type REPL struct {
	vm    *vm.VM
	order []string // порядок появления имён
	env   map[string]runtime.Value
	out   io.Writer
}

// New создаёт REPL поверх ВМ. out — куда писать диагностику и info.
func New(m *vm.VM, out io.Writer) *REPL {
	return &REPL{
		vm:  m,
		env: make(map[string]runtime.Value),
		out: out,
	}
}

// Eval выполняет одну REPL-строку и возвращает её значение.
//
// `src` должен содержать ровно один top-level стейтмент (repl_line).
// Ошибки парсинга/sema/runtime возвращаются как error.
func (r *REPL) Eval(src string) (runtime.Value, error) {
	prog, err := parser.ParseProgram(parser.ModeRepl, src)
	if err != nil {
		return runtime.Unit, err
	}
	if len(prog.Stmts) == 0 {
		return runtime.Unit, nil
	}
	if len(prog.Stmts) > 1 {
		return runtime.Unit, fmt.Errorf("repl: expected one statement per line")
	}
	stmt := prog.Stmts[0]

	// Контекстный анализ (§F.3).
	semaRes := sema.Check(prog)
	for _, d := range semaRes.Diagnostics {
		sev := "error"
		if d.Severity == sema.SeverityInfo {
			sev = "info"
		}
		fmt.Fprintf(r.out, "%s: <repl>:%d:%d: %s\n", sev, d.Line, d.Col, d.Message)
	}
	if semaRes.HasErrors() {
		return runtime.Unit, fmt.Errorf("sema: %d error(s)", countErrors(semaRes))
	}

	c := compiler.New()
	fn, newName, err := c.CompileReplLine(r.order, stmt)
	if err != nil {
		return runtime.Unit, err
	}

	// Регистрируем вложенные функции (из лямбд/локальных fn) как глобалы,
	// чтобы OpGetGlobal их видел.
	for name, f := range c.Image().Functions {
		r.vm.DefineGlobal(name, vm.FuncValue(f))
	}

	// Аргументы — текущие значения видимых имён.
	args := make([]runtime.Value, len(r.order))
	for i, n := range r.order {
		args[i] = r.env[n]
	}

	res, err := r.vm.RunMainWithArgs(vm.FuncValue(fn), args)
	if err != nil {
		return runtime.Unit, err
	}

	if newName != "" {
		if _, exists := r.env[newName]; !exists {
			r.order = append(r.order, newName)
		}
		r.env[newName] = res
	}
	return res, nil
}

// Bindings возвращает имена в порядке появления (для отладки).
func (r *REPL) Bindings() []string {
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

// Reset очищает состояние.
func (r *REPL) Reset() {
	r.order = nil
	r.env = make(map[string]runtime.Value)
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

// IsContinuation сообщает, нужна ли ещё строка для завершения ввода
// (незакрытые скобки или незакрытая интерполяция).
func IsContinuation(src string) bool {
	return countUnbalanced(src) > 0
}

// countUnbalanced — грубая оценка баланса (), [], {}, %[, %{ с учётом
// строковых литералов и комментариев.
func countUnbalanced(s string) int {
	depth := 0
	inStr := false
	inBytes := false
	esc := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '#' && !inStr && !inBytes {
			break
		}
		if inStr || inBytes {
			if esc {
				esc = false
				continue
			}
			if c == '\\' {
				esc = true
				continue
			}
			if c == '"' {
				inStr = false
				inBytes = false
			}
			continue
		}
		if c == '"' {
			inStr = true
			continue
		}
		if c == 'b' && i+1 < len(s) && s[i+1] == '"' {
			inBytes = true
			i++
			continue
		}
		switch c {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case '%':
			if i+1 < len(s) && (s[i+1] == '[' || s[i+1] == '{') {
				depth++
				i++
			}
		}
	}
	return depth
}

// TrimLeadingPrompt удаляет префикс "> " из строки, если он есть.
func TrimLeadingPrompt(line string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), ">"))
}
