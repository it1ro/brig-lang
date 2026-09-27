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
	for _, want := range []string{"-e", "brig -", "check", "Sys.args()", "script"} {
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
	cmd := exec.Command(bin, args...)
	cmd.Stdin = strings.NewReader(stdin)
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

func isTimeout(err error) bool {
	var te interface{ Timeout() bool }
	return errors.As(err, &te) && te.Timeout()
}
