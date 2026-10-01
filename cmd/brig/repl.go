package main

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/it1ro/brig-lang/internal/highlight"
	"github.com/it1ro/brig-lang/internal/repl"
	"github.com/it1ro/brig-lang/internal/repl/term"
	"github.com/it1ro/brig-lang/internal/vm"
)

// prompts — приглашение консоли: `brig N ❯ ` и продолжение той же ширины
// с `·` под `❯`. N — номер ввода для v(n). ascii — терминал без UTF-8
// или TERM=dumb: `brig N> ` и `     .> `. color — `brig` жирным пурпурным,
// номер и знак приглушены.
type prompts struct{ ascii, color bool }

func (p prompts) main(next int) string {
	num := strconv.Itoa(next)
	if p.ascii {
		return p.paint("1;35", "brig") + " " + p.paint("2", num+">") + " "
	}
	return p.paint("1;35", "brig") + " " + p.paint("2", num+" ❯") + " "
}

func (p prompts) cont(next int) string {
	num := strconv.Itoa(next)
	if p.ascii {
		return strings.Repeat(" ", len("brig ")+len(num)-1) + p.paint("2", ".>") + " "
	}
	return strings.Repeat(" ", len("brig ")+len(num)+1) + p.paint("2", "·") + " "
}

func (p prompts) paint(sgr, s string) string {
	if !p.color {
		return s
	}
	return "\x1b[" + sgr + "m" + s + "\x1b[0m"
}

// newPrompts выбирает вид приглашения по TERM и локали.
func newPrompts(color bool) prompts {
	return prompts{ascii: os.Getenv("TERM") == "dumb" || !utf8Locale(os.LookupEnv), color: color}
}

// utf8Locale — локаль терминала в UTF-8: первая непустая из LC_ALL,
// LC_CTYPE, LANG. Ни одна не задана — локаль C, без UTF-8.
func utf8Locale(getenv func(string) (string, bool)) bool {
	for _, k := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v, _ := getenv(k); v != "" {
			v = strings.ToLower(v)
			return strings.Contains(v, "utf-8") || strings.Contains(v, "utf8")
		}
	}
	return false
}

// banner — первые строки консоли: версия, клавиши и что загружено.
func banner(loaded string) string {
	b := fmt.Sprintf("Brig %s · %s %s/%s\n", version, goruntime.Version(), goruntime.GOOS, goruntime.GOARCH)
	b += "h() help · Tab complete · Ctrl-R history · Ctrl-D exit\n"
	if loaded != "" {
		b += loaded + "\n"
	}
	return b
}

// loadedLine — третья строка баннера: проект и init.brig.
func loadedLine(s *repl.Session, initLoaded bool) string {
	var parts []string
	if root := s.ProjectRoot(); root != "" {
		n := s.UserModuleCount()
		unit := "modules"
		if n == 1 {
			unit = "module"
		}
		parts = append(parts, fmt.Sprintf("project %s: %d %s", filepath.Base(root), n, unit))
	}
	if initLoaded {
		parts = append(parts, "init.brig loaded")
	}
	return strings.Join(parts, ", ")
}

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
	machine.SetFiles(osFiles{})
	machine.SetHTTP(newOsHTTP())
	machine.SetArgs(inv.progArgs)
	s := repl.New(machine, os.Stderr)
	defer s.Close()
	// Во время ввода терминал в raw mode, и Ctrl-C — байт редактора.
	// Пока ввод исполняется, терминал обычный: Ctrl-C приходит как SIGINT
	// и снимает вычисление, не убивая процесс и не актор сессии (§11.4).
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	defer signal.Stop(sig)
	errTTY := term.IsTerminal(os.Stderr)
	go func() {
		for range sig {
			// Терминал уже напечатал `^C`: ошибка — с новой строки.
			if errTTY {
				fmt.Fprintln(os.Stderr)
			}
			s.Interrupt()
		}
	}()
	initLoaded := false
	if !inv.noInit {
		initLoaded = runInit(s)
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
	dumb := os.Getenv("TERM") == "dumb"
	plain := inv.dash || !tty || !term.IsTerminal(os.Stdout) || dumb
	loaded := loadedLine(s, initLoaded)
	var err error
	if !plain {
		err = consoleLoop(s, loaded)
	} else {
		fe.In = os.Stdin
		if tty && !inv.dash {
			// Приглашения — в stderr, как баннер: в stdout только значения
			// (`brig | tee out.txt`).
			fmt.Fprint(os.Stderr, banner(loaded))
			fe.ErrPal = highlight.NewPalette(highlight.PaletteOptions{TTY: term.IsTerminal(os.Stderr)})
			pr := newPrompts(fe.ErrPal.Enabled())
			fe.Prompt = func(next int, more bool) string {
				if more {
					return pr.cont(next)
				}
				return pr.main(next)
			}
		}
		err = fe.Run(s)
	}
	if err != nil {
		exitHalt(err)
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(exitInternal)
	}
}

// runInit исполняет ~/.config/brig/init.brig, если файл есть, и
// сообщает, загружен ли он. Нет файла — не ошибка. Ошибка в файле
// печатается, сессия продолжается.
func runInit(s *repl.Session) bool {
	path, err := initFile()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: init: %v\n", err)
		return false
	}
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		return false
	}
	return s.LoadFile(path, false) == nil
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
func consoleLoop(s *repl.Session, loaded string) error {
	fmt.Fprint(os.Stderr, banner(loaded))
	t := term.NewTerminal(os.Stdin, os.Stdout)
	t.NeedMore = s.NeedMore
	t.Indent = repl.Indent
	t.IndentWidth = repl.IndentWidth
	t.History = openHistory(s.ProjectRoot())
	initPath, _ := initFile()
	s.SetConsolePaths(repl.ConsolePaths{History: t.History.Path, Init: initPath})
	pal, errPal := consolePalettes(t)
	pr := newPrompts(pal.Enabled())
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
		Out:    os.Stdout,
		Err:    os.Stderr,
		Pal:    pal,
		ErrPal: errPal,
		Width: func() int {
			if t.Width == nil {
				return 0
			}
			return t.Width()
		},
	}
	s.SetOutput(os.Stderr)
	s.SetPalette(errPal)
	for {
		src, err := t.ReadInput(pr.main(s.Next()), pr.cont(s.Next()))
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
			if project != "" && t.History.Path != globalHistoryPath() {
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

// consolePalettes — палитры stdout и stderr: цвет у потока, только если
// он — терминал. Фон для BRIG_THEME=auto спрашивается у терминала
// (OSC 11) один раз; предупреждения BRIG_COLORS печатаются один раз.
func consolePalettes(t *term.Terminal) (out, errPal highlight.Palette) {
	var once sync.Once
	var light, ok bool
	bg := func() (bool, bool) {
		once.Do(func() { light, ok = highlight.LightBackground(t.QueryBackground(100 * time.Millisecond)) })
		return light, ok
	}
	out = highlight.NewPalette(highlight.PaletteOptions{TTY: term.IsTerminal(os.Stdout), Background: bg, Warn: os.Stderr})
	errPal = highlight.NewPalette(highlight.PaletteOptions{TTY: term.IsTerminal(os.Stderr), Background: bg})
	return out, errPal
}

// openHistory — история проекта для `-i` каталога в
// $XDG_STATE_HOME/brig/projects/<hash>/history; записи старого файла
// `<проект>/.brig/history`, если он есть, идут перед ней. Нет проекта или
// нет прав — глобальная история (T-202).
func openHistory(project string) *term.History {
	if project != "" {
		if h, err := openProjectHistory(project); err != nil {
			fmt.Fprintf(os.Stderr, "warning: history: %v\n", err)
		} else {
			return h
		}
	}
	return openGlobalHistory()
}

func openProjectHistory(project string) (*term.History, error) {
	path, err := term.ProjectHistoryPath(project)
	if err != nil {
		return nil, err
	}
	h, err := term.LoadHistory(path)
	if err != nil {
		return nil, err
	}
	if old, err := term.LoadHistory(filepath.Join(project, ".brig", "history")); err == nil {
		h.Prepend(old)
	}
	return h, nil
}

func globalHistoryPath() string {
	path, _ := term.DefaultHistoryPath()
	return path
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
