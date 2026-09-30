// Command brig — референсный интерпретатор языка Brig.
//
// Запуск файла — без подкоманды: `brig app.brig [args…]`. Первый аргумент
// с `.brig` или `/` — файл-вход, всё после него — Sys.args(); флаги brig
// принимаются только до файла. Файл с `module` — модуль, вызывается
// fn main(); файл без `module` — script (§11.3). Без аргументов: на TTY —
// REPL, иначе stdin исполняется как script и значения не печатаются.
// `brig -` — программа из stdin, `brig -e expr` — исполнить и выйти.
// `brig -i` — REPL: файлы и каталог проекта загружаются в сессию, main
// не вызывается; `-e` вместе с `-i` исполняется до приглашения.
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

	"github.com/it1ro/brig-lang/internal/ast"
	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/highlight"
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
	inv := parseArgs(os.Args[1:])
	switch inv.kind {
	case kindAuto:
		if term.IsTerminal(os.Stdin) {
			replLoop(inv)
			return
		}
		runScript("<stdin>", readStdin(), nil)
	case kindInteractive:
		replLoop(inv)
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
	kindAuto        = "auto"
	kindInteractive = "interactive"
	kindFile        = "file"
	kindStdin       = "stdin"
	kindEval        = "eval"
	kindCheck       = "check"
	kindTest        = "test"
	kindVersion     = "version"
	kindHelp        = "help"
)

// invocation — разобранная командная строка.
type invocation struct {
	kind     string
	dump     bool
	noInit   bool
	dash     bool // -i -: plain-REPL без приглашений
	expr     string
	file     string
	files    []string
	progArgs []string
}

// parseArgs разбирает argv после имени программы.
//
// Флаги brig — только до файла. Первый аргумент, в котором есть `.brig`
// или `/`, — файл-вход; всё после него — аргументы программы (Sys.args),
// даже если они похожи на флаги. Аргумент без `.brig` и `/` — подкоманда.
// `-` — программа из stdin. `-e expr` — исполнить и выйти.
// `-i` — REPL: идущие подряд файлы и каталоги грузятся в сессию, всё после
// этого списка — Sys.args() по правилу T-207. `-e` вместе с `-i` не
// выходит, а исполняется в сессии до приглашения.
func parseArgs(args []string) invocation {
	if len(args) == 0 {
		return invocation{kind: kindAuto}
	}
	inv := invocation{kind: kindAuto}
	dump := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		// После первого файла -i аргумент, который сам не файл и не
		// каталог, начинает Sys.args() — вместе со всем хвостом, включая
		// флаги (правило T-207). Следующие файлы ещё грузятся.
		if inv.kind == kindInteractive && len(inv.files) > 0 && !isInteractiveTarget(a) {
			inv.progArgs = args[i:]
			return inv
		}
		switch a {
		case "-d", "--dump-bytecode":
			if inv.kind == kindInteractive {
				fail(exitParse, "brig: --dump-bytecode не сочетается с -i")
			}
			dump = true
			continue
		case "--no-init":
			inv.noInit = true
			continue
		case "-i":
			if dump {
				fail(exitParse, "brig: --dump-bytecode не сочетается с -i")
			}
			inv.kind = kindInteractive
			continue
		case "-e":
			if dump {
				fail(exitParse, "brig: --dump-bytecode не сочетается с -e")
			}
			if i+1 >= len(args) {
				fail(exitParse, "brig: -e: ожидается выражение")
			}
			i++
			if inv.kind == kindInteractive {
				inv.expr = args[i]
				continue
			}
			return invocation{kind: kindEval, expr: args[i], progArgs: args[i+1:], noInit: inv.noInit}
		case "-h", "--help":
			return invocation{kind: kindHelp}
		case "-v", "--version":
			return invocation{kind: kindVersion}
		case "-":
			if dump {
				fail(exitParse, "brig: --dump-bytecode не сочетается с -")
			}
			if inv.kind == kindInteractive {
				inv.dash = true
				inv.progArgs = args[i+1:]
				return inv
			}
			return invocation{kind: kindStdin, progArgs: args[i+1:], noInit: inv.noInit}
		}
		if strings.HasPrefix(a, "-") {
			fail(exitParse, "brig: неизвестный флаг %q", a)
		}
		if inv.kind == kindInteractive {
			if isInteractiveTarget(a) {
				inv.files = append(inv.files, a)
				continue
			}
			inv.progArgs = args[i:]
			return inv
		}
		if isEntryFile(a) {
			return invocation{kind: kindFile, dump: dump, file: a, progArgs: args[i+1:], noInit: inv.noInit}
		}
		if dump {
			fail(exitParse, "brig: --dump-bytecode ставится перед файлом")
		}
		return invocation{kind: a, progArgs: args[i+1:]}
	}
	if dump {
		fail(exitParse, "brig: --dump-bytecode: ожидается файл")
	}
	return inv
}

// isInteractiveTarget — аргумент -i грузится в сессию: файл-вход,
// «.» / «..» или существующий каталог.
func isInteractiveTarget(a string) bool {
	if a == "." || a == ".." || isEntryFile(a) {
		return true
	}
	fi, err := os.Stat(a)
	return err == nil && fi.IsDir()
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
  brig -i [файлы|каталог] [args...]
                                  REPL: модули в сессию, main не вызывается
  brig -i -e <expr> ...           expr в сессии после загрузки, до приглашения
  brig -i -                       REPL без приглашений (stdin)
  brig check <file.brig>          распарсить и проверить (парсер + sema)
  brig test [path]                тесты *_test.brig (fn test_*) и доктесты ##
  brig version                    версия
  brig help                       эта справка

Файл — первый аргумент, в котором есть «.brig» или «/». Всё после него —
Sys.args(), включая то, что выглядит как флаги. Флаги brig принимаются
только до файла: -d / --dump-bytecode, -e, -i, --no-init, -h / --help,
-v / --version.

Файл с module — модуль, вызывается fn main(). Файл без module — script
(§11.3): top-level инструкции по порядку, fn main() сама не вызывается.
Подкоманд run и repl нет.

brig check выбирает режим так же и ничего не исполняет. Импорты модуля
ищутся от корня проекта (project.brig вверх от файла; lib/, если он есть),
без project.brig — от каталога файла.

-i грузит перечисленные файлы в сессию: модуль виден по имени, script
исполняется как вводы, fn main() сама не вызывается. Каталог (или «.») —
проект: корень ищется по project.brig вверх от пути. -e вместе с -i
исполняется после загрузки и до первого приглашения. Ошибка загрузки
печатается, REPL всё равно открывается.

Каждый REPL сначала исполняет ~/.config/brig/init.brig
($XDG_CONFIG_HOME/brig/init.brig). --no-init это отключает.

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
	_ = repl.WriteDiagnostics(os.Stderr, file, "", r.Diagnostics, highlight.Palette{}, highlight.Env{}, false)
}

// reportCompileError печатает ошибку лексера, парсера или компилятора в
// формате E.1. Ошибка без позиции получает 1:1.
func reportCompileError(file string, err error) {
	f, line, col, msg, _ := repl.DescribeCompileError(file, err)
	fmt.Fprintln(os.Stderr, repl.FormatE1("error", f, line, col, msg))
}

// loadProgram загружает граф модулей от входного файла (§11.1, T-135)
// и прогоняет sema по каждому модулю. Ошибка загрузки или sema —
// сообщение E.1 и выход; ошибка чтения входного файла — exitInternal.
// Корень импортов — каталог файла.
func loadProgram(file string) *loader.Graph { return loadProgramFrom("", file) }

// loadProgramFrom — loadProgram с корнем импортов root ("" — каталог файла).
func loadProgramFrom(root, file string) *loader.Graph {
	g, err := loader.LoadFrom(root, file)
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

// runCheck: brig check <file.brig> — лексинг + парсинг + sema, без
// исполнения. Режим — как у brig <file> (§11.3): файл с module — граф
// модулей от корня проекта (loader.ModuleRoot), без module — script.
func runCheck(args []string) {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "brig check: ожидается один файл")
		os.Exit(exitParse)
	}
	file := args[0]
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
		loadProgramFrom(loader.ModuleRoot(file), file)
	} else {
		checkScript(file, string(src))
	}
	fmt.Printf("%s: ok\n", file)
}

// checkScript — sema script-файла так же, как при его исполнении
// (runScript): каждая top-level инструкция — отдельный ввод (§11.3).
// Плюс info о fn main(), которую script не вызывает. Ошибка — выход.
func checkScript(file, src string) {
	lines, err := parser.ParseReplInput(src)
	if err != nil {
		reportCompileError(file, err)
		os.Exit(exitForCompileErr(err))
	}
	failed := false
	whole := &ast.Program{}
	for _, line := range lines {
		whole.Decls = append(whole.Decls, line.Decls...)
		whole.Stmts = append(whole.Stmts, line.Stmts...)
		if len(line.Stmts) == 0 {
			continue
		}
		res := sema.CheckRepl(line, nil)
		reportDiagnostics(file, res)
		if res.HasErrors() {
			failed = true
		}
	}
	if failed {
		os.Exit(exitParse)
	}
	reportDiagnostics(file, sema.CheckScriptMain(whole))
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

	machine := newMachine()
	machine.SetArgs(progArgs)
	if err := compiler.InstallStdlib(machine); err != nil {
		fail(exitInternal, "brig: %v", err)
	}
	for name, fn := range img.Functions {
		machine.DefineGlobal(name, vm.FuncValue(fn))
	}
	mainVal := machine.Global("main")
	if _, err := machine.RunMain(mainVal); err != nil {
		exitHalt(err)
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
	machine := newMachine()
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
	exitHalt(err)
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
