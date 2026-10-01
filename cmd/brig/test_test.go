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

// T-245: semver.brig из приложения A третьего аудита — ответы доктестов
// со значениями типов модуля, fn модуля `v`/`c` затеняют хелпер `v`.
func TestBrigTestSemverDoctests(t *testing.T) {
	bin := buildBrig(t)
	got, out := runBrigTest(t, bin, filepath.Join("..", "..", "internal", "examples", "testdata", "semver.brig"))
	if got != exitOK || !strings.Contains(out, "5 passed, 0 failed") {
		t.Fatalf("exit %d\n%s", got, out)
	}
}

// T-244: тестовый файл и файл с `##` — программа через загрузчик модулей:
// import модуля проекта работает в тестах и доктестах.
func TestBrigTestImportsProjectModule(t *testing.T) {
	bin := buildBrig(t)
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"calc.brig":      "module Calc\n\n## Сумма.\n##\n## ```brig repl\n## > add(1, 2)\n## 3\n## ```\npub fn add(a, b) -> a + b\n",
		"calc_test.brig": "import Calc\n\nfn test_add() -> Test.assert_eq(Calc.add(2, 3), 5)\n",
		"report.brig":    "module Report\nimport Calc\n\n## Итог.\n##\n## ```brig repl\n## > total([1, 2, 3])\n## 6\n## > Calc.add(1, 1)\n## 2\n## ```\npub fn total(xs) -> Enum.fold(xs, 0, (acc, x) -> Calc.add(acc, x))\n",
	})
	got, out := runBrigTest(t, bin, dir)
	if got != exitOK {
		t.Fatalf("exit %d, want 0\n%s", got, out)
	}
	for _, want := range []string{"ok   " + filepath.Join(dir, "calc_test.brig") + ": test_add", "calc.brig:5: doctest", "report.brig:6: doctest", "3 passed, 0 failed"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}

	// Модуль проекта: корень — по project.brig (lib/), как у brig check.
	proj := t.TempDir()
	writeFiles(t, proj, map[string]string{
		"project.brig":             "module Project\npub fn project() -> {name: \"p\"}\n",
		"lib/shop/cart.brig":       "module Shop.Cart\npub fn total() -> 7\n",
		"test/shop/cart_test.brig": "import Shop.Cart\n\nfn test_total() -> Test.assert_eq(Cart.total(), 7)\n",
	})
	got, out = runBrigTest(t, bin, filepath.Join(proj, "test"))
	if got != exitOK || !strings.Contains(out, "test_total") || !strings.Contains(out, "1 passed, 0 failed") {
		t.Fatalf("project: exit %d\n%s", got, out)
	}

	// Нет модуля — ошибка загрузчика с line:col, а не undefined function.
	miss := t.TempDir()
	writeFiles(t, miss, map[string]string{
		"x_test.brig": "import Nope\n\nfn test_x() -> Nope.f()\n",
	})
	got, out = runBrigTest(t, bin, miss)
	want := "error: " + filepath.Join(miss, "x_test.brig") + ":1:1: module Nope not found"
	if got != 1 || !strings.Contains(out, want) || strings.Contains(out, "undefined function") {
		t.Fatalf("missing module: exit %d, want %q\n%s", got, want, out)
	}
}
