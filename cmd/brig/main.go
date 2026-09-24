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

	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/parser"
	"github.com/it1ro/brig-lang/internal/vm"
)

// version поднят до 0.1.0-dev: появился исполняемый пайплайн Трека C.
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

// runFile: brig run <file.brig> — полный пайплайн Трека C (вертикальный срез).
//
// Лексер → парсер → компилятор → стековая ВМ. Точка входа — `fn main()`.
// Осознанное отступление от §15.1: ВМ стековая, не регистровая — ради
// быстрого получения исполняемого пайплайна. Миграция на регистровую — отдельный подэтап.
func runFile(args []string) {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "brig run: ожидается один файл")
		os.Exit(exitParse)
	}
	src, err := os.ReadFile(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "brig run: %v\n", err)
		os.Exit(exitInternal)
	}

	// 1. Парсинг (режим module: top-level только декларации).
	prog, err := parser.ParseProgram(parser.ModeModule, string(src))
	if err != nil {
		fmt.Fprintf(os.Stderr, "brig run: %s: %v\n", args[0], err)
		os.Exit(exitParse)
	}

	// 2. Компиляция AST → байткод.
	img, err := compiler.New().Compile(prog)
	if err != nil {
		fmt.Fprintf(os.Stderr, "brig run: %s: compile: %v\n", args[0], err)
		os.Exit(exitInternal)
	}
	if img.Main == nil {
		fmt.Fprintf(os.Stderr, "brig run: %s: нет функции main()\n", args[0])
		os.Exit(exitParse)
	}

	// 3. Загрузка в ВМ: все функции модуля + прелюдия (ставится в vm.New()).
	machine := vm.New()
	for name, fn := range img.Functions {
		machine.DefineGlobal(name, vm.FuncValue(fn))
	}

	// 4. Исполнение точки входа.
	mainVal := machine.Global("main")
	if _, err := machine.Call(mainVal, nil); err != nil {
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
