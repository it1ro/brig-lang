// Command brig — референсный интерпретатор языка Brig.
//
// Подкоманды:
//   - check — парсинг без исполнения (Трек B);
//   - run   — полный пайплайн: парсер → компилятор → стековая ВМ (Трек C, вертикальный срез);
//   - repl  — отладочный цикл (токены лексера);
//   - version / help.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/parser"
	"github.com/it1ro/brig-lang/internal/vm"
)

// version поднят до 0.1.0-dev: появился исполняемый пайплайн Трека C.
// v0.4.8: акторы — scheduler loop, spawn/send/recv/watch.
const version = "0.1.0-dev"

// Exit codes (см. скилл brig-cli):
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
  brig check <file.brig>    распарсить и проверить, не исполняя
  brig run   <file.brig>    выполнить модуль (Трек C: компилятор + стековая ВМ)
  brig repl                 интерактивный режим (отладочный)
  brig version              версия

Exit codes: 0 ok, 1 ошибка парсинга, 2 runtime raise, 3 внутренняя ошибка.
`)
}

// runCheck: brig check <file.brig> — лексинг + парсинг (Трек B).
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
	if err := parser.Parse(parser.ModeModule, string(src)); err != nil {
		fmt.Fprintf(os.Stderr, "brig check: %s: %v\n", args[0], err)
		os.Exit(exitParse)
	}
	fmt.Printf("%s: ok\n", args[0])
}

// runFile: brig run [--dump-bytecode] <file.brig>
//
// Полный пайплайн Трека C. С --dump-bytecode печатает дизассемблированный
// байткод всех функций модуля и не исполняет — основной инструмент
// отладки компилятора (§15.1 Must).
//
// v0.4.8: main запускается как актор через vm.RunMain (scheduler loop).
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
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "brig run: ожидается один файл (опционально --dump-bytecode)")
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
	img, err := compiler.New().Compile(prog)
	if err != nil {
		fmt.Fprintf(os.Stderr, "brig run: %s: compile: %v\n", args[0], err)
		os.Exit(exitInternal)
	}
	if img.Main == nil {
		fmt.Fprintf(os.Stderr, "brig run: %s: нет функции main()\n", args[0])
		os.Exit(exitParse)
	}

	// --dump-bytecode: печатаем все функции и выходим.
	if dump {
		for name, fn := range img.Functions {
			fmt.Print(fn.Chunk.Disassemble(name))
		}
		return
	}

	machine := vm.New()
	for name, fn := range img.Functions {
		machine.DefineGlobal(name, vm.FuncValue(fn))
	}
	mainVal := machine.Global("main")
	if _, err := machine.RunMain(mainVal); err != nil {
		fmt.Fprintf(os.Stderr, "brig run: %s: %v\n", args[0], err)
		os.Exit(exitRuntime)
	}
}

// runRepl: отладочный REPL — по строке выводит токены лексера.
// Полноценная семантика §10.7 (снимок связываний на строку, N12) — после расширения ВМ.
func runRepl(args []string) {
	if len(args) != 0 {
		fmt.Fprintln(os.Stderr, "brig repl: аргументы не принимаются")
		os.Exit(exitParse)
	}
	replLoop()
}
