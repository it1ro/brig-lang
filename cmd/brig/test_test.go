package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runBrigTest запускает `brig test args...` и возвращает exit-код и вывод.
func runBrigTest(t *testing.T, bin string, args ...string) (int, string) {
	t.Helper()
	out, err := exec.Command(bin, append([]string{"test"}, args...)...).CombinedOutput()
	if err == nil {
		return 0, string(out)
	}
	ee, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("brig test: %v\n%s", err, out)
	}
	return ee.ExitCode(), string(out)
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
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

// Проба h14 (G-9): упавший тест обязан ронять exit-код.
func TestBrigTestExitCodeOnFailure(t *testing.T) {
	bin := buildBrig(t)
	cases := []struct {
		name string
		src  string
		want int
	}{
		{"all pass → 0", `fn test_add() -> Test.assert_eq(1 + 1, 2)
fn test_it() -> Test.it("sum", () -> Test.assert_eq(2 + 2, 4))
`, exitOK},
		{"assert_eq fails → 1", `fn test_ok() -> Test.assert_eq(1, 1)
fn test_bad() -> Test.assert_eq(1 + 1, 3)
`, 1},
		{"raise in test → 1", `fn test_boom() -> raise(:boom)
`, 1},
		{"Test.it fails → 1", `fn test_group() ->
    Test.describe("math")
    Test.it("bad", () -> Test.assert_eq(2 + 2, 5))
`, 1},
		{"compile error → 1", `fn test_x( ->
`, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFiles(t, dir, map[string]string{"m_test.brig": tc.src})
			got, out := runBrigTest(t, bin, dir)
			if got != tc.want {
				t.Fatalf("exit %d, want %d\n%s", got, tc.want, out)
			}
		})
	}
}

// Соглашение (T-125): `*_test.brig` под path — тестовые файлы, `test_*`
// без параметров — тесты; прочие `.brig` не исполняются, кроме доктестов.
func TestBrigTestDiscoversFiles(t *testing.T) {
	bin := buildBrig(t)
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"a_test.brig":        "fn test_a() -> Test.assert(true)\nfn helper() -> raise(:not_a_test)\nfn test_arity(x) -> raise(:not_a_test)\n",
		"sub/b_test.brig":    "fn test_b() -> Test.assert_eq(:b, :b)\n",
		"lib.brig":           "fn main() -> raise(:not_run)\n",
		"broken.brig":        "fn broken( ->\n",
		"sub/other.brig.txt": "fn test_no() -> raise(:no)\n",
	})
	got, out := runBrigTest(t, bin, dir)
	if got != exitOK {
		t.Fatalf("exit %d, want 0\n%s", got, out)
	}
	for _, want := range []string{"test_a", "test_b", "2 passed, 0 failed"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	for _, bad := range []string{"helper", "test_arity", "test_no"} {
		if strings.Contains(out, bad) {
			t.Errorf("output mentions %q:\n%s", bad, out)
		}
	}

	// Путь к одному файлу.
	got, out = runBrigTest(t, bin, filepath.Join(dir, "sub", "b_test.brig"))
	if got != exitOK || !strings.Contains(out, "1 passed, 0 failed") {
		t.Fatalf("single file: exit %d\n%s", got, out)
	}

	// Несуществующий путь — ошибка, а не «0 тестов».
	if got, out := runBrigTest(t, bin, filepath.Join(dir, "nope")); got == exitOK {
		t.Fatalf("missing path: exit 0\n%s", out)
	}
}

// Доктесты `##` исполняются тем же раннером и влияют на exit-код.
func TestBrigTestDoctests(t *testing.T) {
	bin := buildBrig(t)
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"lib.brig": "## Сумма.\n##\n## ```brig repl\n## > add(1, 2)\n## 3\n## ```\nfn add(a, b) -> a + b\n"})
	if got, out := runBrigTest(t, bin, dir); got != exitOK || !strings.Contains(out, "1 passed, 0 failed") {
		t.Fatalf("exit %d\n%s", got, out)
	}
	writeFiles(t, dir, map[string]string{"lib.brig": "## ```brig repl\n## > add(1, 2)\n## 4\n## ```\nfn add(a, b) -> a + b\n"})
	if got, out := runBrigTest(t, bin, dir); got != 1 || !strings.Contains(out, "lib.brig:3") {
		t.Fatalf("mismatch: exit %d\n%s", got, out)
	}
}
