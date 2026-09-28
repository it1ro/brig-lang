package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// T-146: встроенная stdlib на Brig (`List`, `Option`, `Result`).

// Бинарник собран во временный каталог и запускается там, где исходников
// stdlib нет: модули встроены. Модуль, script и доктест `brig test`.
func TestStdlibNoFilesystem(t *testing.T) {
	bin := buildBrig(t)
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"main.brig": `module Main

fn main() ->
    print([3, 1, 2] |> List.sort() |> List.take(2))
    print(Some(2) |> Option.map(x -> x * 10) |> Option.unwrap())
    print([Ok(1), Ok(2)] |> Result.all())
`,
		"script.brig": "print(List.reverse([1, 2, 3]))\n",
		"doc.brig": "module Doc\n\n## ```brig repl\n## > twice([1])\n## [1, 1]\n## ```\n" +
			"pub fn twice(xs) -> List.concat(xs, xs)\n",
	})
	out, code := brigIn(t, bin, dir, "main.brig")
	if code != exitOK || out != "[1, 2]\n20\nOk([1, 2])\n" {
		t.Fatalf("module: exit %d, out %q", code, out)
	}
	out, code = brigIn(t, bin, dir, "script.brig")
	if code != exitOK || out != "[3, 2, 1]\n" {
		t.Fatalf("script: exit %d, out %q", code, out)
	}
	code, out = runBrigTest(t, bin, filepath.Join(dir, "doc.brig"))
	if code != exitOK || !strings.Contains(out, "1 passed, 0 failed") {
		t.Fatalf("brig test: exit %d, out %q", code, out)
	}
}

// §6.3: `args |> List.each(log)` исполняется.
func TestStdlibVariadicPipeEach(t *testing.T) {
	bin := buildBrig(t)
	src := "module Main\n\nfn log_all(..args) ->\n    args |> List.each(log)\n\nfn main() ->\n    log_all(1, 2, 3)\n"
	for _, cmd := range []string{"check", ""} {
		out, code := brigFile(t, bin, cmd, src)
		if code != exitOK {
			t.Fatalf("%s: exit %d, out %q", cmd, code, out)
		}
		if cmd == "" && out != "log: 1\nlog: 2\nlog: 3\n" {
			t.Fatalf("run: out %q", out)
		}
	}
}

// Приватная функция stdlib, неизвестная функция и неверная арность —
// ошибки разрешения имён, как у пользовательского модуля (§11.2, T-139).
func TestStdlibNamesChecked(t *testing.T) {
	bin := buildBrig(t)
	for _, c := range []struct{ call, want string }{
		{"List.subject([1], :x)", "subject/2 is private to List"},
		{"List.nope([1])", "undefined function List.nope/1"},
		{"Option.unwrap(Some(1), 2)", "undefined function Option.unwrap/2"},
	} {
		src := "module Main\nfn main() ->\n    print(" + c.call + ")\n"
		out, code := brigFile(t, bin, "check", src)
		if code != exitParse || !strings.Contains(out, c.want) {
			t.Errorf("%s: exit %d, out %q; want %q", c.call, code, out, c.want)
		}
	}
}

// Функции входного модуля лежат в ВМ под голыми именами; stdlib зовёт
// прелюдию как `Prelude.*`, поэтому одноимённые fn программы её не
// подменяют.
func TestStdlibIgnoresShadowedPrelude(t *testing.T) {
	bin := buildBrig(t)
	src := `module Main

fn any(_, _) -> :mine
fn len(_) -> :mine
fn map(_, _) -> :mine

fn main() ->
    print(List.member?([1, 2], 2), List.reverse([1, 2]), List.map([1], x -> x + 1))
    print(Result.all([Ok(1), Error(:e)]))
`
	out, code := brigFile(t, bin, "", src)
	if code != exitOK || out != "true [2, 1] [2]\nError(:e)\n" {
		t.Fatalf("exit %d, out %q", code, out)
	}
}

// Встроенный модуль побеждает файл с тем же именем (§11.1: сначала
// встроенный модуль, затем файл).
func TestStdlibBuiltinWinsOverFile(t *testing.T) {
	bin := buildBrig(t)
	dir := writeMods(t, map[string]string{
		"list.brig": "module List\npub fn take(_, _) -> :file\n",
		"main.brig": "module Main\nimport List\nfn main() -> print(List.take([1, 2], 1))\n",
	})
	out, code := brigIn(t, bin, dir, "main.brig")
	if code != exitOK || out != "[1]\n" {
		t.Fatalf("exit %d, out %q", code, out)
	}
}

// T-223: which_children — порядок старта, тип и таймаут (stdlib/supervisor_test.brig).
func TestSupervisorWhichChildren(t *testing.T) {
	bin := buildBrig(t)
	path := filepath.Join(findModuleRoot(t), "stdlib", "supervisor_test.brig")
	code, out := runBrigTest(t, bin, path)
	for _, name := range []string{
		"test_supervisor_which_children",
		"test_supervisor_which_children_type",
		"test_supervisor_which_children_timeout",
		"test_supervisor_initial_fn",
	} {
		if !strings.Contains(out, "ok   "+path+": "+name) && !strings.Contains(out, name) {
			t.Fatalf("missing %s\n%s", name, out)
		}
	}
	if code != exitOK || !strings.Contains(out, "0 failed") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

// Доктесты исходников stdlib проходят через `brig test` (раннер T-147).
func TestStdlibDoctestsCLI(t *testing.T) {
	bin := buildBrig(t)
	code, out := runBrigTest(t, bin, filepath.Join(findModuleRoot(t), "stdlib"))
	if code != exitOK || !strings.Contains(out, " passed, 0 failed") {
		t.Fatalf("brig test stdlib: exit %d\n%s", code, out)
	}
}
