package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestExitClassifyHelpers(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		fn   func(error) int
		err  error
		want int
	}{
		{"compile срез → parse", exitForCompileErr, errors.New("fn main: срез: pipe не реализован"), exitParse},
		{"compile other user → parse", exitForCompileErr, errors.New("undefined: foo"), exitParse},
		{"compile internal → internal", exitForCompileErr, errors.New("internal: boom"), exitInternal},
		{"run internal upvalue → internal", exitForRunErr, errors.New("internal: upvalue 0 out of range"), exitInternal},
		{"run raise → runtime", exitForRunErr, errors.New("raise: :boom"), exitRuntime},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.fn(tc.err); got != tc.want {
				t.Fatalf("got %d, want %d for %v", got, tc.want, tc.err)
			}
		})
	}
}

// Probes for CLI exit codes. upvalueSrc used to reach runtime
// `internal: upvalue` (A-F7 / T-14); T-38 fail-fasted it at compile time.
// T-51 lifts captures into hidden params, so only a capturing local fn
// used as a value is still срез (T-39).
const (
	sliceNYISrc = `module Main
fn main() ->
    xs = [1, 2, 3] |> print
`
	upvalueSrc = `module Main
fn outer(x) ->
    fn add(y) -> x + y
    h = add
    h(2)
fn main() ->
    print(outer(1))
`
	raiseSrc = `module Main
fn main() ->
    raise(:boom)
`
)

func TestBrigRunExitCodes(t *testing.T) {
	bin := buildBrig(t)
	dir := t.TempDir()

	cases := []struct {
		name string
		src  string
		want int
	}{
		{"срез pipe → 1", sliceNYISrc, exitParse},
		{"local fn capture срез → 1", upvalueSrc, exitParse},
		{"uncaught raise → 2", raiseSrc, exitRuntime},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.name+".brig")
			if err := os.WriteFile(path, []byte(tc.src), 0o644); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(bin, "run", path)
			out, err := cmd.CombinedOutput()
			got := 0
			if err != nil {
				ee, ok := err.(*exec.ExitError)
				if !ok {
					t.Fatalf("run: %v\n%s", err, out)
				}
				got = ee.ExitCode()
			}
			if got != tc.want {
				t.Fatalf("exit %d, want %d\n%s", got, tc.want, out)
			}
		})
	}
}

func buildBrig(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "brig")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = filepath.Join(findModuleRoot(t), "cmd", "brig")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

func findModuleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}
