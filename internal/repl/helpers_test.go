package repl_test

import (
	"bytes"
	"errors"
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
	if got := mustEval(t, s, out, "routes()\n"); got.Display() != "routes" {
		t.Fatalf("routes() = %s", got.Inspect())
	}
	// T-143: регистрируются только pub-функции.
	if _, err := s.Eval("hidden()\n"); err == nil {
		t.Fatal("non-pub hidden() registered as a helper")
	}
	// Тот же модуль в новой сессии регистрирует себя из своего кода.
	s2, out2 := helperSession(t)
	mustEval(t, s2, out2, "load(\""+tools+"\")\n")
	mustEval(t, s2, out2, "Tools.console()\n")
	if got := mustEval(t, s2, out2, "routes()\n"); got.Display() != "routes" {
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

// TestHelperRecompile — T-214 (#309) п.1: recompile() и Repl.recompile()
// зовут Session.Recompile и печатают перекомпилированные модули; ошибка
// компиляции — (:load_error, (path, msg)), старый код остаётся.
func TestHelperRecompile(t *testing.T) {
	s, out, path := newModuleSession(t, "module M\n\nfn f() -> 1\n")

	out.Reset()
	if got := evalValue(t, s, out, "recompile()\n"); got != "()" {
		t.Fatalf("recompile() без правок = %s", got)
	}
	if !strings.Contains(out.String(), "нет изменений") {
		t.Fatalf("recompile() без правок печатает:\n%s", out.String())
	}

	writeModule(t, path, "module M\n\nfn f() -> 2\n")
	out.Reset()
	evalValue(t, s, out, "recompile()\n")
	if !strings.Contains(out.String(), "перекомпилировано: M") {
		t.Fatalf("recompile() печатает:\n%s", out.String())
	}
	if got := evalValue(t, s, out, "M.f()\n"); got != "2" {
		t.Fatalf("M.f() после recompile() = %s, want 2", got)
	}

	writeModule(t, path, "module M\n\nfn f() -> 3\n")
	out.Reset()
	evalValue(t, s, out, "Repl.recompile()\n")
	if got := evalValue(t, s, out, "M.f()\n"); got != "3" {
		t.Fatalf("M.f() после Repl.recompile() = %s, want 3", got)
	}

	writeModule(t, path, "module M\n\nfn f( -> 4\n")
	_, err := s.Eval("recompile()\n")
	if err == nil || !strings.Contains(err.Error(), ":load_error") || !strings.Contains(err.Error(), path) {
		t.Fatalf("recompile() с ошибкой: %v", err)
	}
	if got := evalValue(t, s, out, "M.f()\n"); got != "3" {
		t.Fatalf("M.f() после неудачного recompile() = %s, want 3", got)
	}
}

// TestHelperNestedRecv — T-214 (#309) п.2: recv во вложенном вызове (time,
// script из load) ждёт сообщения от заспавненных акторов, а не падает с
// internal:; Interrupt снимает ожидающий вложенный вызов.
func TestHelperNestedRecv(t *testing.T) {
	s, out := helperSession(t)
	mustEval(t, s, out, `fn waiter() ->
    me = self()
    spawn(() -> send(me, :hi))
    recv
        m -> m
`)
	got := mustEval(t, s, out, "time(waiter)\n")
	if got.Kind != runtime.KindTuple || len(got.Tuple) != 2 || got.Tuple[1].Inspect() != ":hi" {
		t.Fatalf("time(waiter) = %s", got.Inspect())
	}

	// Сообщение приходит после таймера другого актора.
	mustEval(t, s, out, `fn sleeper(dst) ->
    recv
        _ -> ()
    after 30 -> send(dst, :late)
fn late() ->
    me = self()
    spawn(() -> sleeper(me))
    recv
        m -> m
`)
	got = mustEval(t, s, out, "time(late)\n")
	if got.Kind != runtime.KindTuple || got.Tuple[1].Inspect() != ":late" {
		t.Fatalf("time(late) = %s", got.Inspect())
	}

	// recv … after во вложенном вызове.
	got = mustEval(t, s, out, `fn timeout() ->
    recv
        m -> m
    after 20 -> :timeout
time(timeout)
`)
	if got.Kind != runtime.KindTuple || got.Tuple[1].Inspect() != ":timeout" {
		t.Fatalf("time(timeout) = %s", got.Inspect())
	}

	script := filepath.Join(t.TempDir(), "recv.brig")
	src := "me = self()\nspawn(() -> send(me, 7))\ngot = recv\n    m -> m\n"
	if err := os.WriteFile(script, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	mustEval(t, s, out, "load(\""+script+"\")\n")
	if got := mustEval(t, s, out, "got\n"); got.Inspect() != "7" {
		t.Fatalf("got after script recv = %s", got.Inspect())
	}

	mustEval(t, s, out, "fn block() ->\n    recv\n        m -> m\n")
	done := make(chan error, 1)
	go func() {
		_, err := s.Eval("time(block)\n")
		done <- err
	}()
	time.Sleep(50 * time.Millisecond)
	s.Interrupt()
	select {
	case err := <-done:
		if !errors.Is(err, vm.ErrInterrupted) {
			t.Fatalf("interrupted time(block): %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Interrupt не снял time(block)")
	}
	if got := mustEval(t, s, out, "1 + 1\n"); got.Inspect() != "2" {
		t.Fatalf("ввод после прерывания = %s", got.Inspect())
	}
}

// TestHelperAwaitNested — T-223: await внутри хелпера (CallNested) ждёт
// ответ, пока планировщик крутит сервер. Без этого tree() не может
// звать Supervisor.which_children.
func TestHelperAwaitNested(t *testing.T) {
	s, out := helperSession(t)
	mustEval(t, s, out, "fn srv() ->\n    recv\n        (:call, from, req) -> Server.reply(from, req)\n")
	mustEval(t, s, out, "pid = spawn(srv)\n")
	got := mustEval(t, s, out, "time(() -> Server.call(pid, :ping, 1000))\n")
	if got.Kind != runtime.KindTuple || len(got.Tuple) != 2 || got.Tuple[1].Inspect() != "Ok(:ping)" {
		t.Fatalf("time(Server.call) = %s", got.Inspect())
	}
}

// TestHelperObserverLeavesActors — T-223: tree() не кладёт сообщения
// чужим акторам и не останавливает их. sticky принимает только :inc:
// чужое сообщение убило бы его через :recv_clause.
func TestHelperObserverLeavesActors(t *testing.T) {
	s, out := helperSession(t)
	mustEval(t, s, out, "fn sticky() ->\n    recv\n        :inc -> sticky()\n")
	mustEval(t, s, out, "fn echo() ->\n    recv\n        (:ping, from) ->\n            send(from, :pong)\n            echo()\n")
	mustEval(t, s, out, "pid = spawn(sticky)\n")
	mustEval(t, s, out, "sup = Supervisor.start({ strategy: :one_for_one, max_restarts: 1, within: 5000, children: [{ name: :e, start: () -> spawn_linked(echo), restart: :permanent }] })\n")
	if got := mustEval(t, s, out, "mailbox_size(pid)\n"); got.Inspect() != "0" {
		t.Fatalf("mailbox before = %s", got.Inspect())
	}
	mustEval(t, s, out, "tree()\n")
	if got := mustEval(t, s, out, "mailbox_size(pid)\n"); got.Inspect() != "0" {
		t.Fatalf("sticky mailbox after tree = %s\n%s", got.Inspect(), out.String())
	}
	if got := mustEval(t, s, out, "mailbox_size()\n"); got.Inspect() != "0" {
		t.Fatalf("session mailbox after tree = %s", got.Inspect())
	}
	if got := mustEval(t, s, out, "match Actor.info(pid)\n    Some(_) -> true\n    None -> false\n"); got.Inspect() != "true" {
		t.Fatalf("sticky dead after tree\n%s", out.String())
	}
	mustEval(t, s, out, "send(pid, :inc)\n")
	mustEval(t, s, out, "recv\n    :never -> ()\nafter 0 -> ()\n")
	if got := mustEval(t, s, out, "match Actor.info(pid)\n    Some(_) -> true\n    None -> false\n"); got.Inspect() != "true" {
		t.Fatalf("sticky dead after :inc\n%s", out.String())
	}
	mustEval(t, s, out, "cs = Supervisor.which_children(sup)\n")
	mustEval(t, s, out, "send(cs[0][1], (:ping, self()))\n")
	if got := mustEval(t, s, out, "recv\n    m -> m\nafter 1000 -> :timeout\n"); got.Inspect() != ":pong" {
		t.Fatalf("echo after tree = %s\n%s", got.Inspect(), out.String())
	}
	if got := mustEval(t, s, out, "Supervisor.stop(sup, :shutdown, { timeout: 1000 })\n"); got.Inspect() != "Ok(())" {
		t.Fatalf("stop after tree = %s", got.Inspect())
	}
	mustEval(t, s, out, "Repl.tree()\n")
	for _, src := range []string{"top(0)\n", "top(-1)\n", "top(1.5)\n", "top(:n)\n", "info(1)\n"} {
		_, err := s.Eval(src)
		var rerr *vm.ErrRaise
		if !errors.As(err, &rerr) {
			t.Fatalf("%s: %v", src, err)
		}
	}
}

// TestHelperNestedTrace — T-214 (#309) п.3: непойманный raise в time(f)
// несёт stack trace, как в обычном вводе.
func TestHelperNestedTrace(t *testing.T) {
	s, out := helperSession(t)
	mustEval(t, s, out, "fn boom() -> raise(:boom)\n")
	_, err := s.Eval("time(boom)\n")
	var rerr *vm.ErrRaise
	if !errors.As(err, &rerr) {
		t.Fatalf("time(boom): %v", err)
	}
	found := false
	for _, fr := range rerr.Trace {
		if strings.Contains(fr.Func, "boom") {
			found = true
		}
	}
	if !found {
		t.Fatalf("time(boom) trace = %+v", rerr.Trace)
	}
	// Пойманный raise trace не собирает и до ввода не доходит.
	if got := mustEval(t, s, out, "r = trap(time(boom))\n"); !strings.Contains(got.Inspect(), ":boom") {
		t.Fatalf("trap time(boom) = %s", got.Inspect())
	}
}

// TestHelperDocNames — T-214 (#309) п.4–8: имена в h и dis — как ввёл
// пользователь; документация прелюдии — из сигнатур sema.
func TestHelperDocNames(t *testing.T) {
	t.Run("bare helper", func(t *testing.T) {
		s, out := helperSession(t)
		mustEval(t, s, out, "h(v)\n")
		got := out.String()
		if strings.Contains(got, "Repl.") || !strings.Contains(got, "v/0") || !strings.Contains(got, "v()") {
			t.Fatalf("h(v):\n%s", got)
		}
		out.Reset()
		mustEval(t, s, out, "Repl.h(Repl.v)\n")
		if !strings.Contains(out.String(), "Repl.v/0") {
			t.Fatalf("Repl.h(Repl.v):\n%s", out.String())
		}
		out.Reset()
		mustEval(t, s, out, "h(Repl)\n")
		if !strings.Contains(out.String(), "recompile/0") {
			t.Fatalf("h(Repl):\n%s", out.String())
		}
	})
	t.Run("json arities", func(t *testing.T) {
		s, out := helperSession(t)
		mustEval(t, s, out, "h(Json)\n")
		for _, frag := range []string{"decode/1", "encode/1", "encode/2"} {
			if !strings.Contains(out.String(), frag) {
				t.Fatalf("h(Json) missing %q:\n%s", frag, out.String())
			}
		}
	})
	t.Run("local fn keeps prelude doc", func(t *testing.T) {
		s, out := helperSession(t)
		mustEval(t, s, out, "## Своя.\nfn len(x) -> 0\n")
		out.Reset()
		mustEval(t, s, out, "h(Prelude.len)\n")
		if !strings.Contains(out.String(), "len(v)") || strings.Contains(out.String(), "Своя.") {
			t.Fatalf("h(Prelude.len):\n%s", out.String())
		}
		out.Reset()
		mustEval(t, s, out, "h(len)\n")
		if !strings.Contains(out.String(), "len(x)") || !strings.Contains(out.String(), "Своя.") {
			t.Fatalf("h(len) локальной:\n%s", out.String())
		}
	})
	t.Run("string is not a module", func(t *testing.T) {
		s, _ := helperSession(t)
		_, err := s.Eval("h(\"Map\")\n")
		if err == nil || !strings.Contains(err.Error(), ":type_error") {
			t.Fatalf("h(\"Map\"): %v", err)
		}
		_, err = s.Eval("h(Foo)\n")
		if err == nil || strings.Contains(err.Error(), `"Foo"`) {
			t.Fatalf("h(Foo): %v", err)
		}
	})
	t.Run("dis local fn", func(t *testing.T) {
		s, out := helperSession(t)
		mustEval(t, s, out, "fn sq(x) -> x * x\n")
		out.Reset()
		mustEval(t, s, out, "dis(sq)\n")
		if strings.Contains(out.String(), "__repl__") || !strings.Contains(out.String(), "== sq ") {
			t.Fatalf("dis(sq):\n%s", out.String())
		}
	})
}
