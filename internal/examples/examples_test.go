package examples

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/vm"
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

func TestRunRepl(t *testing.T) {
	src := strings.Join([]string{
		"> x = 1",
		"> x + 1",
		"2",
		"> y = x * 2",
		"",
	}, "\n")
	if err := runRepl(src, vm.New(), vm.New(), nil); err != nil {
		t.Fatalf("runRepl: %v", err)
	}

	bad := "> x %"
	if err := runRepl(bad, vm.New(), vm.New(), nil); err == nil {
		t.Fatal("runRepl: want error for '> x %'")
	}
}

// replSnapshot — блок §11.4: снимок замыкания.
var replSnapshot = []string{
	"> x = 5",
	"> f = () -> x",
	"> x = 10        # shadowing: новая область",
	"> f()",
	"5              # лексический снимок: f видит старый x",
	"> x + 1",
	"11",
}

// TestReplBlockExecutes: строки `> expr` исполняются в одной REPL-сессии,
// ответ сравнивается через `==`; связывания без ответа не сравниваются (T-117).
func TestReplBlockExecutes(t *testing.T) {
	cases := map[string][]string{
		"§11.4":              replSnapshot,
		"ответ — выражение":  {"> xs = [1, 2, 3]", "> Enum.map(xs, x -> x * 2)", "[2, 4, 6]", "> len(xs)", "1 + 2"},
		"Int == Float":       {"> 2 * 3", "6.0"},
		"строка":             {`> to_str(12)`, `"12"`},
		"без ответа":         {"> y = 1", "> y + 1"},
		"ответ связыванию":   {"> z = 7", "7"},
		"пустые строки":      {"> 1", "", "1", ""},
		"эвристика без меты": {"> 1 + 2"},
	}
	for name, lines := range cases {
		lang := "brig repl"
		if name == "эвристика без меты" {
			lang = "brig"
		}
		b := block{file: "t.md", line: 1, lang: lang, raw: strings.Join(lines, "\n")}
		if r := checkBlock(b, nil); !r.OK || r.Mode != "repl" {
			t.Errorf("%s: want OK repl, got %s (mode %s)", name, r, r.Mode)
		}
	}
}

// TestReplBlockMismatch: испорченная копия §11.4 — FAIL с ожидаемым и
// фактическим значением на строке ответа (T-117).
func TestReplBlockMismatch(t *testing.T) {
	lines := append([]string(nil), replSnapshot...)
	lines[4] = "10             # испорчено: снимок не соблюдён"
	b := block{file: "t.md", line: 100, lang: "brig repl", raw: strings.Join(lines, "\n")}
	r := checkBlock(b, nil)
	if r.OK {
		t.Fatalf("want FAIL, got %s", r)
	}
	for _, want := range []string{"f()", "want 10", "got 5"} {
		if !strings.Contains(r.ErrMsg, want) {
			t.Errorf("msg %q does not contain %q", r.ErrMsg, want)
		}
	}
	// Fence на 100-й строке, ответ — 5-я строка тела → 105-я строка markdown.
	if r.Line != 105 {
		t.Errorf("line: got %d, want 105 (%s)", r.Line, r)
	}

	bad := map[string][]string{
		"две строки ответа":        {"> 1", "1", "1"},
		"ответ без ввода":          {"1", "> 1"},
		"ответ не выражение":       {"> 1", "x = 1"},
		"ошибка ввода":             {"> x %"},
		"sema-ошибка ввода":        {"> 1 |> spawn"},
		"ответ несравним":          {"> 1", `"1"`},
		"неизвестное имя в ответе": {"> x = 1", "> x", "x"},
	}
	for name, lines := range bad {
		b := block{file: "t.md", line: 1, lang: "brig repl", raw: strings.Join(lines, "\n")}
		if r := checkBlock(b, nil); r.OK {
			t.Errorf("%s: want FAIL, got %s", name, r)
		}
	}
}

// TestReplBlockRaise: строка `raise <term>` сопоставляется с непойманным
// raise; неожиданный raise — FAIL (T-117).
func TestReplBlockRaise(t *testing.T) {
	cases := []struct {
		name    string
		lines   []string
		wantOK  bool
		wantMsg string
	}{
		{"raise совпал", []string{"> raise((:not_found, 42))", "raise (:not_found, 42)"}, true, ""},
		{"type_error", []string{`> 1 + "a"`, `raise (:type_error, (:add, (1, "a")))`}, true, ""},
		{"сессия жива после raise", []string{"> x = 1", "> raise(:boom)", "raise :boom", "> x", "1"}, true, ""},
		{"другой терм", []string{"> raise(:boom)", "raise :bang"}, false, "want raise :bang, got raise :boom"},
		{"raise не случился", []string{"> 1", "raise :boom"}, false, "want raise :boom, got 1"},
		{"неожиданный raise", []string{"> raise(:boom)", "1"}, false, "want 1, got raise :boom"},
		{"неожиданный raise без ответа", []string{"> raise(:boom)"}, false, "raise: :boom"},
	}
	for _, c := range cases {
		b := block{file: "t.md", line: 1, lang: "brig repl", raw: strings.Join(c.lines, "\n")}
		r := checkBlock(b, nil)
		if r.OK != c.wantOK {
			t.Errorf("%s: OK=%v (want %v): %s", c.name, r.OK, c.wantOK, r)
			continue
		}
		if !strings.Contains(r.ErrMsg, c.wantMsg) {
			t.Errorf("%s: msg %q does not contain %q", c.name, r.ErrMsg, c.wantMsg)
		}
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
		"x = 1",
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

	// Парсится, но не компилируется: запись необъявленного типа.
	b := block{file: "t.md", line: 10, lang: "brig stmt", raw: "t = (1, 2, 3)\nFoo{a: t}"}
	r := checkBlock(b, tasks)
	if r.OK {
		t.Fatalf("unknown record type: want FAIL, got %s", r)
	}
	// Fence на строке 10, стейтмент — на 12-й строке markdown, колонка 1.
	if r.Line != 12 || r.Col != 1 {
		t.Errorf("position: got %d:%d, want 12:1 (%s)", r.Line, r.Col, r)
	}

	// module без обёртки: строка 2 блока — 12-я строка markdown, колонка та же.
	b = block{file: "t.md", line: 10, lang: "brig module", raw: "fn main() ->\n    Foo{a: 1}"}
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

// TestCheckBlockUnknownFunctionFails: обычный блок проходит разрешение
// имён (§F.3, T-179), как `brig check`: неизвестная функция — FAIL.
func TestCheckBlockUnknownFunctionFails(t *testing.T) {
	b := block{file: "t.md", line: 1, lang: "brig stmt", raw: "x = nope(1)\nprint(x)"}
	r := checkBlock(b, map[string]bool{})
	if r.OK || !strings.Contains(r.ErrMsg, "undefined function nope/1") {
		t.Fatalf("want FAIL with undefined function nope/1, got %s", r)
	}
	if r.Line != 2 || r.Col != 5 {
		t.Errorf("position: got %d:%d, want 2:5 (%s)", r.Line, r.Col, r)
	}
}

// TestCheckBlockPendingNeedsTask: `brig pending(T-NNN)` обязан не
// компилироваться и ссылаться на существующую задачу (T-116). Неизвестная
// функция — тоже «не компилируется», как в `brig check` (§F.3, T-160).
// Парсер и round-trip pending-блок проходит как обычный: ждать задачу
// может только sema или компилятор (T-151).
func TestCheckBlockPendingNeedsTask(t *testing.T) {
	tasks := map[string]bool{"T-500": true}
	notCompiling := "t = (1, 2, 3)\nFoo{a: t}"

	cases := []struct {
		name, lang, raw string
		wantOK          bool
		wantMsg         string
	}{
		{"pending, не компилируется", "brig pending(T-500)", notCompiling, true, ""},
		{"pending с режимом", "brig stmt pending(T-500)", notCompiling, true, ""},
		{"pending, неизвестная функция", "brig pending(T-500)", "no_such_fn(1)", true, ""},
		{"pending компилируется", "brig pending(T-500)", "x = 1", false, "снять pending"},
		{"pending неизвестной задачи", "brig pending(T-999)", notCompiling, false, "T-999"},
		{"pending без номера", "brig pending", notCompiling, false, "pending"},
		{"pending(T-NNN) в invalid", "brig invalid pending(T-500)", "x %", false, "pending"},
		{"pending, ошибка парсера", "brig pending(T-500)", "x %", false, "pending(T-500)"},
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
		{"ошибка компилятора", `brig invalid "неизвестный тип записи"`, "fn main() ->\n    Foo{a: 1}", true},
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

// T-246: pending(T-NNN) на закрытую задачу — проблема.
func TestPendingClosedTask(t *testing.T) {
	doc := "# Doc\n\n" +
		"```brig module pending(T-7)\nmodule Main\nfn main() -> not_yet_defined(1)\n```\n\n" +
		"```brig module pending(T-8)\nmodule Main\nfn main() -> also_missing(1)\n```\n"
	path := filepath.Join(t.TempDir(), "doc.md")
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	tasks := map[string]bool{"T-7": true, "T-8": true}

	problems, err := ClosedPending(path, tasks, map[string]bool{"T-7": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 || !strings.Contains(problems[0], "doc.md:3") || !strings.Contains(problems[0], "pending(T-7)") {
		t.Fatalf("problems = %q, want одна про pending(T-7) на строке 3", problems)
	}
	if problems, _ = ClosedPending(path, tasks, nil); len(problems) != 0 {
		t.Fatalf("без закрытых задач проблем быть не должно: %q", problems)
	}

	closed, err := ParseClosed("T-7, T-9")
	if err != nil || !closed["T-7"] || !closed["T-9"] || len(closed) != 2 {
		t.Fatalf("ParseClosed = %v, %v", closed, err)
	}
	if _, err := ParseClosed("246"); err == nil {
		t.Fatal("ParseClosed(246): want error")
	}
}
