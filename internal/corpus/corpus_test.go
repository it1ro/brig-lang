package corpus

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	srcOK        = "fn main() ->\n    print(1)\n"
	srcParseErr  = "fn main( ->\n"
	srcCheckErr  = "fn main() ->\n    1 |> send\n" // sema: актор в правой части pipe
	srcNeedsT1   = "# needs: T-1\n" + srcOK
	srcNeedsT1T2 = "# needs: T-1, T-2\n#   причина\n" + srcCheckErr
)

// fixture — корень репозитория с tasks/, corpus/ и манифестом.
func fixture(t *testing.T, manifest string, files map[string]string) Config {
	t.Helper()
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for rel, body := range files {
		write(rel, body)
	}
	write("corpus/manifest.tsv", manifest)
	return Config{
		Root:     root,
		Dir:      "corpus",
		Manifest: "corpus/manifest.tsv",
		Tasks:    map[string]bool{"T-1": true, "T-2": true},
		Run:      fakeRun(map[string]string{}),
	}
}

// fakeRun: stdout по имени файла; "!err" — ненулевой код выхода.
func fakeRun(out map[string]string) Runner {
	return func(path string) (string, error) {
		s := out[filepath.Base(path)]
		if s == "!err" {
			return "", errors.New("brig run: exit status 2")
		}
		return s, nil
	}
}

func verify(t *testing.T, cfg Config) *Report {
	t.Helper()
	rep, err := Verify(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

// wantProblems: у каждого файла из want — ровно одна проблема с подстрокой.
func wantProblems(t *testing.T, rep *Report, want map[string]string) {
	t.Helper()
	if len(rep.Problems) != len(want) {
		t.Errorf("problems: got %d, want %d:\n%s", len(rep.Problems), len(want), strings.Join(rep.Problems, "\n"))
	}
	for file, sub := range want {
		found := false
		for _, p := range rep.Problems {
			if strings.Contains(p, file+":") && strings.Contains(p, sub) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: no problem containing %q in:\n%s", file, sub, strings.Join(rep.Problems, "\n"))
		}
	}
}

func TestCorpusOK(t *testing.T) {
	cfg := fixture(t, strings.Join([]string{
		"# комментарий",
		"corpus/a.brig\trun\tpass\t-",
		"corpus/b.brig\tcheck\tfail\tT-1, T-2",
		"corpus/c.brig\tparse\tpass\t-",
		"corpus/sub/d.brig\tparse\tfail\tT-1",
		"research/x.html.bt\tparse\tfail\thorizon",
		"",
	}, "\n"), map[string]string{
		"corpus/a.brig":      srcOK,
		"corpus/a.out":       "1\n",
		"corpus/b.brig":      srcNeedsT1T2,
		"corpus/c.brig":      srcCheckErr,
		"corpus/sub/d.brig":  "# needs: T-1\n" + srcParseErr,
		"corpus/README.md":   "не .brig — строка не нужна",
		"research/x.html.bt": "<p>{x}</p>",
	})
	cfg.Run = fakeRun(map[string]string{"a.brig": "1\n"})
	rep := verify(t, cfg)
	wantProblems(t, rep, nil)

	var sb strings.Builder
	if err := rep.Summary(&sb, 10); err != nil {
		t.Fatal(err)
	}
	got := sb.String()
	for _, want := range []string{
		"corpus: 4 files — run 1, check 0, parse 2, none 1\nhorizon: 1\n",
		"top needs (blocked files):\n  T-1      2\n  T-2      1\ncorpus: ok",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary lacks %q:\n%s", want, got)
		}
	}
}

func TestCorpusRegression(t *testing.T) {
	cfg := fixture(t, strings.Join([]string{
		"corpus/parse_pass.brig\tparse\tpass\t-",
		"corpus/check_pass.brig\tcheck\tpass\t-",
		"corpus/run_pass.brig\trun\tpass\t-",
		"corpus/stdout.brig\trun\tpass\t-",
		"corpus/earlier.brig\tcheck\tfail\tT-1",
	}, "\n"), map[string]string{
		"corpus/parse_pass.brig": srcParseErr,
		"corpus/check_pass.brig": srcCheckErr,
		"corpus/run_pass.brig":   srcOK,
		"corpus/stdout.brig":     srcOK,
		"corpus/stdout.out":      "1\n",
		"corpus/earlier.brig":    "# needs: T-1\n" + srcParseErr,
	})
	cfg.Run = fakeRun(map[string]string{"run_pass.brig": "!err", "stdout.brig": "2\n"})
	rep := verify(t, cfg)
	wantProblems(t, rep, map[string]string{
		"corpus/parse_pass.brig": "регрессия: ожидался pass на parse, файл падает на parse",
		"corpus/check_pass.brig": "регрессия: ожидался pass на check, файл падает на check",
		"corpus/run_pass.brig":   "регрессия: ожидался pass на run, файл падает на run: brig run: exit status 2",
		"corpus/stdout.brig":     "stdout не совпадает с stdout.out",
		"corpus/earlier.brig":    "регрессия: ожидался fail на check, файл падает раньше — на parse",
	})
}

func TestCorpusUnexpectedPass(t *testing.T) {
	cfg := fixture(t, strings.Join([]string{
		"corpus/parse.brig\tparse\tfail\tT-1",
		"corpus/check.brig\tcheck\tfail\tT-1",
		"corpus/run.brig\trun\tfail\tT-1",
		"corpus/still.brig\trun\tfail\tT-1",
	}, "\n"), map[string]string{
		"corpus/parse.brig": srcNeedsT1,
		"corpus/check.brig": srcNeedsT1,
		"corpus/run.brig":   srcNeedsT1,
		"corpus/still.brig": srcNeedsT1,
	})
	cfg.Run = fakeRun(map[string]string{"still.brig": "!err"})
	rep := verify(t, cfg)
	wantProblems(t, rep, map[string]string{
		"corpus/parse.brig": "неожиданный проход: по манифесту файл падает на parse и ждёт T-1",
		"corpus/check.brig": "неожиданный проход: по манифесту файл падает на check",
		"corpus/run.brig":   "неожиданный проход: по манифесту файл падает на run",
	})
}

func TestCorpusNeedsUnknownTask(t *testing.T) {
	cfg := fixture(t, strings.Join([]string{
		"corpus/a.brig\tparse\tfail\tT-1, T-999",
		"corpus/b.brig\tparse\tfail\tsomeday",
	}, "\n"), map[string]string{
		"corpus/a.brig": "# needs: T-1, T-999\n" + srcParseErr,
		"corpus/b.brig": "# needs: someday\n" + srcParseErr,
	})
	rep := verify(t, cfg)
	wantProblems(t, rep, map[string]string{
		"corpus/a.brig": "needs: задачи T-999 нет в tasks/",
		"corpus/b.brig": `needs: "someday" — ни T-NNN, ни horizon`,
	})
}

func TestCorpusManifestRows(t *testing.T) {
	cfg := fixture(t, strings.Join([]string{
		"corpus/a.brig\tparse\tpass\t-",
		"corpus/a.brig\tparse\tpass\t-",
		"corpus/gone.brig\tparse\tpass\t-",
		"corpus/../x.brig\tparse\tpass\t-",
		"corpus/pass_needs.brig\tparse\tpass\tT-1",
		"corpus/fail_no_needs.brig\tparse\tfail\t-",
		"corpus/header.brig\tparse\tfail\tT-1, T-2",
	}, "\n"), map[string]string{
		"corpus/a.brig":             srcOK,
		"corpus/orphan.brig":        srcOK,
		"corpus/pass_needs.brig":    srcNeedsT1,
		"corpus/fail_no_needs.brig": srcParseErr,
		"corpus/header.brig":        srcNeedsT1,
		"x.brig":                    srcOK,
	})
	rep := verify(t, cfg)
	wantProblems(t, rep, map[string]string{
		"corpus/a.brig":             "вторая строка для того же файла",
		"corpus/gone.brig":          "файла нет",
		"corpus/../x.brig":          "путь должен быть относительным",
		"corpus/pass_needs.brig":    "pass с needs",
		"corpus/fail_no_needs.brig": "fail без needs",
		"corpus/header.brig":        "`# needs:` в файле (T-1) не совпадает с манифестом (T-1, T-2)",
		"corpus/orphan.brig":        "нет строки в corpus/manifest.tsv",
	})
}

func TestParseManifestErrors(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"corpus/a.brig\tparse\tpass", "нужно 4 колонки"},
		{"corpus/a.brig\tlink\tpass\t-", `уровень "link"`},
		{"corpus/a.brig\tparse\tmaybe\t-", `ожидание "maybe"`},
	} {
		if _, err := ParseManifest(tc.src); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("ParseManifest(%q) = %v, want error containing %q", tc.src, err, tc.want)
		}
	}
}

func TestCorpusUpdate(t *testing.T) {
	manifest := strings.Join([]string{
		"# шапка остаётся",
		"corpus/runs.brig\tparse\tfail\tT-1",
		"corpus/checks.brig\tparse\tfail\tT-1, T-2",
		"corpus/out.brig\trun\tpass\t-",
		"corpus/broken.brig\tcheck\tpass\t-",
		"",
	}, "\n")
	cfg := fixture(t, manifest, map[string]string{
		"corpus/runs.brig":   srcNeedsT1,
		"corpus/checks.brig": srcNeedsT1T2,
		"corpus/out.brig":    srcOK,
		"corpus/out.out":     "old\n",
		"corpus/broken.brig": srcParseErr,
	})
	cfg.Run = fakeRun(map[string]string{"runs.brig": "1\n", "out.brig": "1\n"})
	rep := verify(t, cfg)
	changes, err := Update(cfg, rep)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 3 {
		t.Errorf("changes: got %d, want 3:\n%s", len(changes), strings.Join(changes, "\n"))
	}

	read := func(rel string) string {
		data, err := os.ReadFile(filepath.Join(cfg.Root, rel))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	wantManifest := strings.Join([]string{
		"# шапка остаётся",
		"corpus/runs.brig\trun\tpass\t-",
		"corpus/checks.brig\tcheck\tfail\tT-1, T-2",
		"corpus/out.brig\trun\tpass\t-",
		"corpus/broken.brig\tcheck\tpass\t-",
		"",
	}, "\n")
	if got := read("corpus/manifest.tsv"); got != wantManifest {
		t.Errorf("manifest:\n%s\nwant:\n%s", got, wantManifest)
	}
	if got := read("corpus/runs.brig"); got != srcOK {
		t.Errorf("runs.brig: `# needs:` not dropped:\n%s", got)
	}
	if got := read("corpus/checks.brig"); got != srcNeedsT1T2 {
		t.Errorf("checks.brig changed:\n%s", got)
	}
	if got := read("corpus/out.out"); got != "1\n" {
		t.Errorf("out.out = %q, want %q", got, "1\n")
	}

	// Регрессия остаётся: update её не прячет.
	rep = verify(t, cfg)
	wantProblems(t, rep, map[string]string{"corpus/broken.brig": "регрессия"})
}

// T-246: закрытая задача в needs — проблема; horizon и открытые — нет.
func TestClosedTaskInNeeds(t *testing.T) {
	cfg := fixture(t, strings.Join([]string{
		"corpus/a.brig\tparse\tfail\tT-1, T-2, horizon",
		"corpus/b.brig\tparse\tfail\tT-2",
		"corpus/c.brig\tparse\tfail\thorizon",
	}, "\n"), map[string]string{})
	problems, err := CheckClosed(cfg, map[string]bool{"T-1": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 || !strings.Contains(problems[0], "corpus/a.brig") || !strings.Contains(problems[0], "T-1 закрыта") {
		t.Fatalf("problems = %q, want одна про corpus/a.brig T-1", problems)
	}
	problems, err = CheckClosed(cfg, map[string]bool{"T-1": true, "T-2": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 3 {
		t.Fatalf("problems = %q, want 3 (a: T-1, a: T-2, b: T-2)", problems)
	}
	if problems, _ = CheckClosed(cfg, nil); len(problems) != 0 {
		t.Fatalf("без закрытых задач проблем быть не должно: %q", problems)
	}
}

// T-249: уровень check — граф модулей от корня проекта, как у brig check
// (T-243): модуль из lib/ находит соседа по project.brig, а не по своему
// каталогу; импорт несуществующего модуля — провал check.
func TestCorpusProjectRoot(t *testing.T) {
	cfg := fixture(t, strings.Join([]string{
		"corpus/proj/project.brig\tcheck\tpass\t-",
		"corpus/proj/lib/util.brig\tcheck\tpass\t-",
		"corpus/proj/lib/app/main.brig\tcheck\tpass\t-",
		"corpus/proj/lib/app/lost.brig\tcheck\tfail\thorizon",
	}, "\n"), map[string]string{
		"corpus/proj/project.brig":      "module Project\n\npub fn project() -> { name: \"p\" }\n",
		"corpus/proj/lib/util.brig":     "module Util\n\npub fn two() -> 2\n",
		"corpus/proj/lib/app/main.brig": "module App.Main\n\nimport Util\n\nfn main() -> print(Util.two())\n",
		"corpus/proj/lib/app/lost.brig": "# needs: horizon\nmodule App.Lost\n\nimport Calmar.Endpoint\n\nfn main() -> 1\n",
	})
	rep := verify(t, cfg)
	wantProblems(t, rep, nil)
	for _, r := range rep.Results {
		if r.Path == "corpus/proj/lib/app/lost.brig" && !strings.Contains(r.Err, "module Calmar.Endpoint not found") {
			t.Errorf("lost.brig: err %q, want module not found", r.Err)
		}
	}
}
