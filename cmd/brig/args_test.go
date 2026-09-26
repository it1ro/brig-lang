package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// T-75, §16 Must: `brig run p.brig a b` — Sys.args() возвращает ["a", "b"].
func TestBrigRunSysArgs(t *testing.T) {
	bin := buildBrig(t)
	path := filepath.Join(t.TempDir(), "p.brig")
	src := "module Main\nfn main() ->\n    print(Sys.args())\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(bin, "run", path, "a", "b").CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if got, want := string(out), "[\"a\", \"b\"]\n"; got != want {
		t.Fatalf("stdout %q, want %q", got, want)
	}
}
