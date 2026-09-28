package repl_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/it1ro/brig-lang/internal/highlight"
	"github.com/it1ro/brig-lang/internal/repl"
	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/vm"
)

func helperSession(t *testing.T) (*repl.Session, *bytes.Buffer) {
	t.Helper()
	var out bytes.Buffer
	s := repl.New(vm.New(), &out)
	t.Cleanup(s.Close)
	return s, &out
}

func mustEval(t *testing.T, s *repl.Session, out *bytes.Buffer, src string) runtime.Value {
	t.Helper()
	res, err := s.Eval(src)
	if err != nil {
		t.Fatalf("Eval %q: %v\n%s", src, err, out.String())
	}
	if len(res) == 0 {
		t.Fatalf("Eval %q: no result", src)
	}
	return res[len(res)-1].Value
}

func replFile(name string) string {
	return filepath.Join("..", "..", "testdata", "repl", name)
}

// TestHelperH — T-206: h печатает сигнатуры и ##, h(M) — модуль,
// код в доках идёт через highlight, Session.Doc отдаёт тот же текст.
func TestHelperH(t *testing.T) {
	s, out := helperSession(t)

	mustEval(t, s, out, "h(len)\n")
	got := out.String()
	for _, frag := range []string{"len/1", "len(v)", "нет документации"} {
		if !strings.Contains(got, frag) {
			t.Fatalf("h(len) missing %q:\n%s", frag, got)
		}
	}
	doc, ok := s.Doc("len")
	if !ok || doc != got {
		t.Fatalf("Doc(len) = %q, %v; h printed %q", doc, ok, got)
	}
	if _, ok := s.Doc("no_such"); ok {
		t.Fatal("Doc(no_such) found")
	}

	out.Reset()
	mustEval(t, s, out, "h(Map)\n")
	got = out.String()
	for _, frag := range []string{"Map\n", "get/2", "keys/1", "нет документации"} {
		if !strings.Contains(got, frag) {
			t.Fatalf("h(Map) missing %q:\n%s", frag, got)
		}
	}

	out.Reset()
	mustEval(t, s, out, "h(Map.get)\n")
	if !strings.Contains(out.String(), "Map.get/2") || !strings.Contains(out.String(), "нет документации") {
		t.Fatalf("h(Map.get):\n%s", out.String())
	}

	demo := replFile("demo.brig")
	mustEval(t, s, out, "load(\""+demo+"\")\n")
	out.Reset()
	mustEval(t, s, out, "h(Demo.add)\n")
	got = out.String()
	for _, frag := range []string{"Demo.add/2", "Demo.add(a, b)", "Складывает."} {
		if !strings.Contains(got, frag) {
			t.Fatalf("h(Demo.add) missing %q:\n%s", frag, got)
		}
	}
	if doc, ok := s.Doc("Demo.add"); !ok || !strings.Contains(doc, "Складывает.") {
		t.Fatalf("Doc(Demo.add) = %q, %v", doc, ok)
	}

	out.Reset()
	mustEval(t, s, out, "h(Demo)\n")
	got = out.String()
	for _, frag := range []string{"Модуль демонстрации.", "add/2", "hidden/1"} {
		if !strings.Contains(got, frag) {
			t.Fatalf("h(Demo) missing %q:\n%s", frag, got)
		}
	}

	out.Reset()
	mustEval(t, s, out, "h(Demo.hidden)\n")
	if !strings.Contains(out.String(), "нет документации") {
		t.Fatalf("h(Demo.hidden):\n%s", out.String())
	}

	out.Reset()
	s.SetPalette(highlight.PaletteFromEnv(func(string) (string, bool) { return "", false }))
	mustEval(t, s, out, "h(Demo.add)\n")
	if !strings.Contains(out.String(), "\x1b[") {
		t.Fatalf("h(Demo.add) did not highlight the doc sample:\n%s", out.String())
	}

	out.Reset()
	s.SetPalette(highlight.Palette{})
	mustEval(t, s, out, "## Факториал.\nfn fact(n) ->\n    n\n")
	mustEval(t, s, out, "h(fact)\n")
	if !strings.Contains(out.String(), "fact(n)") || !strings.Contains(out.String(), "Факториал.") {
		t.Fatalf("h(fact):\n%s", out.String())
	}

	out.Reset()
	mustEval(t, s, out, "fn f(x) -> x + 1\n")
	mustEval(t, s, out, "dis(f)\n")
	if !strings.Contains(out.String(), "arity=1") || !strings.Contains(out.String(), "ADD") {
		t.Fatalf("dis(f):\n%s", out.String())
	}

	out.Reset()
	timed := mustEval(t, s, out, "time(() -> 2 + 2)\n")
	if timed.Kind != runtime.KindTuple || len(timed.Tuple) != 2 || timed.Tuple[1].Inspect() != "4" {
		t.Fatalf("time: %s", timed.Inspect())
	}
	if timed.Tuple[0].Kind != runtime.KindInt || !timed.Tuple[0].IsSmall || timed.Tuple[0].SmallInt < 0 {
		t.Fatalf("time us: %s", timed.Tuple[0].Inspect())
	}
}

// TestHelperV — T-206: v() — последний пронумерованный ввод, v(n) — ввод n,
// номера нет — (:no_value, n). () номер не получает.
func TestHelperV(t *testing.T) {
	s, out := helperSession(t)
	if got := mustEval(t, s, out, "1 + 1\n"); got.Inspect() != "2" {
		t.Fatalf("1+1 = %s", got.Inspect())
	}
	if got := mustEval(t, s, out, "()\n"); got.Kind != runtime.KindUnit {
		t.Fatalf("() = %s", got.Inspect())
	}
	if got := mustEval(t, s, out, "3\n"); got.Inspect() != "3" {
		t.Fatalf("3 = %s", got.Inspect())
	}
	if got := mustEval(t, s, out, "v()\n"); got.Inspect() != "3" {
		t.Fatalf("v() = %s, want 3", got.Inspect())
	}
	if got := mustEval(t, s, out, "v(1)\n"); got.Inspect() != "2" {
		t.Fatalf("v(1) = %s, want 2", got.Inspect())
	}
	_, err := s.Eval("v(9)\n")
	if err == nil || !strings.Contains(err.Error(), ":no_value") || !strings.Contains(err.Error(), "9") {
		t.Fatalf("v(9): %v", err)
	}
}

// TestHelperFlush — T-206: flush печатает сообщения ящика и снимает их.
func TestHelperFlush(t *testing.T) {
	s, out := helperSession(t)
	mustEval(t, s, out, "me = self()\nsend(me, :a)\nsend(me, :b)\n")
	out.Reset()
	got := mustEval(t, s, out, "flush()\n")
	if got.Inspect() != "[:a, :b]" {
		t.Fatalf("flush = %s", got.Inspect())
	}
	if !strings.Contains(out.String(), ":a") || !strings.Contains(out.String(), ":b") {
		t.Fatalf("flush did not print:\n%s", out.String())
	}
	if n := mustEval(t, s, out, "mailbox_size()\n"); n.Inspect() != "0" {
		t.Fatalf("mailbox after flush = %s", n.Inspect())
	}
	out.Reset()
	mustEval(t, s, out, "i(self())\n")
	if !strings.Contains(out.String(), "Pid alive") || !strings.Contains(out.String(), "mailbox=0") {
		t.Fatalf("i(self):\n%s", out.String())
	}
	out.Reset()
	mustEval(t, s, out, "i([1, 2, 3])\n")
	if !strings.Contains(out.String(), "List size=3") {
		t.Fatalf("i(list):\n%s", out.String())
	}
	out.Reset()
	mustEval(t, s, out, "i({id: 1})\n")
	if !strings.Contains(out.String(), "Record size=1") || !strings.Contains(out.String(), "fields=id") {
		t.Fatalf("i(record):\n%s", out.String())
	}
}

// TestHelperBindingsReset — T-206: bindings() — карта привязок; reset
// снимает их, но не актор сессии, ящик и заспавненные акторы.
func TestHelperBindingsReset(t *testing.T) {
	s, out := helperSession(t)
	me := mustEval(t, s, out, "self()\n")
	mustEval(t, s, out, `x = 1
y = "a"
me = self()
send(me, :kept)
fn later() ->
    recv
        _ -> ()
    after 30 -> ()
    send(me, :after)
spawn(later)
`)
	b := mustEval(t, s, out, "bindings()\n")
	if !strings.Contains(b.Inspect(), `"x" => 1`) || !strings.Contains(b.Inspect(), `"y" => "a"`) {
		t.Fatalf("bindings = %s", b.Inspect())
	}
	mustEval(t, s, out, "reset()\n")
	if len(s.Bindings()) != 0 {
		t.Fatalf("bindings after reset: %v", s.Bindings())
	}
	if got := mustEval(t, s, out, "bindings()\n"); got.Inspect() != "%{}" {
		t.Fatalf("bindings() after reset = %s", got.Inspect())
	}
	if got := mustEval(t, s, out, "self()\n"); got.Inspect() != me.Inspect() {
		t.Fatalf("self after reset = %s, want %s", got.Inspect(), me.Inspect())
	}
	// Ящик и актор, заспавненный до reset, переживают сброс привязок.
	time.Sleep(200 * time.Millisecond)
	got := mustEval(t, s, out, "flush()\n")
	if !strings.Contains(got.Inspect(), ":kept") || !strings.Contains(got.Inspect(), ":after") {
		t.Fatalf("mailbox/actor after reset: %s", got.Inspect())
	}
}

// TestHelperLoad — T-206: load модуля и script-файла; ошибка — :load_error.
func TestHelperLoad(t *testing.T) {
	s, out := helperSession(t)
	demo := replFile("demo.brig")
	mustEval(t, s, out, "load(\""+demo+"\")\n")
	if got := mustEval(t, s, out, "Demo.add(2, 3)\n"); got.Inspect() != "5" {
		t.Fatalf("Demo.add = %s", got.Inspect())
	}
	script := replFile("script.brig")
	mustEval(t, s, out, "load(\""+script+"\")\n")
	if got := mustEval(t, s, out, "m\n"); got.Inspect() != "42" {
		t.Fatalf("script m = %s", got.Inspect())
	}
	if got := mustEval(t, s, out, "v()\n"); got.Inspect() != "42" {
		t.Fatalf("v() after script = %s, want 42 (m)", got.Inspect())
	}
	missing := filepath.Join(t.TempDir(), "nope.brig")
	_, err := s.Eval("load(\"" + missing + "\")\n")
	if err == nil || !strings.Contains(err.Error(), ":load_error") || !strings.Contains(err.Error(), missing) {
		t.Fatalf("missing load: %v", err)
	}
	// Привязки до ошибки на месте.
	if got := mustEval(t, s, out, "m\n"); got.Inspect() != "42" {
		t.Fatalf("m after failed load = %s", got.Inspect())
	}
	// raise в script — тоже :load_error, прежние привязки живы.
	bad := filepath.Join(t.TempDir(), "bad.brig")
	if err := os.WriteFile(bad, []byte("k = 7\nraise(:boom)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = s.Eval("load(\"" + bad + "\")\n")
	if err == nil || !strings.Contains(err.Error(), ":load_error") {
		t.Fatalf("raise in script: %v", err)
	}
	if got := mustEval(t, s, out, "k\n"); got.Inspect() != "7" {
		t.Fatalf("k after script raise = %s", got.Inspect())
	}
}

// TestHelperRegister — T-206: RegisterHelpers и Repl.register из кода
// модуля делают функцию видимой без префикса.
func TestHelperRegister(t *testing.T) {
	s, out := helperSession(t)
	tools := replFile("tools.brig")
	mustEval(t, s, out, "load(\""+tools+"\")\n")
	if err := s.RegisterHelpers("Tools"); err != nil {
		t.Fatal(err)
	}
	if got := mustEval(t, s, out, "routes()\n"); got.Inspect() != "routes" {
		t.Fatalf("routes() = %s", got.Inspect())
	}
	// Тот же модуль в новой сессии регистрирует себя из своего кода.
	s2, out2 := helperSession(t)
	mustEval(t, s2, out2, "load(\""+tools+"\")\n")
	mustEval(t, s2, out2, "Tools.console()\n")
	if got := mustEval(t, s2, out2, "routes()\n"); got.Inspect() != "routes" {
		t.Fatalf("routes() after console = %s", got.Inspect())
	}
}

// TestHelperShadowing — T-206: связывание имени хелпера — info, значение
// затеняет голый вызов, Repl.h остаётся.
func TestHelperShadowing(t *testing.T) {
	s, out := helperSession(t)
	mustEval(t, s, out, "h = 1\n")
	if !strings.Contains(out.String(), "shadows repl helper") || !strings.Contains(out.String(), "Repl.h") {
		t.Fatalf("shadow diag:\n%s", out.String())
	}
	if got := mustEval(t, s, out, "h\n"); got.Inspect() != "1" {
		t.Fatalf("shadowed h = %s", got.Inspect())
	}
	out.Reset()
	mustEval(t, s, out, "Repl.h(len)\n")
	if !strings.Contains(out.String(), "len/1") {
		t.Fatalf("Repl.h(len):\n%s", out.String())
	}
}
