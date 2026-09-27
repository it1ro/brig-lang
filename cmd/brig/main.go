// Command brig — референсный интерпретатор языка Brig.
//
// Запуск файла — без подкоманды: `brig app.brig [args…]`. Первый аргумент
// с `.brig` или `/` — файл-вход, всё после него — Sys.args(); флаги brig
// принимаются только до файла. Файл с `module` — модуль, вызывается
// fn main(); файл без `module` — script (§11.3). Без аргументов: на TTY —
// REPL, иначе stdin исполняется как script и значения не печатаются.
// `brig -` — программа из stdin, `brig -e expr` — исполнить и выйти.
//
// Подкоманды: check, test, version, help. Подкоманд run и repl нет.
//
// Переменные окружения:
//   - BRIG_VERIFY=1 — прогнать vm.Verify по всем функциям перед исполнением.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/lexer"
	"github.com/it1ro/brig-lang/internal/loader"
	"github.com/it1ro/brig-lang/internal/parser"
	"github.com/it1ro/brig-lang/internal/repl"
	"github.com/it1ro/brig-lang/internal/repl/term"
	"github.com/it1ro/brig-lang/internal/sema"
	"github.com/it1ro/brig-lang/internal/vm"
)

const version = "0.1.0-dev"

// Exit codes:
//
//	0 — ok
//	1 — ошибка парсинга / семантической проверки
//	2 — runtime uncaught raise
//	3 — внутренняя ошибка / not implemented
const (
	exitOK       = 0
	exitParse    = 1
	exitRuntime  = 2
	exitInternal = 3
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		if term.IsTerminal(os.Stdin) {
			replLoop()
			return
		}
		runScript("<stdin>", readStdin(), nil)
		return
	}

	inv := parseArgs(args)
	switch inv.kind {
	case kindFile:
		runEntry(inv.file, inv.progArgs, inv.dump)
	case kindStdin:
		runScript("-", readStdin(), inv.progArgs)
	case kindEval:
		runScript("<eval>", inv.expr, inv.progArgs)
	case kindCheck:
		runCheck(inv.progArgs)
	case kindTest:
		runTest(inv.progArgs)
	case kindVersion:
		fmt.Printf("brig %s\n", version)
	case kindHelp:
		usage()
	default:
		unknownCommand(inv.kind, inv.progArgs)
	}
}

const (
	kindFile    = "file"
	kindStdin   = "stdin"
	kindEval    = "eval"
	kindCheck   = "check"
	kindTest    = "test"
	kindVersion = "version"
	kindHelp    = "help"
)

// invocation — разобранная командная строка.
type invocation struct {
	kind     string
	dump     bool
	expr     string
	file     string
	progArgs []string
}

// parseArgs разбирает argv после имени программы.
//
// Флаги brig — только до файла. Первый аргумент, в котором есть `.brig`
// или `/`, — файл-вход; всё после него — аргументы программы (Sys.args),
// даже если они похожи на флаги. Аргумент без `.brig` и `/` — подкоманда.
// `-` — программа из stdin. `-e expr` — исполнить и выйти.
func parseArgs(args []string) invocation {
	dump := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "-d", "--dump-bytecode":
			dump = true
			continue
		case "-e":
			if dump {
				fail(exitParse, "brig: --dump-bytecode не сочетается с -e")
			}
			if i+1 >= len(args) {
				fail(exitParse, "brig: -e: ожидается выражение")
			}
			return invocation{kind: kindEval, expr: args[i+1], progArgs: args[i+2:]}
		case "-h", "--help":
			return invocation{kind: kindHelp}
		case "-v", "--version":
			return invocation{kind: kindVersion}
		case "-":
			if dump {
				fail(exitParse, "brig: --dump-bytecode не сочетается с -")
			}
			return invocation{kind: kindStdin, progArgs: args[i+1:]}
		}
		if strings.HasPrefix(a, "-") {
			fail(exitParse, "brig: неизвестный флаг %q", a)
		}
		if isEntryFile(a) {
			return invocation{kind: kindFile, dump: dump, file: a, progArgs: args[i+1:]}
		}
		if dump {
			fail(exitParse, "brig: --dump-bytecode ставится перед файлом")
		}
		return invocation{kind: a, progArgs: args[i+1:]}
	}
	if dump {
		fail(exitParse, "brig: --dump-bytecode: ожидается файл")
	}
	fail(exitParse, "brig: ожидается файл или подкоманда")
	return invocation{}
}

// isEntryFile — аргумент является файлом-входом, а не подкомандой.
func isEntryFile(a string) bool {
	return strings.Contains(a, ".brig") || strings.Contains(a, "/")
}

func unknownCommand(name string, rest []string) {
	fmt.Fprintf(os.Stderr, "brig: неизвестная команда %q\n", name)
	if len(rest) > 0 {
		fmt.Fprintf(os.Stderr, "подсказка: brig %s\n", strings.Join(rest, " "))
	}
	usage()
	os.Exit(exitParse)
}

func fail(code int, format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(code)
}

func usage() {
	fmt.Fprint(os.Stderr, `brig — референсный интерпретатор

Использование:
  brig [флаги] <file> [args...]   выполнить файл
  brig -e <expr> [args...]        исполнить выражение и выйти
  brig - [args...]                программа из stdin
  brig                            на TTY — REPL; иначе stdin как script
  brig check <file.brig>          распарсить и проверить (парсер + sema)
  brig test [path]                тесты *_test.brig (fn test_*) и доктесты ##
  brig version                    версия
  brig help                       эта справка

Файл — первый аргумент, в котором есть «.brig» или «/». Всё после него —
Sys.args(), включая то, что выглядит как флаги. Флаги brig принимаются
только до файла: -d / --dump-bytecode, -e, -h / --help, -v / --version.

Файл с module — модуль, вызывается fn main(). Файл без module — script
(§11.3): top-level инструкции по порядку, fn main() сама не вызывается.
Подкоманд run и repl нет.

Переменные окружения:
  BRIG_VERIFY=1                   прогнать vm.Verify перед исполнением

Exit codes: 0 ok, 1 ошибка парсинга/sema, 2 runtime raise, 3 внутренняя ошибка;
brig test: 0 — всё прошло, 1 — упал тест или файл не скомпилировался.
`)
}

func readStdin() string {
	b, err := io.ReadAll(os.Stdin)
	if err != nil {
		fail(exitInternal, "brig: %v", err)
	}
	return string(b)
}

// reportDiagnostics печатает диагностики sema в формате E.1:
//
//	error: <file>:<line>:<col>: <message>
//	info:  <file>:<line>:<col>: <message>
//
// info не влияет на exit code.
func reportDiagnostics(file string, r *sema.Result) {
	for _, d := range r.Diagnostics {
		sev := "error"
		if d.Severity == sema.SeverityInfo {
			sev = "info"
		}
		fmt.Fprintf(os.Stderr, "%s: %s:%d:%d: %s\n",
			sev, file, d.Line, d.Col, d.Message)
	}
}

// reportCompileError печатает ошибку лексера, парсера или компилятора в
// формате E.1. Ошибка без позиции получает 1:1.
func reportCompileError(file string, err error) {
	line, col, msg := 1, 1, err.Error()
	var le *lexer.Error
	var pe *parser.Error
	var ce *compiler.Error
	switch {
	case errors.As(err, &le):
		line, col, msg = le.Line, le.Col, le.Msg
	case errors.As(err, &pe):
		line, col, msg = pe.Line, pe.Col, pe.Msg
	case errors.As(err, &ce):
		if ce.Line > 0 {
			line, col = ce.Line, ce.Col
		}
		if ce.File != "" {
			file = ce.File
		}
		msg = ce.Msg
	}
	fmt.Fprintf(os.Stderr, "error: %s:%d:%d: %s\n", file, line, col, msg)
}

// loadProgram загружает граф модулей от входного файла (§11.1, T-135)
// и прогоняет sema по каждому модулю. Ошибка загрузки или sema —
// сообщение E.1 и выход; ошибка чтения входного файла — exitInternal.
func loadProgram(file string) *loader.Graph {
	g, err := loader.Load(file)
	if err != nil {
		var le *loader.Error
		if errors.As(err, &le) {
			fmt.Fprintf(os.Stderr, "error: %v\n", le)
			os.Exit(exitParse)
		}
		fmt.Fprintf(os.Stderr, "brig: %v\n", err)
		os.Exit(exitInternal)
	}
	mods := make([]sema.Module, len(g.Modules))
	for i, m := range g.Modules {
		mods[i] = sema.Module{Name: m.Name, Prog: m.Prog}
	}
	world := sema.NewWorld(mods)
	failed := false
	for _, m := range g.Modules {
		// CheckNames включает проверки Check и разрешение имён (§F.3, T-139).
		semaRes := sema.CheckNames(m.Prog, world)
		reportDiagnostics(m.Path, semaRes)
		if semaRes.HasErrors() {
			failed = true
		}
	}
	if failed {
		os.Exit(exitParse)
	}
	return g
}

// runCheck: brig check <file.brig> — лексинг + парсинг + sema по всем
// модулям графа.
func runCheck(args []string) {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "brig check: ожидается один файл")
		os.Exit(exitParse)
	}
	loadProgram(args[0])
	fmt.Printf("%s: ok\n", args[0])
}

// runEntry запускает файл: с `module` — модуль и fn main(), без — script.
func runEntry(file string, progArgs []string, dump bool) {
	src, err := os.ReadFile(file)
	if err != nil {
		fail(exitInternal, "brig: %v", err)
	}
	mod, err := sourceIsModule(src)
	if err != nil {
		reportCompileError(file, err)
		os.Exit(exitForCompileErr(err))
	}
	if mod {
		runModule(file, progArgs, dump)
		return
	}
	if dump {
		fail(exitParse, "brig: --dump-bytecode требует файл с module")
	}
	runScript(file, string(src), progArgs)
}

// sourceIsModule — первый значимый токен файла это `module` (§11.3).
// Shebang и прочие комментарии лексер снимает до разбора.
func sourceIsModule(src []byte) (bool, error) {
	toks, err := lexer.Lex(string(src))
	if err != nil {
		return false, err
	}
	for _, t := range toks {
		if t.Type == lexer.NEWLINE || t.Type == lexer.EOF {
			continue
		}
		return t.Type == lexer.KW_MODULE, nil
	}
	return false, nil
}

// runModule — файл с module: loader, sema, компилятор, fn main().
func runModule(file string, progArgs []string, dump bool) {
	g := loadProgram(file)
	mods := make([]compiler.Module, len(g.Modules))
	for i, m := range g.Modules {
		mods[i] = compiler.Module{Name: m.Name, Path: m.Path, Prog: m.Prog}
	}

	img, err := compiler.New().CompileProgram(mods)
	if err != nil {
		reportCompileError(file, err)
		os.Exit(exitForCompileErr(err))
	}
	if img.Main == nil {
		reportCompileError(file, errors.New("нет функции main()"))
		os.Exit(exitParse)
	}

	if os.Getenv("BRIG_VERIFY") == "1" {
		names := make([]string, 0, len(img.Functions))
		for name := range img.Functions {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if verr := vm.Verify(img.Functions[name].Chunk); verr != nil {
				fmt.Fprintf(os.Stderr, "brig: verify %s: %v\n", name, verr)
				os.Exit(exitInternal)
			}
		}
	}

	if dump {
		names := make([]string, 0, len(img.Functions))
		for name := range img.Functions {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			fmt.Print(img.Functions[name].Disassemble())
		}
		return
	}

	machine := vm.New()
	machine.SetArgs(progArgs)
	for name, fn := range img.Functions {
		machine.DefineGlobal(name, vm.FuncValue(fn))
	}
	mainVal := machine.Global("main")
	if _, err := machine.RunMain(mainVal); err != nil {
		exitRaise(file, err)
	}
}

// runScript исполняет текст как script (§11.3): инструкции по порядку,
// каждая — своя область, как вводы REPL. Значения не печатаются.
// Непойманный raise останавливает скрипт. Модуль Repl сюда не подмешивается:
// файл исполняется вне консоли.
func runScript(name, src string, progArgs []string) {
	if os.Getenv("BRIG_VERIFY") == "1" {
		compiler.Verify = true
	}
	machine := vm.New()
	machine.SetArgs(progArgs)
	s := repl.New(machine, os.Stderr)
	defer s.Close()
	s.SetDiagFile(name)
	if _, err := s.Eval(src); err != nil {
		exitScript(name, err)
	}
}

// exitScript переводит ошибку script-ввода в exit-код CLI.
// Диагностику sema сессия уже напечатала.
func exitScript(name string, err error) {
	if strings.HasPrefix(err.Error(), "sema:") {
		os.Exit(exitParse)
	}
	var rerr *vm.ErrRaise
	if errors.As(err, &rerr) {
		exitRaise(name, err)
	}
	if isInternalErr(err) {
		fail(exitInternal, "brig: %s: %v", name, err)
	}
	reportCompileError(name, err)
	os.Exit(exitForCompileErr(err))
}

// exitRaise печатает непойманный raise и stack trace и выходит.
func exitRaise(name string, err error) {
	fmt.Fprintf(os.Stderr, "brig: %s: %v\n", name, err)
	var rerr *vm.ErrRaise
	if errors.As(err, &rerr) {
		for _, fr := range rerr.Trace {
			file := fr.File
			if file == "" {
				file = name
			}
			fmt.Fprintf(os.Stderr, "  at %s (%s:%d:%d)\n", fr.Func, file, fr.Pos.Line, fr.Pos.Col)
		}
	}
	os.Exit(exitForRunErr(err))
}

// exitForCompileErr maps compile failures to CLI exit codes (A-F7 / T-45).
// User-facing compile errors including «срез: …» → exitParse; messages with
// an "internal:" prefix → exitInternal.
func exitForCompileErr(err error) int {
	if isInternalErr(err) {
		return exitInternal
	}
	return exitParse
}

// exitForRunErr maps RunMain failures to CLI exit codes (A-F7 / T-45).
// Errors whose message has an "internal:" prefix → exitInternal; a real
// uncaught raise → exitRuntime.
func exitForRunErr(err error) int {
	if isInternalErr(err) {
		return exitInternal
	}
	return exitRuntime
}

func isInternalErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "internal:")
}
