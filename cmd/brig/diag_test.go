package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
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
		{"lexer file", "", "module Main\nfn main() ->\n    x = 1 $\n    x\n", `3:11: unexpected character '$'`},
		{"lexer code point col", "", "module Main\nfn main() ->\n    x = \"ж\" $\n    x\n", `3:13: unexpected character '$'`},
		{"lexer check", "check", "module Main\nfn main() ->\n    x = 1 $\n    x\n", `3:11: unexpected character '$'`},
		{"parser file", "", "module Main\nfn main() ->\n    x = (1 + )\n    x\n", `3:14: `},
		{"parser check", "check", "module Main\nfn main() ->\n    x = (1 + )\n    x\n", `3:14: `},
		// T-141: параметры лямбды — полные паттерны (checkParams), диагностика
		// указывает на сам параметр `..b`, не на позицию `fn` (точнее, чем раньше).
		{"sema file", "", "module Main\nfn main() ->\n    f = fn (a, ..b, c) -> 1\n    f(1)\n", `3:16: variadic parameter`},
		{"compiler clauses arity", "", "module Main\nfn f(a) -> 1\nfn f(a, b) -> 2\nfn main() ->\n    f(1)\n", `2:1: fn f: клозы разной арности без variadic`},
		{"compiler unsupported", "", "module Main\nfn main() ->\n    x = rx\"a\"\n    x\n", `3:9: fn main: срез: regex не реализован`},
		{"no main", "", "module Main\nfn f() -> 1\n", `1:1: no function main()`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, strings.ReplaceAll(tc.name, " ", "_")+".brig")
			if err := os.WriteFile(path, []byte(tc.src), 0o644); err != nil {
				t.Fatal(err)
			}
			argv := []string{path}
			if tc.cmd != "" {
				argv = []string{tc.cmd, path}
			}
			out, err := exec.Command(bin, argv...).CombinedOutput()
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

// T-248 (F-12, D-2): корпус диагностик testdata/diagnostics/*.brig. Первая
// строка каждого файла — `# expect: <check|run> <exit> <line>:<col>|- <подстрока>`:
// `brig check <файл>` или `brig <файл>` обязан завершиться с кодом exit, а
// вывод — содержать позицию `:<line>:<col>` (`-` — без позиции) и подстроку.
// Строки нумеруются с учётом самой строки-заголовка.
//
// Вторая строка `# pending: T-NNN` — сообщение плохое (таблица D-2), в
// `expect` записан желаемый результат: тест ждёт несовпадение и падает на
// неожиданном совпадении (как корпус, T-115) — тогда снять `# pending` и
// сдвинуть номера строк. Сообщения таблицы D-2 исправлены в T-265; тексты
// `raise` целиком не сверяются — только подстрока.
func TestDiagnosticsCorpus(t *testing.T) {
	bin := buildBrig(t)
	files, err := filepath.Glob(filepath.Join(findModuleRoot(t), "testdata", "diagnostics", "*.brig"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 28 {
		t.Fatalf("в корпусе диагностик %d файлов, want >= 28", len(files))
	}
	expectRe := regexp.MustCompile(`^# expect: (check|run) (\d+) (?:(\d+):(\d+)|-) (.+)$`)
	pendingRe := regexp.MustCompile(`^# pending: (T-\d+)$`)
	for _, path := range files {
		name := strings.TrimSuffix(filepath.Base(path), ".brig")
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.SplitN(string(src), "\n", 3)
		pend := ""
		if len(lines) > 1 {
			if pm := pendingRe.FindStringSubmatch(lines[1]); pm != nil {
				pend = pm[1]
			}
		}
		t.Run(name, func(t *testing.T) {
			m := expectRe.FindStringSubmatch(lines[0])
			if m == nil {
				t.Fatalf("первая строка %q не `# expect: <check|run> <exit> <line>:<col>|- <подстрока>`", lines[0])
			}
			wantExit, _ := strconv.Atoi(m[2])
			wantSub := m[5]
			var posRe *regexp.Regexp
			if m[3] != "" {
				posRe = regexp.MustCompile(`:` + m[3] + `:` + m[4] + `(\D|$)`)
			}

			argv := []string{path}
			if m[1] == "check" {
				argv = []string{"check", path}
			}
			out, err := exec.Command(bin, argv...).CombinedOutput()
			code := 0
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			} else if err != nil {
				t.Fatal(err)
			}

			var problems []string
			if code != wantExit {
				problems = append(problems, fmt.Sprintf("exit %d, want %d", code, wantExit))
			}
			if posRe != nil && !posRe.Match(out) {
				problems = append(problems, fmt.Sprintf("нет позиции %s:%s", m[3], m[4]))
			}
			if !strings.Contains(string(out), wantSub) {
				problems = append(problems, fmt.Sprintf("нет подстроки %q", wantSub))
			}

			switch {
			case pend == "" && len(problems) > 0:
				t.Errorf("%s\n%s", strings.Join(problems, "; "), out)
			case pend != "" && len(problems) == 0:
				t.Errorf("неожиданное совпадение: сообщение уже исправлено, снять `# pending: %s`\n%s", pend, out)
			}
		})
	}
}
