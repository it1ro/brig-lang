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
// Тонкость: checkBlock("invalid") вызывает parser.Parse(ModeModule, raw)
// без обёртки. Чтобы проверить ветку «invalid-блок всё же парсится»,
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
		r := checkBlock(block{file: "t.md", line: 1, lang: c.lang, raw: c.raw})
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
	results, err := CheckFile(p)
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
