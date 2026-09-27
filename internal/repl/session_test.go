package repl_test

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/repl"
	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/vm"
)

// TestEvalMultipleStatements — T-201: несколько инструкций за один ввод
// исполняются по порядку, каждая — своя область (§11.4): повторное
// связывание — shadowing, замыкание видит связывание своей инструкции.
func TestEvalMultipleStatements(t *testing.T) {
	var out bytes.Buffer
	s := repl.New(vm.New(), &out)
	t.Cleanup(s.Close)

	res, err := s.Eval("x = 1\nf = () -> x\nx = 2\n[x, f()]\n")
	if err != nil {
		t.Fatalf("Eval: %v (diag: %s)", err, out.String())
	}
	if len(res) != 4 {
		t.Fatalf("results: got %d, want 4: %v", len(res), res)
	}
	wantNames := []string{"x", "f", "x", ""}
	for i, r := range res {
		if r.Name != wantNames[i] {
			t.Errorf("res[%d].Name = %q, want %q", i, r.Name, wantNames[i])
		}
		if r.Input != 1 {
			t.Errorf("res[%d].Input = %d, want 1", i, r.Input)
		}
	}
	if got := res[3].Value.Inspect(); got != "[2, 1]" {
		t.Fatalf("[x, f()] = %s, want [2, 1]", got)
	}

	// Локальная fn: значение инструкции — (), ввод номера не получает;
	// следующая инструкция того же ввода её вызывает.
	res, err = s.Eval("fn fact(n) ->\n    match n\n        0 -> 1\n        _ -> n * fact(n - 1)\nfact(5)\n")
	if err != nil {
		t.Fatalf("Eval fn: %v (diag: %s)", err, out.String())
	}
	if len(res) != 2 || res[0].Name != "fact" || res[0].Value.Kind != runtime.KindUnit {
		t.Fatalf("fn fact: got %v", res)
	}
	if res[1].Value.Inspect() != "120" || res[1].Input != 2 {
		t.Fatalf("fact(5): got %v, want 120 as input 2", res[1])
	}
	if res, err := s.Eval("fn g() -> 1\n"); err != nil || res[0].Input != 0 {
		t.Fatalf("fn g: got %v, %v; want no input number", res, err)
	}
	if s.Next() != 3 {
		t.Fatalf("Next() = %d, want 3", s.Next())
	}

	// raise во второй инструкции: первая уже связала имя, третья не
	// исполняется.
	res, err = s.Eval("a = 10\nraise(:boom)\nb = 20\n")
	if err == nil || len(res) != 1 || res[0].Name != "a" {
		t.Fatalf("raise mid-input: got %v, %v", res, err)
	}
	names := map[string]bool{}
	for _, b := range s.Bindings() {
		names[b.Name] = true
	}
	if !names["a"] || names["b"] {
		t.Fatalf("bindings after raise: %v, want a without b", s.Bindings())
	}

	// Ошибка разбора: ни одна инструкция ввода не исполняется.
	if res, err := s.Eval("c = 1\nd = )\n"); err == nil || len(res) != 0 {
		t.Fatalf("parse error: got %v, %v", res, err)
	}
	for _, b := range s.Bindings() {
		if b.Name == "c" {
			t.Fatalf("binding c from input with parse error")
		}
	}

	s.Reset()
	if len(s.Bindings()) != 0 {
		t.Fatalf("Reset: bindings %v", s.Bindings())
	}
}

// TestEvalRedefinedLocalFnSnapshot — T-201: переопределение локальной fn
// в следующем вводе не подменяет её у ранее созданного замыкания (N12):
// глобальные имена вложенных fn у разных инструкций различаются.
func TestEvalRedefinedLocalFnSnapshot(t *testing.T) {
	var out bytes.Buffer
	s := repl.New(vm.New(), &out)
	t.Cleanup(s.Close)
	for _, src := range []string{
		"fn k(n) ->\n    fn inner(m) -> m + 1\n    inner(n)\n\n",
		"old = () -> k(1)\n",
		"fn k(n) ->\n    fn inner(m) -> m + 100\n    inner(n)\n\n",
	} {
		if _, err := s.Eval(src); err != nil {
			t.Fatalf("Eval(%q): %v (diag: %s)", src, err, out.String())
		}
	}
	res, err := s.Eval("[old(), k(1)]\n")
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if got := res[0].Value.Inspect(); got != "[2, 101]" {
		t.Fatalf("[old(), k(1)] = %s, want [2, 101]", got)
	}

	// Рекурсивный вызов в переопределённой fn — в новую fn, не в прежнюю.
	for _, src := range []string{
		"fn c(n) -> if n == 0 then 0 else c(n - 1)\n",
		"fn c(n) -> if n == 0 then 7 else c(n - 1)\n",
	} {
		if _, err := s.Eval(src); err != nil {
			t.Fatalf("Eval(%q): %v (diag: %s)", src, err, out.String())
		}
	}
	res, err = s.Eval("c(2)\n")
	if err != nil || res[0].Value.Inspect() != "7" {
		t.Fatalf("c(2): got %v, %v; want 7", res, err)
	}
}

// TestHighlightEnv — T-203: привязка сессии видна подсветке, прелюдия и
// хелперы — тоже.
func TestHighlightEnv(t *testing.T) {
	s := repl.New(vm.New(), io.Discard)
	t.Cleanup(s.Close)
	env := s.HighlightEnv()
	if !env.Prelude["map"] || !env.Helpers["h"] || !env.Modules["Vec"]["len"] {
		t.Fatalf("prelude/helpers/Vec.len missing: %+v", env)
	}
	if _, err := s.Eval("x = 1\n"); err != nil {
		t.Fatal(err)
	}
	if !s.HighlightEnv().Bindings["x"] {
		t.Fatal("binding x missing")
	}
}

// TestReplUndefinedStillRuntime — T-139: строка REPL видит функции
// предыдущих строк; неизвестное имя остаётся ошибкой рантайма, не check.
func TestReplUndefinedStillRuntime(t *testing.T) {
	var out bytes.Buffer
	s := repl.New(vm.New(), &out)
	t.Cleanup(s.Close)
	if _, err := s.Eval("fn f(x) -> x\n"); err != nil {
		t.Fatalf("def: %v (diag %s)", err, out.String())
	}
	res, err := s.Eval("f(1)\n")
	if err != nil || len(res) != 1 || res[0].Value.Inspect() != "1" {
		t.Fatalf("f from previous line: %v, %v (diag %s)", res, err, out.String())
	}
	out.Reset()
	_, err = s.Eval("nope(1)\n")
	if err == nil || !strings.Contains(err.Error(), "undefined") {
		t.Fatalf("err = %v, want runtime undefined", err)
	}
	if strings.Contains(out.String(), "undefined function") {
		t.Fatalf("REPL reported a check error:\n%s", out.String())
	}
}
