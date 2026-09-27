package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// T-135: brig check и запуск файла над графом модулей (§11.1).
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
		for _, args := range [][]string{{"check", "main.brig"}, {"main.brig"}} {
			out, code := brig(t, dir, args...)
			want := "error: main.brig:3:1: module Util not found\n"
			if code != exitParse || out != want {
				t.Errorf("%v: exit %d, out %q; want %d, %q", args, code, out, exitParse, want)
			}
		}
	})

	// T-137: e2e-фикстура testdata/modules — вывод совпадает с main.out.
	t.Run("run several modules", func(t *testing.T) {
		dir := filepath.Join("..", "..", "testdata", "modules")
		want, err := os.ReadFile(filepath.Join(dir, "main.out"))
		if err != nil {
			t.Fatal(err)
		}
		out, code := brig(t, dir, "main.brig")
		if code != exitOK || out != string(want) {
			t.Fatalf("exit %d, out:\n%s\nwant:\n%s", code, out, want)
		}
	})

	t.Run("stack trace names module function and its file", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, map[string]string{
			"main.brig": "module Main\nimport Util\n\nfn main() ->\n    r = Util.f(1)\n    print(r)\n",
			"util.brig": "fn g(x) ->\n    raise(:boom)\n\nfn f(x) ->\n    y = g(x)\n    y\n",
		})
		out, code := brig(t, dir, "main.brig")
		want := "  at Util.g (util.brig:2:5)\n  at Util.f (util.brig:5:9)\n  at main (main.brig:5:13)\n"
		if code != exitRuntime || !strings.HasSuffix(out, want) {
			t.Fatalf("exit %d, want %d with trace\n%s\ngot:\n%s", code, exitRuntime, want, out)
		}
	})

	t.Run("compile error in imported module names its file", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, map[string]string{
			"main.brig": "module Main\nimport Util\n\nfn main() -> Util.f()\n",
			"util.brig": "fn f() -> [1] |> Nope.g\n",
		})
		out, code := brig(t, dir, "main.brig")
		want := "error: util.brig:1:18: undefined function Nope.g/1\n"
		if code != exitParse || out != want {
			t.Fatalf("exit %d, out %q; want %d, %q", code, out, exitParse, want)
		}
	})

	t.Run("run with builtin import only", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, map[string]string{
			"main.brig": "module Main\nimport Json\n\nfn main() -> print(1)\n",
		})
		if out, code := brig(t, dir, "main.brig"); code != exitOK {
			t.Fatalf("exit %d, want 0\n%s", code, out)
		}
	})
}
