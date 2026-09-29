//go:build unix

package main

import (
	"bufio"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// startBrig запускает `brig <src>` и ждёт строку ready в stdout: после
// неё программа подписана и сигнал можно слать.
func startBrig(t *testing.T, bin, src string) (*exec.Cmd, *strings.Builder, <-chan error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "main.brig")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, path)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		signalled := false
		for sc.Scan() {
			out.WriteString(sc.Text() + "\n")
			if !signalled && sc.Text() == ":ready" {
				signalled = true
				close(ready)
			}
		}
		_, _ = io.Copy(io.Discard, stdout)
		done <- cmd.Wait()
	}()
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("brig exited before ready: %v\nstdout:\n%s\nstderr:\n%s", err, out.String(), stderr.String())
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("no ready line\nstderr:\n%s", stderr.String())
	}
	return cmd, &out, done
}

func waitExit(t *testing.T, cmd *exec.Cmd, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("brig did not exit within 10s")
		return nil
	}
}

// TestSignalSubscribeE2E — Signal.subscribe([:sigterm]) в `brig <file>`:
// kill -TERM доставляется владельцу как (:signal, port, :sigterm), программа
// ждёт его и завершается сама с кодом 0 (§12.12, §15.2).
func TestSignalSubscribeE2E(t *testing.T) {
	bin := buildBrig(t)
	cmd, out, done := startBrig(t, bin, `module Main
fn guard(parent) ->
    port = Signal.subscribe([:sigterm])
    send(parent, :subscribed)
    recv
        (:signal, _, name) ->
            print(name)
            Port.close(port)

fn main() ->
    me = self()
    _pid = spawn(() -> guard(me))
    recv
        :subscribed -> print(:ready)
`)
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err := waitExit(t, cmd, done); err != nil {
		t.Fatalf("exit: %v\nstdout:\n%s", err, out.String())
	}
	if got := out.String(); got != ":ready\n:sigterm\n" {
		t.Fatalf("stdout = %q", got)
	}
}

// TestSignalDefaultAfterClose — подписка закрыта: SIGTERM снова действует
// по умолчанию и завершает процесс (§12.12).
func TestSignalDefaultAfterClose(t *testing.T) {
	bin := buildBrig(t)
	cmd, _, done := startBrig(t, bin, `module Main
fn main() ->
    Port.close(Signal.subscribe([:sigterm]))
    print(:ready)
    recv
        _ -> ()
    after 20000 -> ()
`)
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	err := waitExit(t, cmd, done)
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("want killed by signal, got %v", err)
	}
	ws, ok := ee.Sys().(syscall.WaitStatus)
	if !ok || !ws.Signaled() || ws.Signal() != syscall.SIGTERM {
		t.Fatalf("want SIGTERM termination, got %v", err)
	}
}

// TestSysHaltExitCode — Sys.halt(code) завершает `brig <file>` с кодом
// code, не дожидаясь открытого порта (§12.12).
func TestSysHaltExitCode(t *testing.T) {
	bin := buildBrig(t)
	path := filepath.Join(t.TempDir(), "halt.brig")
	src := `module Main
fn main() ->
    _port = Signal.subscribe([:sigterm])
    trap
        ensure print(:ensure)
        Sys.halt(7)
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(bin, path).CombinedOutput()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 7 {
		t.Fatalf("want exit 7, got %v\n%s", err, out)
	}
	if len(out) != 0 {
		t.Fatalf("output on halt: %q", out)
	}
}
