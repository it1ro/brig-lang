package main

import (
	"fmt"
	"os"

	"github.com/it1ro/brig-lang/internal/repl"
	"github.com/it1ro/brig-lang/internal/vm"
)

// replLoop — REPL (§11.4, N12) поверх repl.Session.
//
// Ввод продолжается, пока repl.NeedMore: открытые скобки и литералы,
// заголовок блока, открытый offside-блок (его закрывает пустая строка).
// Без TTY (`brig repl < file`) — plain-фронтенд без приглашений.
func replLoop() {
	s := repl.New(vm.New(), os.Stderr)
	fe := repl.Plain{In: os.Stdin, Out: os.Stdout, Err: os.Stderr}
	tty := isTerminal(os.Stdin)
	if tty {
		fmt.Fprintln(os.Stderr, "brig repl (persistent): введите выражение; пустая строка закрывает блок; Ctrl-D — выход")
		fe.Prompt = func(next int, more bool) string {
			if more {
				return "   ...> "
			}
			return fmt.Sprintf("brig[%d]> ", next)
		}
	}
	if err := fe.Run(s); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(exitInternal)
	}
	if tty {
		fmt.Fprintln(os.Stderr, "bye")
	}
}

// isTerminal — f подключён к терминалу (символьное устройство).
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
