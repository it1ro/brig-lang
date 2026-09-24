// Command brig — референсный интерпретатор языка Brig.
//
// Подкоманды: check (парсинг без исполнения), run (входная точка §9),
// repl (отладочный цикл Трек B), version.
package main

import (
	"fmt"
	"os"

	"github.com/it1ro/brig-lang/internal/parser"
)

const version = "0.0.1-dev"

// Exit codes (см. скилл brig-cli):
//
//	0 — ok
//	1 — ошибка парсинга / семантической проверки
//	2 — runtime uncaught raise (VM не реализована — пока недостижим)
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
		fmt.Fprintln(os.Stderr, "run: VM не реализована (Трек B). Используйте: brig check <file.brig>")
		os.Exit(exitInternal)
	case "repl":
		runRepl(args[1:])
	case "version", "--version", "-v":
		fmt.Printf("brig %s\n", version)
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "brig: неизвестная команда %q\n\n", args[0])
		usage()
		os.Exit(exitParse)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `brig — референсный интерпретатор

Использование:
  brig check <file.brig>    распарсить и проверить, не исполняя
  brig run   <file.brig>    выполнить модуль (TODO: Трек B)
  brig repl                 интерактивный режим (отладочный, Трек B)
  brig version              версия

Exit codes: 0 ok, 1 ошибка парсинга, 2 runtime raise, 3 внутренняя ошибка.
`)
}

// runCheck: brig check <file.brig> — лексинг + парсинг (этап 1: инварианты,
// строгий recursive descent — этап 2 Трек B).
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

// runRepl: отладочный REPL Трек B — по строке выводит токены лексера.
// Полноценная семантика §10.7 (снимок связываний на строку, N12) — после VM.
func runRepl(args []string) {
	if len(args) != 0 {
		fmt.Fprintln(os.Stderr, "brig repl: аргументы не принимаются")
		os.Exit(exitParse)
	}
	replLoop()
}
