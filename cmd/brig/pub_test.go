package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T-143: функция модуля приватна по умолчанию, `pub fn` её экспортирует
// (§11.2). Вызов приватной функции другого модуля — ошибка brig check и
// запуска, exit 1; main вызывается тулчейном без pub.

func writeMods(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestPubFnCallableFromOtherModule(t *testing.T) {
	bin := buildBrig(t)
	dir := writeMods(t, map[string]string{
		"util.brig": "module Util\npub fn twice(x) -> scale(x)\nfn scale(x) -> x * 2\n",
		"main.brig": "module Main\nimport Util\nfn main() -> print(Util.twice(21))\n",
	})
	if out, code := brigIn(t, bin, dir, "check", "main.brig"); code != exitOK {
		t.Fatalf("check: exit %d\n%s", code, out)
	}
	out, code := brigIn(t, bin, dir, "main.brig")
	if code != exitOK || strings.TrimSpace(out) != "42" {
		t.Fatalf("run: exit %d, out %q", code, out)
	}
}

func TestPrivateFnCallFromOtherModuleIsError(t *testing.T) {
	bin := buildBrig(t)
	dir := writeMods(t, map[string]string{
		"util.brig": "module Util\npub fn twice(x) -> scale(x)\nfn scale(x) -> x * 2\n",
		"main.brig": "module Main\nimport Util\nfn main() ->\n    print(Util.scale(1))\n",
	})
	const frag = "main.brig:4:11: scale/1 is private to Util"
	for _, cmd := range []string{"check", ""} {
		args := []string{"main.brig"}
		if cmd != "" {
			args = []string{cmd, "main.brig"}
		}
		out, code := brigIn(t, bin, dir, args...)
		if code != exitParse || !strings.Contains(out, "error: ") || !strings.Contains(out, frag) {
			t.Fatalf("%s: exit %d, out %q; want %d and %q", cmd, code, out, exitParse, frag)
		}
	}

	// Клозы одной функции с pub и без — ошибка контекстного анализа.
	dir = writeMods(t, map[string]string{
		"main.brig": "module Main\npub fn f(0) -> 0\nfn f(n) -> n\nfn main() -> print(f(1))\n",
	})
	out, code := brigIn(t, bin, dir, "check", "main.brig")
	if code != exitParse || !strings.Contains(out, "main.brig:3:1: ") || !strings.Contains(out, "f") {
		t.Fatalf("mixed pub: exit %d, out %q", code, out)
	}
}

func TestMainIsEntryWithoutPub(t *testing.T) {
	bin := buildBrig(t)
	// Внутри модуля приватные функции видны как раньше.
	out, code := brigFile(t, bin, "", "module Main\nfn helper() -> 7\nfn main() -> print(helper())\n")
	if code != exitOK || strings.TrimSpace(out) != "7" {
		t.Fatalf("run: exit %d, out %q", code, out)
	}
}
