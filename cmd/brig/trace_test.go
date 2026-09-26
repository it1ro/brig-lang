package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func runBrigSrc(t *testing.T, src string) (string, int) {
	t.Helper()
	bin := buildBrig(t)
	path := filepath.Join(t.TempDir(), "p.brig")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "run", path)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("run: %v", err)
		}
		code = ee.ExitCode()
	}
	return stderr.String(), code
}

func TestUncaughtRaiseStackTrace(t *testing.T) {
	stderr, code := runBrigSrc(t, `module Main
fn g(x) ->
    raise(:boom)
fn f(x) ->
    g(x) + 1
fn main() ->
    r = f(1)
    print(r)
`)
	if code != exitRuntime {
		t.Fatalf("exit %d, want %d\n%s", code, exitRuntime, stderr)
	}
	re := regexp.MustCompile(`(?s)raise: :boom.*\n  at g \(.*p\.brig:3:\d+\)\n  at f \(.*p\.brig:5:\d+\)\n  at main \(.*p\.brig:7:\d+\)\n`)
	if !re.MatchString(stderr) {
		t.Fatalf("stack trace mismatch:\n%s", stderr)
	}
}

func TestStackTraceOmitsTailCalledFrames(t *testing.T) {
	stderr, _ := runBrigSrc(t, `module Main
fn g(x) ->
    raise(:boom)
fn h(x) ->
    g(x)
fn main() ->
    r = h(1)
    print(r)
`)
	if !strings.Contains(stderr, "  at g (") || !strings.Contains(stderr, "  at main (") {
		t.Fatalf("missing frames:\n%s", stderr)
	}
	if strings.Contains(stderr, "  at h (") {
		t.Fatalf("tail-called frame h must not appear:\n%s", stderr)
	}
}

func TestCaughtRaisePrintsNoTrace(t *testing.T) {
	stderr, code := runBrigSrc(t, `module Main
fn g(x) ->
    raise(:boom)
fn main() ->
    r = trap(g(1))
    print(r)
`)
	if code != exitOK || strings.Contains(stderr, "  at ") {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
}
