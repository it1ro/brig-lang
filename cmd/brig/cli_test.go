package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// T-207: brig app.brig a --port 80 — файл-вход, хвост это Sys.args(),
// флаги brig только до файла, импорт тянет loader.
func TestCLIFileArgs(t *testing.T) {
	bin := buildBrig(t)
	dir := t.TempDir()
	app := filepath.Join(dir, "app.brig")
	if err := os.WriteFile(app, []byte("module Main\nimport Util\nfn main() ->\n    print(Util.n())\n    print(Sys.args())\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "util.brig"), []byte("module Util\nfn n() -> 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runCLI(t, bin, "", app, "a", "--port", "80")
	if code != exitOK || stdout != "1\n[\"a\", \"--port\", \"80\"]\n" {
		t.Fatalf("args: exit %d stdout %q stderr %s", code, stdout, stderr)
	}

	stdout, stderr, code = runCLI(t, bin, "", app, "--dump-bytecode")
	if code != exitOK || stdout != "1\n[\"--dump-bytecode\"]\n" {
		t.Fatalf("flag after file is a program arg: exit %d stdout %q stderr %s", code, stdout, stderr)
	}

	stdout, stderr, code = runCLI(t, bin, "", "--dump-bytecode", app)
	if code != exitOK || !strings.Contains(stdout, "== main ") || strings.Contains(stdout, "[\"") {
		t.Fatalf("dump before file: exit %d stdout %q stderr %s", code, stdout, stderr)
	}

	tool := filepath.Join(dir, "tool")
	if err := os.WriteFile(tool, []byte("module Main\nimport Util\nfn main() -> print(Util.n())\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code = runCLI(t, bin, "", tool)
	if code != exitOK || stdout != "1\n" {
		t.Fatalf("path with /: exit %d stdout %q stderr %s", code, stdout, stderr)
	}
}

// T-207: без аргументов TTY — REPL, не TTY — stdin как script, значения не печатаются.
func TestCLINoArgsTTYvsPipe(t *testing.T) {
	bin := buildBrig(t)

	t.Run("pipe", func(t *testing.T) {
		stdout, stderr, code := runCLI(t, bin, "1 + 1\nprint(\"ok\")\n")
		if code != exitOK || stdout != "ok\n" {
			t.Fatalf("pipe: exit %d stdout %q stderr %s", code, stdout, stderr)
		}
		if strings.Contains(stdout+stderr, "brig[") || strings.Contains(stderr, "введите выражение") {
			t.Fatalf("pipe looked like a REPL: stdout %q stderr %q", stdout, stderr)
		}
	})

	t.Run("tty", func(t *testing.T) {
		master, slave := openPTY(t)
		defer func() { _ = master.Close() }()
		cmd := exec.Command(bin)
		cmd.Stdin = slave
		cmd.Stdout = slave
		cmd.Stderr = slave
		cmd.Env = append(os.Environ(), "XDG_CONFIG_HOME="+t.TempDir())
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		if err := slave.Close(); err != nil {
			t.Fatal(err)
		}
		out := readREPLExit(t, master, cmd)
		if !strings.Contains(out, "brig[1]>") {
			t.Fatalf("tty output lacks prompt: %q", out)
		}
		if !strings.Contains(out, "введите выражение") {
			t.Fatalf("tty output lacks banner: %q", out)
		}
	})
}

// T-207: brig -e expr — исполнить и выйти, exit-коды по таблице.
func TestCLIEval(t *testing.T) {
	bin := buildBrig(t)

	stdout, stderr, code := runCLI(t, bin, "", "-e", "print(1 + 1)")
	if code != exitOK || stdout != "2\n" {
		t.Fatalf("print: exit %d stdout %q stderr %s", code, stdout, stderr)
	}
	stdout, stderr, code = runCLI(t, bin, "", "-e", "1 + 1")
	if code != exitOK || stdout != "" {
		t.Fatalf("bare value must not be printed: exit %d stdout %q stderr %s", code, stdout, stderr)
	}
	_, stderr, code = runCLI(t, bin, "", "-e", "raise(:boom)")
	if code != exitRuntime || !strings.Contains(stderr, "raise") {
		t.Fatalf("raise: exit %d stderr %s", code, stderr)
	}
	_, stderr, code = runCLI(t, bin, "", "-e", "1 +")
	if code != exitParse {
		t.Fatalf("parse: exit %d stderr %s", code, stderr)
	}
	_, stderr, code = runCLI(t, bin, "", "-e")
	if code != exitParse || !strings.Contains(stderr, "-e") {
		t.Fatalf("missing expr: exit %d stderr %s", code, stderr)
	}
	stdout, stderr, code = runCLI(t, bin, "", "-e", "print(Sys.args())", "a", "--port", "80")
	if code != exitOK || stdout != "[\"a\", \"--port\", \"80\"]\n" {
		t.Fatalf("eval args: exit %d stdout %q stderr %s", code, stdout, stderr)
	}
}

// T-207: brig - — программа из stdin.
func TestCLIStdinDash(t *testing.T) {
	bin := buildBrig(t)

	stdout, stderr, code := runCLI(t, bin, "print(Sys.args())\n", "-", "a", "b")
	if code != exitOK || stdout != "[\"a\", \"b\"]\n" {
		t.Fatalf("dash args: exit %d stdout %q stderr %s", code, stdout, stderr)
	}
	stdout, stderr, code = runCLI(t, bin, "1 + 1\n", "-")
	if code != exitOK || stdout != "" {
		t.Fatalf("dash value: exit %d stdout %q stderr %s", code, stdout, stderr)
	}
	_, stderr, code = runCLI(t, bin, "raise(:boom)\n", "-")
	if code != exitRuntime || !strings.Contains(stderr, "raise") {
		t.Fatalf("dash raise: exit %d stderr %s", code, stderr)
	}
}

// T-207: аргумент без .brig и / — подкоманда; run и repl неизвестны.
func TestCLISubcommandVsFile(t *testing.T) {
	bin := buildBrig(t)
	path := filepath.Join(t.TempDir(), "x.brig")
	if err := os.WriteFile(path, []byte("module Main\nfn main() -> print(1)\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, stderr, code := runCLI(t, bin, "", "run", "x.brig")
	if code != exitParse || !strings.Contains(stderr, "неизвестная команда") || !strings.Contains(stderr, "подсказка: brig x.brig") {
		t.Fatalf("run: exit %d stderr %q", code, stderr)
	}
	_, stderr, code = runCLI(t, bin, "", "repl")
	if code != exitParse || !strings.Contains(stderr, `неизвестная команда "repl"`) {
		t.Fatalf("repl: exit %d stderr %q", code, stderr)
	}
	_, stderr, code = runCLI(t, bin, "", "nope")
	if code != exitParse || !strings.Contains(stderr, "неизвестная команда") {
		t.Fatalf("nope: exit %d stderr %q", code, stderr)
	}

	stdout, stderr, code := runCLI(t, bin, "", "version")
	if code != exitOK || !strings.HasPrefix(stdout, "brig ") {
		t.Fatalf("version: exit %d stdout %q stderr %s", code, stdout, stderr)
	}
	_, stderr, code = runCLI(t, bin, "", "help")
	if code != exitOK {
		t.Fatalf("help: exit %d\n%s", code, stderr)
	}
	for _, want := range []string{"-e", "-i", "--no-init", "brig -", "check", "Sys.args()", "script"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("help lacks %q:\n%s", want, stderr)
		}
	}
	if strings.Contains(stderr, "brig run") || strings.Contains(stderr, "brig repl") {
		t.Fatalf("help still documents removed subcommands:\n%s", stderr)
	}

	stdout, stderr, code = runCLI(t, bin, "", "check", path)
	if code != exitOK || !strings.HasSuffix(strings.TrimRight(stdout, "\n"), ": ok") {
		t.Fatalf("check: exit %d stdout %q stderr %s", code, stdout, stderr)
	}
	stdout, stderr, code = runCLI(t, bin, "", path)
	if code != exitOK || stdout != "1\n" {
		t.Fatalf("file: exit %d stdout %q stderr %s", code, stdout, stderr)
	}
}

// T-207 (§11.3): файл без module — script; с module — fn main(); shebang работает.
func TestScriptMode(t *testing.T) {
	bin := buildBrig(t)
	dir := t.TempDir()
	write := func(name, src string) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	p := write("snap.brig", ""+
		"x = 1\n"+
		"f = () -> x\n"+
		"x = 2\n"+
		"fn main() ->\n"+
		"    print(\"main\")\n"+
		"print(f())\n"+
		"print(x)\n")
	stdout, stderr, code := runCLI(t, bin, "", p)
	if code != exitOK || stdout != "1\n2\n" {
		t.Fatalf("script: exit %d stdout %q stderr %s", code, stdout, stderr)
	}

	p = write("fact.brig", ""+
		"fn fact(n) ->\n"+
		"    match n\n"+
		"\n"+
		"        0 -> 1\n"+
		"        _ -> n * fact(n - 1)\n"+
		"print(fact(5))\n")
	stdout, stderr, code = runCLI(t, bin, "", p)
	if code != exitOK || stdout != "120\n" {
		t.Fatalf("fact: exit %d stdout %q stderr %s", code, stdout, stderr)
	}

	p = write("boom.brig", "print(1)\nraise(:boom)\nprint(2)\n")
	stdout, stderr, code = runCLI(t, bin, "", p)
	if code != exitRuntime || stdout != "1\n" {
		t.Fatalf("raise stops the script: exit %d stdout %q stderr %s", code, stdout, stderr)
	}

	p = write("mod.brig", "module Main\nfn main() ->\n    print(\"main\")\n")
	stdout, stderr, code = runCLI(t, bin, "", p)
	if code != exitOK || stdout != "main\n" {
		t.Fatalf("module: exit %d stdout %q stderr %s", code, stdout, stderr)
	}

	p = write("ty.brig", "type User { id: Int }\n")
	_, stderr, code = runCLI(t, bin, "", p)
	if code != exitParse || !strings.Contains(stderr, "type") {
		t.Fatalf("type: exit %d stderr %s", code, stderr)
	}
	p = write("pub.brig", "pub fn f() -> 1\n")
	_, stderr, code = runCLI(t, bin, "", p)
	if code != exitParse || !strings.Contains(stderr, "pub") {
		t.Fatalf("pub: exit %d stderr %s", code, stderr)
	}

	pathDir := t.TempDir()
	if err := os.Symlink(bin, filepath.Join(pathDir, "brig")); err != nil {
		t.Fatal(err)
	}
	hook := filepath.Join(dir, "hook")
	if err := os.WriteFile(hook, []byte("#!/usr/bin/env brig\nprint(42)\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(hook)
	cmd.Env = append(os.Environ(), "PATH="+pathDir)
	out, err := cmd.CombinedOutput()
	if err != nil || string(out) != "42\n" {
		t.Fatalf("shebang: %v\n%s", err, out)
	}
}

func runCLI(t *testing.T, bin, stdin string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	return runCLIEnv(t, bin, stdin, nil, args...)
}

func runCLIEnv(t *testing.T, bin, stdin string, extra []string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Stdin = strings.NewReader(stdin)
	env := append([]string{}, extra...)
	if !hasEnv(env, "XDG_CONFIG_HOME") {
		env = append(env, "XDG_CONFIG_HOME="+t.TempDir())
	}
	// В cmd.Env побеждает последнее вхождение ключа.
	cmd.Env = append(os.Environ(), env...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("brig %v: %v\n%s", args, err, errb.String())
		}
		code = ee.ExitCode()
	}
	return out.String(), errb.String(), code
}

func openPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()
	mfd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("ptmx: %v", err)
	}
	// grantpt на современном devpts не нужен: slave уже принадлежит
	// вызывающему. Unlock снимает lock, номер pty — TIOCGPTN.
	if err := unix.IoctlSetPointerInt(mfd, unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	n, err := unix.IoctlGetInt(mfd, unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	sfd, err := unix.Open(fmt.Sprintf("/dev/pts/%d", n), unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	_ = unix.IoctlSetWinsize(sfd, unix.TIOCSWINSZ, &unix.Winsize{Row: 24, Col: 80})
	return os.NewFile(uintptr(mfd), "ptmx"), os.NewFile(uintptr(sfd), "pts")
}

// readREPLExit ждёт приглашение, шлёт Ctrl-D и читает вывод до «bye».
// Ctrl-D до raw mode съедается дисциплиной линии, поэтому байт уходит
// только после того, как приглашение уже нарисовано.
func readREPLExit(t *testing.T, master *os.File, cmd *exec.Cmd) string {
	t.Helper()
	var buf bytes.Buffer
	tmp := make([]byte, 256)
	sent := false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_ = master.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		n, err := master.Read(tmp)
		if n > 0 {
			buf.Write(tmp[:n])
		}
		if !sent && strings.Contains(buf.String(), "brig[1]>") {
			if _, werr := master.Write([]byte{0x04}); werr != nil {
				t.Fatal(werr)
			}
			sent = true
		}
		if strings.Contains(buf.String(), "bye") {
			wait := make(chan error, 1)
			go func() { wait <- cmd.Wait() }()
			select {
			case werr := <-wait:
				if werr != nil {
					t.Fatalf("tty exit: %v\n%s", werr, buf.String())
				}
			case <-time.After(2 * time.Second):
				_ = cmd.Process.Kill()
				t.Fatal("tty did not exit")
			}
			return buf.String()
		}
		if err != nil && !isTimeout(err) && n == 0 {
			break
		}
	}
	_ = cmd.Process.Kill()
	t.Fatalf("tty timeout, output %q", buf.String())
	return ""
}

func hasEnv(env []string, key string) bool {
	prefix := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return true
		}
	}
	return false
}

func isTimeout(err error) bool {
	var te interface{ Timeout() bool }
	return errors.As(err, &te) && te.Timeout()
}

func consoleDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(findModuleRoot(t), "testdata", "console")
}

// T-209: brig -i a.brig b.brig грузит модули, main не вызывает; script
// исполняется как вводы; хвост после файлов — Sys.args().
func TestInteractiveLoadFile(t *testing.T) {
	bin := buildBrig(t)
	dir := t.TempDir()
	write := func(name, src string) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	app := write("app.brig", "module App\n\nfn value() -> 1\n\nfn main() ->\n    print(\"ran-main\")\n")
	extra := write("extra.brig", "module Extra\n\nfn k() -> 2\n")
	notes := write("notes.brig", "n = 5\n")
	stdin := "App.value()\nExtra.k()\nn\nSys.args()\n"
	stdout, stderr, code := runCLI(t, bin, stdin, "-i", "--no-init", app, extra, notes, "tail", "--x")
	if code != exitOK {
		t.Fatalf("exit %d stdout %q stderr %s", code, stdout, stderr)
	}
	if strings.Contains(stdout+stderr, "ran-main") {
		t.Fatalf("main was called: stdout %q stderr %s", stdout, stderr)
	}
	if stdout != "1\n2\n5\n[\"tail\", \"--x\"]\n" {
		t.Fatalf("stdout %q stderr %s", stdout, stderr)
	}
}

// T-209: brig -i . (и подкаталог) грузит проект по project.brig.
func TestInteractiveLoadProject(t *testing.T) {
	bin := buildBrig(t)
	dir := consoleDir(t)
	stdin := "App.secret()\nApp.open()\nUtil.n()\n"
	for _, start := range []string{dir, filepath.Join(dir, "sub")} {
		stdout, stderr, code := runCLI(t, bin, stdin, "-i", "--no-init", start)
		if code != exitOK {
			t.Fatalf("%s: exit %d stdout %q stderr %s", start, code, stdout, stderr)
		}
		if strings.Contains(stdout+stderr, "ran-main") {
			t.Fatalf("%s: main was called\n%s%s", start, stdout, stderr)
		}
		if stdout != "4\n3\n3\n" {
			t.Fatalf("%s: stdout %q stderr %s", start, stdout, stderr)
		}
	}

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "project.brig"), []byte("module Project\n\nfn name() -> \"x\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "lib", "http"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "lib", "main.brig"), []byte("module Main\n\nimport Util\n\nfn answer() -> Util.n()\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "lib", "util.brig"), []byte("module Util\n\nfn n() -> 6\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "lib", "http", "client.brig"), []byte("module Http.Client\n\nfn ping() -> 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := runCLI(t, bin, "Main.answer()\nHttp.Client.ping()\n", "-i", "--no-init", root)
	if code != exitOK || stdout != "6\n1\n" {
		t.Fatalf("lib: exit %d stdout %q stderr %s", code, stdout, stderr)
	}
}

// T-209: -e вместе с -i исполняется после загрузки и до ввода.
func TestInteractiveEvalBeforePrompt(t *testing.T) {
	bin := buildBrig(t)
	stdout, stderr, code := runCLI(t, bin, "App.open()\n", "-i", "--no-init", "-e", "App.secret()", consoleDir(t))
	if code != exitOK || stdout != "4\n3\n" {
		t.Fatalf("exit %d stdout %q stderr %s", code, stdout, stderr)
	}
}

// T-209: ошибка загрузки печатается, REPL открывается, привязки до неё живы.
func TestInteractiveLoadErrorStaysInRepl(t *testing.T) {
	bin := buildBrig(t)
	dir := t.TempDir()
	good := filepath.Join(dir, "good.brig")
	if err := os.WriteFile(good, []byte("module Good\n\nfn n() -> 5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(dir, "bad.brig")
	if err := os.WriteFile(bad, []byte("module Bad\n\nfn f( -> 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	later := filepath.Join(dir, "later.brig")
	if err := os.WriteFile(later, []byte("module Later\n\nfn n() -> 9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := runCLI(t, bin, "Good.n()\nLater.n()\n", "-i", "--no-init", good, bad, later)
	if code != exitOK || stdout != "5\n" {
		t.Fatalf("compile: exit %d stdout %q stderr %s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "error:") || !strings.Contains(stderr, "bad.brig:") {
		t.Fatalf("compile error is not E.1:\n%s", stderr)
	}
	if !strings.Contains(stderr, "undefined") {
		t.Fatalf("file after the error was loaded:\n%s", stderr)
	}

	script := filepath.Join(dir, "notes.brig")
	if err := os.WriteFile(script, []byte("k = 7\nraise(:boom)\nk = 9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code = runCLI(t, bin, "k\n", "-i", "--no-init", script)
	if code != exitOK || stdout != "7\n" {
		t.Fatalf("raise: exit %d stdout %q stderr %s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "boom") {
		t.Fatalf("raise not printed:\n%s", stderr)
	}
}

// T-209: не-pub своих модулей видны; у зависимости — нет.
func TestInteractivePrivateVisibility(t *testing.T) {
	bin := buildBrig(t)
	stdout, stderr, code := runCLI(t, bin, "App.secret()\nUtil.n()\nLib.secret()\n", "-i", "--no-init", consoleDir(t))
	if code != exitOK {
		t.Fatalf("exit %d stdout %q stderr %s", code, stdout, stderr)
	}
	if stdout != "4\n3\n" {
		t.Fatalf("stdout %q stderr %s", stdout, stderr)
	}
	if !strings.Contains(stderr, "error:") || !strings.Contains(stderr, "secret/0 is private to Lib") {
		t.Fatalf("private: stderr %s", stderr)
	}
}

// T-209: init.brig исполняется в каждом REPL до -i-файлов; --no-init отключает.
func TestInitFile(t *testing.T) {
	bin := buildBrig(t)
	cfg := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cfg, "brig"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, "brig", "init.brig"), []byte("a = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := []string{"XDG_CONFIG_HOME=" + cfg}

	stdout, stderr, code := runCLIEnv(t, bin, "a\n", env, "-i", "-")
	if code != exitOK || stdout != "1\n" {
		t.Fatalf("init: exit %d stdout %q stderr %s", code, stdout, stderr)
	}
	stdout, stderr, code = runCLIEnv(t, bin, "a\n", env, "-i", "--no-init", "-")
	if code != exitOK || stdout != "" {
		t.Fatalf("--no-init: exit %d stdout %q stderr %s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "undefined") {
		t.Fatalf("--no-init: want undefined a, stderr %s", stderr)
	}

	dir := t.TempDir()
	script := filepath.Join(dir, "notes.brig")
	if err := os.WriteFile(script, []byte("b = a + 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code = runCLIEnv(t, bin, "b\n", env, "-i", script)
	if code != exitOK || stdout != "2\n" {
		t.Fatalf("init before file: exit %d stdout %q stderr %s", code, stdout, stderr)
	}

	stdout, stderr, code = runCLIEnv(t, bin, "", env, "-e", "print(1)")
	if code != exitOK || stdout != "1\n" {
		t.Fatalf("eval must not run init: exit %d stdout %q stderr %s", code, stdout, stderr)
	}
}

// T-209: история -i каталога — <корень>/.brig/history; нет прав — глобальная.
func TestInteractiveHistory(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)

	root := t.TempDir()
	h := openHistory(root)
	if h.Path != filepath.Join(root, ".brig", "history") {
		t.Fatalf("project history %q", h.Path)
	}

	blocked := t.TempDir()
	if err := os.WriteFile(filepath.Join(blocked, ".brig"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	h = openHistory(blocked)
	if h.Path != filepath.Join(state, "brig", "history") {
		t.Fatalf("fallback history %q", h.Path)
	}
}

// T-209: golden-сессии гоняются как `brig -i -` без TTY.
func TestInteractivePlainSessions(t *testing.T) {
	bin := buildBrig(t)
	files, err := filepath.Glob(filepath.Join(findModuleRoot(t), "testdata", "repl", "*.txt"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no golden sessions: %v", err)
	}
	for _, path := range files {
		t.Run(strings.TrimSuffix(filepath.Base(path), ".txt"), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			input, want, ok := strings.Cut(string(data), "-- output --\n")
			if !ok {
				t.Fatalf("%s: no output separator", path)
			}
			got, code := runCombined(t, bin, input, "-i", "--no-init", "-")
			if code != exitOK || got != want {
				t.Fatalf("exit %d\n--- got ---\n%s--- want ---\n%s", code, got, want)
			}
		})
	}
}

func runCombined(t *testing.T, bin, stdin string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Stdin = strings.NewReader(stdin)
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout = pw
	cmd.Stderr = pw
	cmd.Env = append(os.Environ(), "XDG_CONFIG_HOME="+t.TempDir())
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if err := pw.Close(); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(pr); err != nil {
		t.Fatal(err)
	}
	err = cmd.Wait()
	if err == nil {
		return buf.String(), exitOK
	}
	ee, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("brig %v: %v\n%s", args, err, buf.String())
	}
	return buf.String(), ee.ExitCode()
}
