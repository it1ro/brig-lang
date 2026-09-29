package main

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
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
func replLoop(inv invocation) {
	machine := vm.New()
	machine.SetSignals(osSignals{skip: map[string]bool{"sigint": true}})
	machine.SetArgs(inv.progArgs)
	s := repl.New(machine, os.Stderr)
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
	if !inv.noInit {
		runInit(s)
	}
	for _, path := range inv.files {
		if err := loadTarget(s, path); err != nil {
			break
		}
	}
	fe := repl.Plain{Out: os.Stdout, Err: os.Stderr}
	if inv.expr != "" {
		if err := fe.Eval(s, inv.expr); err != nil {
			exitHalt(err)
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(exitInternal)
		}
	}

	tty := term.IsTerminal(os.Stdin)
	plain := inv.dash || !tty || !term.IsTerminal(os.Stdout)
	var err error
	if !plain {
		err = consoleLoop(s)
	} else {
		fe.In = os.Stdin
		if tty && !inv.dash {
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
		exitHalt(err)
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(exitInternal)
	}
	if tty && !inv.dash {
		fmt.Fprintln(os.Stderr, "bye")
	}
}

// runInit исполняет ~/.config/brig/init.brig, если файл есть.
// Нет файла — не ошибка. Ошибка в файле печатается, сессия продолжается.
func runInit(s *repl.Session) {
	path, err := initFile()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: init: %v\n", err)
		return
	}
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		return
	}
	_ = s.LoadFile(path, false)
}

func initFile() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(dir) {
		return filepath.Join(dir, "brig", "init.brig"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "brig", "init.brig"), nil
}

// loadTarget грузит файл или каталог проекта в сессию. Ошибка уже
// напечатана; REPL открывается в любом случае.
func loadTarget(s *repl.Session, path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return s.LoadFile(path, true)
	}
	if fi.IsDir() {
		return s.LoadProject(path)
	}
	return s.LoadFile(path, true)
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
	t.History = openHistory(s.ProjectRoot())
	pal := highlight.PaletteFromEnv(nil)
	t.Color = pal.Enabled()
	t.Highlight = func(src string, cursor int) string {
		return highlight.Highlight(src, cursor, s.HighlightEnv(), pal)
	}
	t.Hint = func(src string, pos int) string {
		if t.History == nil {
			return ""
		}
		rs := []rune(src)
		if pos < 0 || pos > len(rs) {
			return ""
		}
		return t.History.Suggest(string(rs[:pos]))
	}
	t.Complete = func(src string, pos int) term.Completion {
		c := s.Complete(src, pos)
		out := term.Completion{From: c.From, To: c.To, Candidates: make([]term.Candidate, len(c.Candidates))}
		for i, cand := range c.Candidates {
			out.Candidates[i] = term.Candidate{Insert: cand.Insert, Display: cand.Display}
		}
		return out
	}
	t.Signature = func(src string, pos int) (string, int, int) {
		return s.Signature(src, pos)
	}

	fe := repl.Plain{
		Out: os.Stdout,
		Err: os.Stderr,
		Pal: pal,
		Width: func() int {
			if t.Width == nil {
				return 0
			}
			return t.Width()
		},
	}
	s.SetOutput(os.Stderr)
	s.SetPalette(pal)
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
			project := s.ProjectRoot()
			if project != "" && t.History.Path == projectHistory(project) {
				t.History = openGlobalHistory()
				if err := t.History.Add(src); err != nil {
					fmt.Fprintf(os.Stderr, "warning: history: %v\n", err)
					t.History.Path = ""
				}
			} else {
				t.History.Path = ""
			}
		}
		if err := fe.Eval(s, src); err != nil {
			return err
		}
	}
}

func projectHistory(root string) string {
	return filepath.Join(root, ".brig", "history")
}

// openHistory — история проекта `<корень>/.brig/history` для `-i` каталога.
// Нет прав — глобальная история (T-202).
func openHistory(project string) *term.History {
	if project != "" {
		dir := filepath.Join(project, ".brig")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			fmt.Fprintf(os.Stderr, "warning: history: %v\n", err)
		} else if h, err := term.LoadHistory(projectHistory(project)); err != nil {
			fmt.Fprintf(os.Stderr, "warning: history: %v\n", err)
		} else {
			return h
		}
	}
	return openGlobalHistory()
}

// openGlobalHistory загружает историю консоли. Файл недоступен —
// предупреждение и история только в памяти.
func openGlobalHistory() *term.History {
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
