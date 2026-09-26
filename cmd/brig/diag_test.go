package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// §E.1: каждая compile-time диагностика — `error: <file>:<line>:<col>: <msg>`.
func TestDiagnosticsFormat(t *testing.T) {
	bin := buildBrig(t)
	dir := t.TempDir()

	cases := []struct {
		name string
		cmd  string
		src  string
		want string // после `error: <file>:`
	}{
		{"lexer run", "run", "module Main\nfn main() ->\n    x = 1 $\n    x\n", `3:11: unexpected character '$'`},
		{"lexer code point col", "run", "module Main\nfn main() ->\n    x = \"ж\" $\n    x\n", `3:13: unexpected character '$'`},
		{"lexer check", "check", "module Main\nfn main() ->\n    x = 1 $\n    x\n", `3:11: unexpected character '$'`},
		{"parser run", "run", "module Main\nfn main() ->\n    x = (1 + )\n    x\n", `3:14: `},
		{"parser check", "check", "module Main\nfn main() ->\n    x = (1 + )\n    x\n", `3:14: `},
		{"sema run", "run", "module Main\nfn main() ->\n    f = fn (a, ..b, c) -> 1\n    f(1)\n", `3:9: variadic parameter`},
		{"compiler clauses arity", "run", "module Main\nfn f(a) -> 1\nfn f(a, b) -> 2\nfn main() ->\n    f(1)\n", `2:1: fn f: клозы разной арности без variadic`},
		{"compiler unsupported", "run", "module Main\nfn main() ->\n    x = rx\"a\"\n    x\n", `3:9: fn main: срез: regex не реализован`},
		{"no main", "run", "module Main\nfn f() -> 1\n", `1:1: нет функции main()`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, strings.ReplaceAll(tc.name, " ", "_")+".brig")
			if err := os.WriteFile(path, []byte(tc.src), 0o644); err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command(bin, tc.cmd, path).CombinedOutput()
			if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != exitParse {
				t.Fatalf("exit: %v, want %d\n%s", err, exitParse, out)
			}
			line := strings.TrimRight(string(out), "\n")
			re := regexp.MustCompile(`^error: ` + regexp.QuoteMeta(path) + `:` + regexp.QuoteMeta(tc.want))
			if strings.Contains(line, "\n") || !re.MatchString(line) {
				t.Fatalf("stderr = %q, want prefix %q", line, "error: "+path+":"+tc.want)
			}
		})
	}
}
