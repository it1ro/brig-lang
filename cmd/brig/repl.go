package main

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/it1ro/brig-lang/internal/highlight"
	"github.com/it1ro/brig-lang/internal/repl"
	"github.com/it1ro/brig-lang/internal/repl/term"
	"github.com/it1ro/brig-lang/internal/vm"
)

const (
	replBanner = "brig: введите выражение; пустая строка закрывает блок; Ctrl-D — выход"
	contPrompt = "   ...> "
)

func prompt(next int) string { return fmt.Sprintf("brig[%d]> ", next) }

// replLoop — REPL (§11.4, N12) поверх repl.Session.
//
// Ввод продолжается, пока repl.NeedMore: открытые скобки и литералы,
// заголовок блока, открытый offside-блок (его закрывает пустая строка).
// На терминале — редактор строки с историей (consoleLoop); без TTY
// (`brig < file` без аргументов) сюда не попадает: stdin исполняется
// как script и значения не печатаются.
func replLoop() {
	s := repl.New(vm.New(), os.Stderr)
	defer s.Close()
	// Во время ввода терминал в raw mode, и Ctrl-C — байт редактора.
	// Пока ввод исполняется, терминал обычный: Ctrl-C приходит как SIGINT
	// и снимает вычисление, не убивая процесс и не актор сессии (§11.4).
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	defer signal.Stop(sig)
	go func() {
		for range sig {
			s.Interrupt()
		}
	}()
	tty := term.IsTerminal(os.Stdin)
	var err error
	if tty && term.IsTerminal(os.Stdout) {
		err = consoleLoop(s)
	} else {
		fe := repl.Plain{In: os.Stdin, Out: os.Stdout, Err: os.Stderr}
		if tty {
			fmt.Fprintln(os.Stderr, replBanner)
			fe.Prompt = func(next int, more bool) string {
				if more {
					return contPrompt
				}
				return prompt(next)
			}
		}
		err = fe.Run(s)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(exitInternal)
	}
	if tty {
		fmt.Fprintln(os.Stderr, "bye")
	}
}

// consoleLoop — REPL на терминале: редактор строки (term.Terminal) и
// история в term.DefaultHistoryPath; значения и ошибки печатаются так
// же, как в plain-фронтенде.
func consoleLoop(s *repl.Session) error {
	fmt.Fprintln(os.Stderr, replBanner)
	t := term.NewTerminal(os.Stdin, os.Stdout)
	t.NeedMore = s.NeedMore
	t.Indent = repl.Indent
	t.IndentWidth = repl.IndentWidth
	t.History = openHistory()
	pal := highlight.PaletteFromEnv(nil)
	t.Highlight = func(src string, cursor int) string {
		return highlight.Highlight(src, cursor, s.HighlightEnv(), pal)
	}

	fe := repl.Plain{Out: os.Stdout, Err: os.Stderr}
	s.SetOutput(os.Stderr)
	for {
		src, err := t.ReadInput(prompt(s.Next()), contPrompt)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if strings.TrimSpace(src) == "" {
			continue
		}
		if err := t.History.Add(src); err != nil {
			fmt.Fprintf(os.Stderr, "warning: history: %v\n", err)
			t.History.Path = ""
		}
		if err := fe.Eval(s, src); err != nil {
			return err
		}
	}
}

// openHistory загружает историю консоли. Файл недоступен — предупреждение
// и история только в памяти.
func openHistory() *term.History {
	path, err := term.DefaultHistoryPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: history: %v\n", err)
		return &term.History{}
	}
	h, err := term.LoadHistory(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: history: %v\n", err)
		h.Path = ""
	}
	return h
}
