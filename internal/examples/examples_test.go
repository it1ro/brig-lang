package examples

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractBlocksCommonMark(t *testing.T) {
	// CommonMark: закрывающий fence НЕ короче открывающего; внешний
	// 4-бэктиковый fence надо пропускать как не-brig.
	src := strings.Join([]string{
		"",
		"```markdown",
		"# foo",
		"```",
		"",
		"````", // внешний 4-бэктиковый fence
		"тут ```brig — литерал, не блок",
		"x = 1",
		"````",
		"",
		"```brig",
		"x = 1",
		"```",
		"",
		"```brig module",
		"module Main",
		"```",
		"",
		"```brig",
		"y = 2",
		"````", // закрывающий длиннее открывающего — валидно
	}, "\n")

	blocks := extractBlocks("t.md", src)
	if len(blocks) != 3 {
		t.Fatalf("want 3 blocks, got %d: %+v", len(blocks), blocks)
	}
	wantLangs := []string{"brig", "brig module", "brig"}
	for i, b := range blocks {
		if b.lang != wantLangs[i] {
			t.Errorf("block %d: lang=%q, want %q", i, b.lang, wantLangs[i])
		}
	}
	if got := blocks[1].raw; got != "module Main" {
		t.Errorf("block 1 raw: %q", got)
	}
	if got := blocks[2].raw; got != "y = 2" {
		t.Errorf("block 2 raw: %q", got)
	}
}

func TestExtractBlocksUnclosedFence(t *testing.T) {
	src := "```brig\nx = 1\n"
	blocks := extractBlocks("t.md", src)
	if len(blocks) != 0 {
		t.Fatalf("unclosed fence should yield no blocks, got %+v", blocks)
	}
}

func TestModeByMeta(t *testing.T) {
	cases := []struct{ meta, want string }{
		{"", ""},
		{"module", "module"},
		{"repl", "repl"},
		{"expr", "expr"},
		{"stmt", "stmt"},
		{"invalid", "invalid"},
		{"module explicit", "module"},
		{"invalid something", "invalid"},
		{"foo", ""},
	}
	for _, c := range cases {
		if got := modeByMeta(c.meta); got != c.want {
			t.Errorf("modeByMeta(%q) = %q, want %q", c.meta, got, c.want)
		}
	}
}

func TestHeuristicMode(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"> 1 + 2", "repl"},
		{"> x = 1\n> x + 1", "repl"},
		{"> x = 1\n2", "stmt"},
		{"1 + 2", "stmt"},
		{"x = 1\ny = 2", "stmt"},
		{"module Main\nfn main() ->\n    1 + 2", "stmt"},
		{"", "stmt"},
	}
	for _, c := range cases {
		if got := heuristicMode(c.raw); got != c.want {
			t.Errorf("heuristicMode(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
}

// TestCheckBlockModes проверяет поведение checkBlock по режимам.
//
// Тонкость: checkBlock("invalid") компилирует raw в режиме module без
// обёртки. Чтобы проверить ветку «invalid-блок всё же компилируется»,
// raw должен быть валидным module-сниппетом. И наоборот, чтобы проверить
// ветку «invalid-блок падает по синтаксису», raw должен содержать
// module-скелет, в котором ошибка возникает в теле.
func TestCheckBlockModes(t *testing.T) {
	cases := []struct {
		name   string
		raw    string
		lang   string
		wantOK bool
	}{
		{"ok stmt (без метки)", "x = 1\ny = x + 1", "brig", true},
		{"ok stmt явно", "x = 1\ny = x + 1", "brig stmt", true},
		{"module ok", "module Main\nfn main() ->\n    1 + 2", "brig module", true},
		{"module rejects top-level let", "module M\nx = 1", "brig module", false},
		{"expr ok", "1 to 10 |> list", "brig expr", true},
		{"expr fails on multi-stmt", "x = 1\ny = 2", "brig expr", false},

		{"invalid parses — FAIL", "fn main() ->\n    1", "brig invalid", false},
		{"invalid fails — OK", "fn main() ->\n    x = [1, ..]", "brig invalid", true},
		{"invalid lex error — OK", "x %", "brig invalid", true},
	}
	for _, c := range cases {
		r := checkBlock(block{file: "t.md", line: 1, lang: c.lang, raw: c.raw}, nil)
		if r.OK != c.wantOK {
			t.Errorf("%s: OK=%v (want %v), msg=%q",
				c.name, r.OK, c.wantOK, r.ErrMsg)
		}
	}
}

func TestWrapForMode(t *testing.T) {
	got := wrapForMode("expr", "1 to 10 |> list")
	want := "fn main() ->\n    1 to 10 |> list\n"
	if got != want {
		t.Errorf("wrap expr:\n%q\nwant:\n%q", got, want)
	}

	got = wrapForMode("stmt", "x = 1\ny = x + 1")
	want = "fn main() ->\n    x = 1\n    y = x + 1\n"
	if got != want {
		t.Errorf("wrap stmt:\n%q\nwant:\n%q", got, want)
	}

	for _, m := range []string{"module", "repl", "invalid"} {
		if got := wrapForMode(m, "x = 1\n"); got != "x = 1\n" {
			t.Errorf("wrap %s: got %q, want unchanged", m, got)
		}
	}
}

func TestParseRepl(t *testing.T) {
	src := strings.Join([]string{
		"> x = 1",
		"> x + 1",
		"2",
		"> y = x * 2",
		"",
	}, "\n")
	if err := parseRepl(src); err != nil {
		t.Fatalf("parseRepl: %v", err)
	}

	bad := "> x %"
	if err := parseRepl(bad); err == nil {
		t.Fatal("parseRepl: want error for '> x %'")
	}
}

func TestCheckFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "doc.md")
	src := strings.Join([]string{
		"# Заголовок",
		"",
		"```brig",
		"x = 1",
		"```",
		"",
		"```brig stmt",
		"y = x + 1",
		"```",
	}, "\n")
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	results, err := CheckFile(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("want 2 results, got %d", len(results))
	}
	for _, r := range results {
		if !r.OK {
			t.Errorf("unexpected FAIL: %s", r.String())
		}
	}
}

// TestCheckBlockCompiles: блоки module/stmt/expr проходят sema и компилятор;
// ошибка компиляции — FAIL с позицией в markdown (T-116).
func TestCheckBlockCompiles(t *testing.T) {
	tasks := map[string]bool{}
	ok := []block{
		{file: "t.md", line: 10, lang: "brig stmt", raw: "x = 1\ny = x + 1"},
		{file: "t.md", line: 10, lang: "brig expr", raw: "1 to 10 |> list"},
		{file: "t.md", line: 10, lang: "brig module", raw: "fn main() ->\n    1"},
	}
	for _, b := range ok {
		if r := checkBlock(b, tasks); !r.OK {
			t.Errorf("%q: want OK, got %s", b.raw, r)
		}
	}

	// Парсится, но не компилируется: деструктурирующее связывание.
	b := block{file: "t.md", line: 10, lang: "brig stmt", raw: "t = (1, 2, 3)\n(a, b, c) = t"}
	r := checkBlock(b, tasks)
	if r.OK {
		t.Fatalf("destructuring bind: want FAIL, got %s", r)
	}
	// Fence на строке 10, стейтмент — на 12-й строке markdown, колонка 1.
	if r.Line != 12 || r.Col != 1 {
		t.Errorf("position: got %d:%d, want 12:1 (%s)", r.Line, r.Col, r)
	}

	// module без обёртки: строка 2 блока — 12-я строка markdown, колонка та же.
	b = block{file: "t.md", line: 10, lang: "brig module", raw: "fn main() ->\n    (a, b) = (1, 2)"}
	if r := checkBlock(b, tasks); r.OK || r.Line != 12 || r.Col != 5 {
		t.Errorf("module position: want FAIL at 12:5, got %s", r)
	}

	// Ошибка sema: акторный примитив справа от pipe (§F.3).
	b = block{file: "t.md", line: 1, lang: "brig stmt", raw: "x = 1\nx |> spawn"}
	r = checkBlock(b, tasks)
	if r.OK || !strings.Contains(r.ErrMsg, "pipe RHS") {
		t.Errorf("sema error: want FAIL on pipe RHS, got %s", r)
	}
}

// TestCheckBlockPendingNeedsTask: `brig pending(T-NNN)` обязан не
// компилироваться и ссылаться на существующую задачу (T-116).
func TestCheckBlockPendingNeedsTask(t *testing.T) {
	tasks := map[string]bool{"T-500": true}
	notCompiling := "t = (1, 2, 3)\n(a, b, c) = t"

	cases := []struct {
		name, lang, raw string
		wantOK          bool
		wantMsg         string
	}{
		{"pending, не компилируется", "brig pending(T-500)", notCompiling, true, ""},
		{"pending с режимом", "brig stmt pending(T-500)", notCompiling, true, ""},
		{"pending компилируется", "brig pending(T-500)", "x = 1", false, "снять pending"},
		{"pending неизвестной задачи", "brig pending(T-999)", notCompiling, false, "T-999"},
		{"pending без номера", "brig pending", notCompiling, false, "pending"},
		{"pending(T-NNN) в invalid", "brig invalid pending(T-500)", "x %", false, "pending"},
	}
	for _, c := range cases {
		r := checkBlock(block{file: "t.md", line: 1, lang: c.lang, raw: c.raw}, tasks)
		if r.OK != c.wantOK {
			t.Errorf("%s: OK=%v (want %v): %s", c.name, r.OK, c.wantOK, r)
			continue
		}
		if c.wantOK && r.Pending != "T-500" {
			t.Errorf("%s: Pending=%q, want T-500", c.name, r.Pending)
		}
		if !strings.Contains(r.ErrMsg, c.wantMsg) {
			t.Errorf("%s: msg %q does not contain %q", c.name, r.ErrMsg, c.wantMsg)
		}
	}
}

// TestCheckInvalidReason: `brig invalid "подстрока"` — блок обязан падать
// именно с этой ошибкой (S-2, T-116).
func TestCheckInvalidReason(t *testing.T) {
	tasks := map[string]bool{}
	// S-2: блок §3.2 падает из-за top-level связывания, а не из-за `if`.
	s2 := `x = "value: \(if ready then 1 else 0)"`
	cases := []struct {
		name, lang, raw string
		wantOK          bool
	}{
		{"S-2: другая причина", `brig invalid "if"`, s2, false},
		{"S-2: верная причина", `brig invalid "module top-level"`, s2, true},
		{"без причины", "brig invalid", s2, true},
		{"причина парсера", `brig invalid "expected expression"`, "fn main() ->\n    x = [1, ..]", true},
		{"блок компилируется", `brig invalid "whatever"`, "fn main() ->\n    1", false},
		{"ошибка компилятора", `brig invalid "простые связывания"`, "fn main() ->\n    (a, b) = (1, 2)", true},
	}
	for _, c := range cases {
		r := checkBlock(block{file: "t.md", line: 1, lang: c.lang, raw: c.raw}, tasks)
		if r.OK != c.wantOK {
			t.Errorf("%s: OK=%v (want %v): %s", c.name, r.OK, c.wantOK, r)
		}
	}
}

func TestLoadTasks(t *testing.T) {
	dir := t.TempDir()
	src := "| 7 | T-116 [#177](x) | ... | T-113 |\n### T-175 · Range\n"
	if err := os.WriteFile(filepath.Join(dir, "wave-7.md"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	tasks, err := LoadTasks(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"T-116", "T-113", "T-175"} {
		if !tasks[id] {
			t.Errorf("%s not found in %v", id, tasks)
		}
	}
	if tasks["T-1"] {
		t.Errorf("T-1 must not match a prefix of T-116")
	}
}
