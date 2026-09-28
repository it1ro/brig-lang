package repl_test

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/it1ro/brig-lang/internal/repl"
	"github.com/it1ro/brig-lang/internal/vm"
)

var mtimeSeq int

// writeModule пишет файл модуля и сдвигает mtime вперёд, чтобы Recompile
// увидел правку даже в пределах разрешения часов файловой системы.
func writeModule(t *testing.T, path, src string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	mtimeSeq++
	mt := time.Now().Add(time.Duration(mtimeSeq) * time.Second)
	if err := os.Chtimes(path, mt, mt); err != nil {
		t.Fatal(err)
	}
}

func newModuleSession(t *testing.T, src string) (*repl.Session, *bytes.Buffer, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "m.brig")
	writeModule(t, path, src)
	var out bytes.Buffer
	s := repl.New(vm.New(), &out)
	t.Cleanup(s.Close)
	if _, err := s.LoadModules(path); err != nil {
		t.Fatalf("LoadModules: %v (diag: %s)", err, out.String())
	}
	return s, &out, path
}

// evalValue исполняет ввод и возвращает Inspect значения последней инструкции.
func evalValue(t *testing.T, s *repl.Session, out *bytes.Buffer, src string) string {
	t.Helper()
	res, err := s.Eval(src)
	if err != nil {
		t.Fatalf("Eval %q: %v (diag: %s)", src, err, out.String())
	}
	return res[len(res)-1].Value.Inspect()
}

// TestRecompileNewInputSeesNewCode — T-208 (#246), решение D п.1: новый
// ввод `M.f()` исполняет новую версию; неизменённый файл не
// перекомпилируется; функция, удалённая из файла, из сессии пропадает.
func TestRecompileNewInputSeesNewCode(t *testing.T) {
	s, out, path := newModuleSession(t, "module M\n\nfn f() -> 1\n\nfn g() -> :old\n")

	if got := evalValue(t, s, out, "M.f()\n"); got != "1" {
		t.Fatalf("M.f() = %s, want 1", got)
	}
	// Приватная функция модуля пользователя видна в сессии (§11.4).
	if got := evalValue(t, s, out, "M.g()\n"); got != ":old" {
		t.Fatalf("M.g() = %s, want :old", got)
	}

	if mods, err := s.Recompile(); err != nil || len(mods) != 0 {
		t.Fatalf("Recompile без правок: %v, %v; want [], nil", mods, err)
	}

	writeModule(t, path, "module M\n\nfn f() -> 2\n")
	mods, err := s.Recompile()
	if err != nil {
		t.Fatalf("Recompile: %v (diag: %s)", err, out.String())
	}
	if !reflect.DeepEqual(mods, []string{"M"}) {
		t.Fatalf("Recompile() = %v, want [M]", mods)
	}
	if got := evalValue(t, s, out, "M.f()\n"); got != "2" {
		t.Fatalf("M.f() после recompile = %s, want 2", got)
	}
	if _, err := s.Eval("M.g()\n"); err == nil || !strings.Contains(err.Error(), "M.g") {
		t.Fatalf("M.g() после удаления: err = %v, want undefined M.g", err)
	}
}

// TestRecompileOldBindingsPerDD — T-208 (#246), решение D пп.2–3:
// функция-значение исполняет код, из которого получена; вызов по имени —
// версию, текущую в момент вызова: в замыкании, в новом вводе и в цикле
// работающего актора.
func TestRecompileOldBindingsPerDD(t *testing.T) {
	const v1 = `module M

fn f() -> 1

fn mk() -> () -> 1

fn caller() -> () -> f()

fn loop(p) ->
    recv
        :ping ->
            send(p, f())
            loop(p)
`
	s, out, path := newModuleSession(t, v1)

	evalValue(t, s, out, "g = M.mk()\n")
	evalValue(t, s, out, "h = M.caller()\n")
	evalValue(t, s, out, "me = self()\n")
	evalValue(t, s, out, "a = spawn(() -> M.loop(me))\n")
	evalValue(t, s, out, "send(a, :ping)\n")
	if got := evalValue(t, s, out, "recv\n    x -> x\n\n"); got != "1" {
		t.Fatalf("актор до recompile: %s, want 1", got)
	}

	writeModule(t, path, strings.NewReplacer("-> 1", "-> 2").Replace(v1))
	if _, err := s.Recompile(); err != nil {
		t.Fatalf("Recompile: %v (diag: %s)", err, out.String())
	}

	for _, c := range []struct{ src, want, why string }{
		{"g()\n", "1", "значение g = M.mk() хранит старый код"},
		{"M.mk()()\n", "2", "новый ввод — новая версия"},
		{"h()\n", "2", "вызов по имени внутри старого замыкания — новая версия"},
	} {
		if got := evalValue(t, s, out, c.src); got != c.want {
			t.Errorf("%s= %s, want %s (%s)", c.src, got, c.want, c.why)
		}
	}

	// Актор ждал в recv старого кадра: следующий вызов по имени — f() и
	// loop(p) — уже новый код.
	for i := 0; i < 2; i++ {
		evalValue(t, s, out, "send(a, :ping)\n")
		if got := evalValue(t, s, out, "recv\n    x -> x\n\n"); got != "2" {
			t.Fatalf("актор после recompile, ping %d: %s, want 2", i+1, got)
		}
	}
}

// TestRecompileLocalFnKeepsVersion — T-208 (#246): поднятая локальная fn
// принадлежит версии объемлющей функции. Старый кадр, ждущий в recv,
// вызывает свою локальную fn, даже если у новой другой набор захватов.
func TestRecompileLocalFnKeepsVersion(t *testing.T) {
	s, out, path := newModuleSession(t, `module M

fn run(p, k) ->
    fn step() -> k
    recv
        :go -> send(p, step())
`)
	evalValue(t, s, out, "me = self()\n")
	evalValue(t, s, out, "a = spawn(() -> M.run(me, 10))\n")

	// Новая step захватывает два имени вместо одного.
	writeModule(t, path, `module M

fn run(p, k) ->
    fn step() -> (k, k + 1)
    recv
        :go -> send(p, step())
`)
	if _, err := s.Recompile(); err != nil {
		t.Fatalf("Recompile: %v (diag: %s)", err, out.String())
	}
	evalValue(t, s, out, "send(a, :go)\n")
	if got := evalValue(t, s, out, "recv\n    x -> x\n\n"); got != "10" {
		t.Fatalf("старый кадр: step() = %s, want 10", got)
	}
	evalValue(t, s, out, "b = spawn(() -> M.run(me, 10))\n")
	evalValue(t, s, out, "send(b, :go)\n")
	if got := evalValue(t, s, out, "recv\n    x -> x\n\n"); got != "(10, 11)" {
		t.Fatalf("новый кадр: step() = %s, want (10, 11)", got)
	}
}

// TestRecompileCompileErrorKeepsOld — T-208 (#246): ошибка в любом модуле
// графа — ничего не заменено, сессия остаётся на старом коде; после
// исправления файла recompile проходит.
func TestRecompileCompileErrorKeepsOld(t *testing.T) {
	s, out, path := newModuleSession(t, "module M\n\nfn f() -> 1\n")

	for _, bad := range []string{
		"module M\n\nfn f() -> (\n",            // парсер
		"module M\n\nfn f() -> nope()\n",       // sema: неизвестная функция
		"module M\n\nfn f() -> Nope.T{x: 1}\n", // компилятор: неизвестный модуль
	} {
		writeModule(t, path, bad)
		out.Reset()
		if mods, err := s.Recompile(); err == nil {
			t.Fatalf("Recompile(%q) = %v, nil; want error", bad, mods)
		}
		if !strings.Contains(out.String(), "m.brig:") {
			t.Errorf("Recompile(%q): диагностика без файла: %q", bad, out.String())
		}
		if got := evalValue(t, s, out, "M.f()\n"); got != "1" {
			t.Fatalf("после ошибки M.f() = %s, want 1 (старый код)", got)
		}
	}

	writeModule(t, path, "module M\n\nfn f() -> 3\n")
	if mods, err := s.Recompile(); err != nil || !reflect.DeepEqual(mods, []string{"M"}) {
		t.Fatalf("Recompile после исправления: %v, %v", mods, err)
	}
	if got := evalValue(t, s, out, "M.f()\n"); got != "3" {
		t.Fatalf("M.f() = %s, want 3", got)
	}
}

// TestRecompileImportedModule — граф: правка импортированного модуля
// видна через вызов из входного; перекомпилирован только изменённый.
func TestRecompileImportedModule(t *testing.T) {
	dir := t.TempDir()
	entry := filepath.Join(dir, "app.brig")
	util := filepath.Join(dir, "util.brig")
	writeModule(t, entry, "module App\n\nimport Util\n\nfn run() -> Util.v()\n")
	writeModule(t, util, "module Util\n\nfn v() -> :a\n")

	var out bytes.Buffer
	s := repl.New(vm.New(), &out)
	t.Cleanup(s.Close)
	mods, err := s.LoadModules(entry)
	if err != nil {
		t.Fatalf("LoadModules: %v (diag: %s)", err, out.String())
	}
	if !reflect.DeepEqual(mods, []string{"App", "Util"}) {
		t.Fatalf("LoadModules() = %v, want [App Util]", mods)
	}
	if got := evalValue(t, s, &out, "App.run()\n"); got != ":a" {
		t.Fatalf("App.run() = %s, want :a", got)
	}

	writeModule(t, util, "module Util\n\nfn v() -> :b\n")
	mods, err = s.Recompile()
	if err != nil || !reflect.DeepEqual(mods, []string{"Util"}) {
		t.Fatalf("Recompile() = %v, %v; want [Util]", mods, err)
	}
	if got := evalValue(t, s, &out, "App.run()\n"); got != ":b" {
		t.Fatalf("App.run() = %s, want :b", got)
	}
}

// TestLoadModulesRejectsScript — script-файл (§11.3) — не модуль:
// LoadModules его не принимает (script исполняет load, T-206).
func TestLoadModulesRejectsScript(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.brig")
	writeModule(t, path, "x = 1\n")
	var out bytes.Buffer
	s := repl.New(vm.New(), &out)
	t.Cleanup(s.Close)
	if _, err := s.LoadModules(path); err == nil {
		t.Fatal("LoadModules(script): want error")
	}
}
