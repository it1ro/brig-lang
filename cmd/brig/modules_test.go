package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// T-135: brig check/run над графом модулей (§11.1).
func TestBrigModuleGraph(t *testing.T) {
	bin := buildBrig(t)

	write := func(t *testing.T, dir string, files map[string]string) {
		t.Helper()
		for name, src := range files {
			path := filepath.Join(dir, name)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	brig := func(t *testing.T, dir string, args ...string) (string, int) {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			ee, ok := err.(*exec.ExitError)
			if !ok {
				t.Fatalf("brig %v: %v\n%s", args, err, out)
			}
			return string(out), ee.ExitCode()
		}
		return string(out), 0
	}

	t.Run("check runs sema in every module", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, map[string]string{
			"main.brig": "module Main\n\nimport Http.Client\n\nfn main() -> 0\n",
			// sema-ошибка (rebinding, §6.6) в импортированном модуле.
			"http/client.brig": "fn f() ->\n    x = 1\n    x = 2\n    x\n",
		})
		out, code := brig(t, dir, "check", "main.brig")
		if code != exitParse || !strings.Contains(out, "error: "+filepath.Join("http", "client.brig")+":3:") {
			t.Fatalf("exit %d, want %d with error in http/client.brig\n%s", code, exitParse, out)
		}
	})

	t.Run("check ok on graph with cycle", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, map[string]string{
			"main.brig": "import Util\n\nfn main() -> 0\n",
			"util.brig": "import Main\n\nfn one() -> 1\n",
		})
		if out, code := brig(t, dir, "check", "main.brig"); code != exitOK {
			t.Fatalf("exit %d, want 0\n%s", code, out)
		}
	})

	t.Run("missing module", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, map[string]string{
			"main.brig": "module Main\n\nimport Util\n\nfn main() -> 0\n",
		})
		for _, sub := range []string{"check", "run"} {
			out, code := brig(t, dir, sub, "main.brig")
			want := "error: main.brig:3:1: module Util not found\n"
			if code != exitParse || out != want {
				t.Errorf("%s: exit %d, out %q; want %d, %q", sub, code, out, exitParse, want)
			}
		}
	})

	t.Run("run with several modules is a slice", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, map[string]string{
			"main.brig": "import Util\n\nfn main() -> 0\n",
			"util.brig": "fn one() -> 1\n",
		})
		out, code := brig(t, dir, "run", "main.brig")
		if code != exitParse || !strings.Contains(out, "срез: несколько модулей") {
			t.Fatalf("exit %d, want %d with «срез: несколько модулей»\n%s", code, exitParse, out)
		}
	})

	t.Run("run with builtin import only", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, map[string]string{
			"main.brig": "import Json\n\nfn main() -> print(1)\n",
		})
		if out, code := brig(t, dir, "run", "main.brig"); code != exitOK {
			t.Fatalf("exit %d, want 0\n%s", code, out)
		}
	})
}
