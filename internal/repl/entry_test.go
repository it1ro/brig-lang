package repl_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/parser"
	"github.com/it1ro/brig-lang/internal/repl"
)

// TestEntryShadowsHelper — T-245: fn программы сессии (доктест) с именем
// хелпера затеняет хелпер голым именем, info — как у связывания,
// хелпер остаётся как Repl.v; типы модуля видны вводу.
func TestEntryShadowsHelper(t *testing.T) {
	src := "module Short\n\ntype Pt { x: Int }\n\npub fn v(x) -> x + 100\n"
	prog, err := parser.ParseProgram(parser.ModeModule, src)
	if err != nil {
		t.Fatal(err)
	}
	mods := []compiler.Module{{Name: "Short", Path: "short.brig", Prog: prog}}
	img, err := compiler.New().CompileProgram(mods)
	if err != nil {
		t.Fatal(err)
	}
	s, out := helperSession(t)
	if err := s.UseEntry(&repl.Entry{Mods: mods, Image: img}); err != nil {
		t.Fatal(err)
	}
	if want := "info: short.brig:5:1: `v` shadows repl helper; use `Repl.v` if the helper was intended"; !strings.Contains(out.String(), want) {
		t.Fatalf("diag:\n%s\nwant %q", out.String(), want)
	}
	if got := mustEval(t, s, out, "v(1)\n"); got.Inspect() != "101" {
		t.Fatalf("v(1) = %s, want 101 (module fn)", got.Inspect())
	}
	if got := mustEval(t, s, out, "Repl.v(1)\n"); got.Inspect() != "101" {
		t.Fatalf("Repl.v(1) = %s, want value of input 1", got.Inspect())
	}
	if got := mustEval(t, s, out, "Pt{ x: 1 }.x\n"); got.Inspect() != "1" {
		t.Fatalf("Pt{ x: 1 }.x = %s", got.Inspect())
	}
}

// TestLoadedScriptFnShadowsHelper — T-245, сессия -i: fn загруженного
// script-файла — связывание сессии, затеняет хелпер с info (fn модуля
// в -i видны по имени модуля, `M.v`, и с хелпером не пересекаются).
func TestLoadedScriptFnShadowsHelper(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "tools.brig")
	if err := os.WriteFile(script, []byte("fn v(x) -> x + 100\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, out := helperSession(t)
	if err := s.LoadFile(script, true); err != nil {
		t.Fatalf("LoadFile: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "`v` shadows repl helper; use `Repl.v`") {
		t.Fatalf("diag:\n%s", out.String())
	}
	if got := mustEval(t, s, out, "v(1)\n"); got.Inspect() != "101" {
		t.Fatalf("v(1) = %s, want 101", got.Inspect())
	}
	if got := mustEval(t, s, out, "Repl.v(1)\n"); got.Inspect() != "101" {
		t.Fatalf("Repl.v(1) = %s", got.Inspect())
	}
}
