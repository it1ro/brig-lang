// Command brig — референсный интерпретатор языка Brig.
//
// Подкоманды:
//   - check — парсинг + контекстный анализ без исполнения;
//   - run   — полный пайплайн: парсер → sema → компилятор → регистровая ВМ;
//   - repl  — persistent REPL (§11.4, N12);
//   - version / help.
//
// Переменные окружения:
//   - BRIG_VERIFY=1 — прогнать vm.Verify по всем функциям перед RunMain.
package main

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/parser"
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
		usage()
		os.Exit(exitParse)
	}

	switch args[0] {
	case "check":
		runCheck(args[1:])
	case "run":
		runFile(args[1:])
	case "repl":
		runRepl(args[1:])
	case "version", "--version", "-v":
		fmt.Printf("brig %s\n", version)
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "brig: неизвестная команда %q\n", args[0])
		usage()
		os.Exit(exitParse)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `brig — референсный интерпретатор

Использование:
  brig check <file.brig>    распарсить и проверить (парсер + sema)
  brig run   <file.brig>    выполнить модуль (парсер + sema + компилятор + ВМ)
  brig repl                 интерактивный режим (отладочный)
  brig version              версия

Переменные окружения:
  BRIG_VERIFY=1             прогнать vm.Verify перед RunMain

Exit codes: 0 ok, 1 ошибка парсинга/sema, 2 runtime raise, 3 внутренняя ошибка.
`)
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

// runCheck: brig check <file.brig> — лексинг + парсинг + sema.
func runCheck(args []string) {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "brig check: ожидается один файл")
		os.Exit(exitParse)
	}
	src, err := os.ReadFile(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "brig check: %v\n", err)
		os.Exit(exitInternal)
	}
	prog, err := parser.ParseProgram(parser.ModeModule, string(src))
	if err != nil {
		fmt.Fprintf(os.Stderr, "brig check: %s: %v\n", args[0], err)
		os.Exit(exitParse)
	}
	semaRes := sema.Check(prog)
	reportDiagnostics(args[0], semaRes)
	if semaRes.HasErrors() {
		os.Exit(exitParse)
	}
	fmt.Printf("%s: ok\n", args[0])
}

// runFile: brig run [--dump-bytecode] <file.brig>
func runFile(args []string) {
	var dump bool
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "--dump-bytecode", "-d":
			dump = true
			args = args[1:]
		default:
			fmt.Fprintf(os.Stderr, "brig run: неизвестный флаг %q\n", args[0])
			os.Exit(exitParse)
		}
	}
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "brig run: ожидается файл (опционально --dump-bytecode) и аргументы программы")
		os.Exit(exitParse)
	}
	src, err := os.ReadFile(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "brig run: %v\n", err)
		os.Exit(exitInternal)
	}

	prog, err := parser.ParseProgram(parser.ModeModule, string(src))
	if err != nil {
		fmt.Fprintf(os.Stderr, "brig run: %s: %v\n", args[0], err)
		os.Exit(exitParse)
	}

	// Контекстный анализ (§F.3) — до компиляции.
	semaRes := sema.Check(prog)
	reportDiagnostics(args[0], semaRes)
	if semaRes.HasErrors() {
		os.Exit(exitParse)
	}

	img, err := compiler.New().Compile(prog)
	if err != nil {
		fmt.Fprintf(os.Stderr, "brig run: %s: compile: %v\n", args[0], err)
		os.Exit(exitForCompileErr(err))
	}
	if img.Main == nil {
		fmt.Fprintf(os.Stderr, "brig run: %s: нет функции main()\n", args[0])
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
				fmt.Fprintf(os.Stderr,
					"brig run: verify %s: %v\n", name, verr)
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
	machine.SetArgs(args[1:])
	for name, fn := range img.Functions {
		machine.DefineGlobal(name, vm.FuncValue(fn))
	}
	mainVal := machine.Global("main")
	if _, err := machine.RunMain(mainVal); err != nil {
		fmt.Fprintf(os.Stderr, "brig run: %s: %v\n", args[0], err)
		var rerr *vm.ErrRaise
		if errors.As(err, &rerr) {
			for _, fr := range rerr.Trace {
				fmt.Fprintf(os.Stderr, "  at %s (%s:%d:%d)\n", fr.Func, args[0], fr.Pos.Line, fr.Pos.Col)
			}
		}
		os.Exit(exitForRunErr(err))
	}
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

// runRepl: persistent REPL (§11.4, N12).
func runRepl(args []string) {
	if len(args) != 0 {
		fmt.Fprintln(os.Stderr, "brig repl: аргументы не принимаются")
		os.Exit(exitParse)
	}
	replLoop()
}
