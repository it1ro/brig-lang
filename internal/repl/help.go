package repl

import (
	"fmt"
	"sort"
	"strings"

	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/sema"
	"github.com/it1ro/brig-lang/internal/vm"
)

// ConsolePaths — файлы консоли для h(): история и init.brig. Пустое поле
// в справке не печатается.
type ConsolePaths struct {
	History string
	Init    string
}

// SetConsolePaths задаёт пути, которые печатает h().
func (s *Session) SetConsolePaths(p ConsolePaths) { s.paths = p }

// consoleKeys — клавиши редактора консоли для h() и brig help.
const consoleKeys = `Keys:
  Enter          run the input; inside an open block, a new line
  Alt-Enter      run the whole input from any position
  Tab            complete a name; in leading spaces, indent one level
  Shift-Tab      remove one indent level
  Up, Down       history; on a non-empty line, search by prefix
  Alt-P, Alt-N   history search by prefix
  Ctrl-R         search history; Esc or Enter accepts the match
  Right, End     accept the history hint
  Ctrl-A, Ctrl-E line start, line end; Alt-<, Alt-> input start, end
  Alt-B, Alt-F   word back, word forward
  Ctrl-K, Ctrl-U, Ctrl-W, Alt-D, Alt-Backspace
                 kill to line end, to line start, word back, word forward
  Ctrl-Y, Alt-Y  yank, yank previous kill
  Ctrl-_         undo
  Ctrl-T         transpose characters
  Alt-.          insert the last argument of the previous input
  Ctrl-X Ctrl-E  edit the input in $EDITOR
  Ctrl-L         clear the screen
  Ctrl-C         clear the input; during evaluation, interrupt it
  Ctrl-D         exit on an empty line
`

// consoleEnv — переменные окружения консоли для h() и brig help.
const consoleEnv = `Environment:
  BRIG_THEME     auto (default), dark or light: palette for the background
  BRIG_COLORS    per-class SGR codes over the theme, e.g. number=34:atom=36
  NO_COLOR       non-empty value disables color
  FORCE_COLOR    non-empty value enables color outside a terminal
`

// ConsoleHelp — клавиши и переменные окружения консоли (brig help).
func ConsoleHelp() string { return consoleKeys + "\n" + consoleEnv }

// generalHelp — h(): хелперы Repl с арностями, клавиши, окружение, файлы.
func (s *Session) generalHelp() string {
	var b strings.Builder
	b.WriteString("Helpers (also as Repl.name):\n")
	arities := sema.BuiltinArities()["Repl"]
	names := make([]string, 0, len(arities))
	for name := range arities {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		sig := name + "/" + strings.Join(arities[name], ",")
		fmt.Fprintf(&b, "  %-14s %s\n", sig, helperSummary[name])
	}
	b.WriteString("h(f) or h(Module) shows the documentation of a function or a module.\n\n")
	b.WriteString(consoleKeys)
	b.WriteString("\n")
	b.WriteString(consoleEnv)
	if s.paths.History != "" || s.paths.Init != "" {
		b.WriteString("\nFiles:\n")
		if s.paths.History != "" {
			fmt.Fprintf(&b, "  history        %s\n", s.paths.History)
		}
		if s.paths.Init != "" {
			fmt.Fprintf(&b, "  init           %s\n", s.paths.Init)
		}
	}
	return b.String()
}

// helperSummary — одна строка о хелпере в h().
var helperSummary = map[string]string{
	"h":         "help: this text, or the docs of a function or module",
	"i":         "kind and size of a value; for a pid, alive and mailbox",
	"v":         "value of input n; v() is the last value",
	"bindings":  "session bindings as a map",
	"reset":     "drop session bindings; actors and modules stay",
	"load":      "load a file into the session",
	"flush":     "print and take the messages of the session mailbox",
	"time":      "call f() and return (microseconds, result)",
	"dis":       "disassemble a function",
	"recompile": "recompile loaded user modules",
	"register":  "Repl.register(\"M\"): pub functions of M as helpers",
	"tree":      "supervision tree of actors",
	"info":      "print Actor.info(pid)",
	"top":       "n live actors with the most reductions",
	"observe":   "full-screen actor observer",
}

// raiseClause — (:function_clause, args) хелпера с неподходящей арностью.
func raiseClause(args []runtime.Value) error {
	return &vm.ErrRaise{Val: runtime.Tuple(
		runtime.Atom("function_clause"),
		runtime.List(append([]runtime.Value(nil), args...)...))}
}
