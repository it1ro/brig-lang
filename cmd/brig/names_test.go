package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// T-139: неизвестное имя и неверная арность — ошибка brig check и brig run
// (§F.3), exit 1. Динамический вызов и вариадик проходят check.

func TestCheckUndefinedFunction(t *testing.T) {
	bin := buildBrig(t)
	const src = "fn main() -> nope(1)\n"
	const frag = ":1:14: undefined function nope/1"
	for _, cmd := range []string{"check", "run"} {
		out, code := brigFile(t, bin, cmd, src)
		if code != exitParse || !strings.Contains(out, frag) || strings.Contains(out, "undefined:") {
			t.Fatalf("%s: exit %d, out %q; want %d and %q", cmd, code, out, exitParse, frag)
		}
	}
}

func TestCheckUndefinedModuleFunction(t *testing.T) {
	bin := buildBrig(t)
	cases := []struct{ src, frag string }{
		{"fn main() -> Json.nope(1)\n", "undefined function Json.nope/1"},
		{"fn main() -> Util.nope()\n", "undefined function Util.nope/0"},
	}
	for _, tc := range cases {
		for _, cmd := range []string{"check", "run"} {
			out, code := brigFile(t, bin, cmd, tc.src)
			if code != exitParse || !strings.Contains(out, tc.frag) {
				t.Fatalf("%s %q: exit %d, out %q", cmd, tc.src, code, out)
			}
		}
	}

	dir := t.TempDir()
	write := func(name, src string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("util.brig", "module Util\nfn twice(x) -> x * 2\n")
	write("main.brig", "module Main\nimport Util\nfn main() -> Util.twice(3)\n")
	if out, code := brigIn(t, bin, dir, "check", "main.brig"); code != exitOK {
		t.Fatalf("Util.twice(3): exit %d\n%s", code, out)
	}
	write("main.brig", "module Main\nimport Util\nfn main() -> Util.twice()\n")
	out, code := brigIn(t, bin, dir, "check", "main.brig")
	if code != exitParse || !strings.Contains(out, "undefined function Util.twice/0") {
		t.Fatalf("Util.twice(): exit %d, out %q", code, out)
	}
}

func TestCheckArityMismatch(t *testing.T) {
	bin := buildBrig(t)
	bad := []struct{ src, frag string }{
		{"fn main() -> len(1, 2)\n", "undefined function len/2"},
		{"fn f(a) -> a\nfn main() -> f()\n", "undefined function f/0"},
		{"fn g(x, ..xs) -> x\nfn main() -> g()\n", "undefined function g/0"},
	}
	for _, tc := range bad {
		for _, cmd := range []string{"check", "run"} {
			out, code := brigFile(t, bin, cmd, tc.src)
			if code != exitParse || !strings.Contains(out, tc.frag) {
				t.Fatalf("%s %q: exit %d, out %q", cmd, tc.src, code, out)
			}
		}
	}
	good := []string{
		"fn main() -> print(1, 2, 3)\n",
		"fn g(x, ..xs) -> x\nfn main() -> g(1, 2)\n",
		"fn main() ->\n    f = len\n    f(1, 2)\n",
		"fn main() ->\n    xs = [1]\n    len(..xs)\n",
	}
	for _, src := range good {
		out, code := brigFile(t, bin, "check", src)
		if code != exitOK {
			t.Fatalf("check %q: exit %d\n%s", src, code, out)
		}
	}
}

func brigFile(t *testing.T, bin, cmd, src string) (string, int) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "main.brig")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return brigIn(t, bin, dir, cmd, path)
}

func brigIn(t *testing.T, bin, dir, cmd, file string) (string, int) {
	t.Helper()
	c := exec.Command(bin, cmd, file)
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err == nil {
		return string(out), 0
	}
	ee, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("brig %s: %v\n%s", cmd, err, out)
	}
	return string(out), ee.ExitCode()
}
