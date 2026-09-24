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
	// Блоки, попавшие внутрь внешнего fence, не извлекаются.
	if got := blocks[1].raw; got != "module Main" {
		t.Errorf("block 1 raw: %q", got)
	}
	if got := blocks[2].raw; got != "y = 2" {
		t.Errorf("block 2 raw: %q", got)
	}
}

func TestExtractBlocksUnclosedFence(t *testing.T) {
	// Незакрытый fence (например, конец файла) не должен приводить к панике.
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
		{"module explicit", "module"}, // метка с пояснением после пробела
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
		{"import Option\nx = 1", "module"},             // import → module
		{"module Main\nfn main() ->\n    x", "module"}, // fn main → module
		{"> 1 + 2", "repl"},                            // '>' → repl
		{"1 + 2", "expr"},                              // одна строка без '=' → expr
		{"x = 1\ny = 2", "stmt"},                       // иначе → stmt
	}
	for _, c := range cases {
		if got := heuristicMode(c.raw); got != c.want {
			t.Errorf("heuristicMode(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
}

func TestHasTopLevelBind(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{"import Option\nx = opt |> f", true}, // top-level bind
		{"fn main() ->\n    x = 1", false},    // bind внутри fn (индент)
		{"module Main\nfn main() ->\n    1 + 2", false},
		{"x = 1", true},
	}
	for _, c := range cases {
		if got := hasTopLevelBind(c.raw); got != c.want {
			t.Errorf("hasTopLevelBind(%q) = %v, want %v", c.raw, got, c.want)
		}
	}
}

func TestCheckBlockTextRules(t *testing.T) {
	cases := []struct {
		name   string
		raw    string
		lang   string
		wantOK bool
	}{
		{"ok stmt", "x = 1\ny = x + 1", "brig", true},
		{"B4 Result[]", "fn f() -> Result[Int]", "brig", false},
		{"B2 fn ->", "f = fn -> 1", "brig", false},
		{"M-004 f(..)", "f(..)", "brig", false},
		{"2.9 Ok equiv", "x = Ok(1) ≡ (:ok, 1)", "brig", false},
		{"ambiguous module+let", "import Option\nx = f()", "brig", false},
		{"module ok", "module Main\nfn main() ->\n    1 + 2", "brig", true},
	}
	for _, c := range cases {
		r := checkBlock(block{file: "t.md", line: 1, lang: c.lang, raw: c.raw})
		if r.OK != c.wantOK {
			t.Errorf("%s: OK=%v (want %v), msg=%q", c.name, r.OK, c.wantOK, r.ErrMsg)
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
}

func TestParseRepl(t *testing.T) {
	// Приглашения и вывод отбрасываются; строки-код парсятся по отдельности.
	src := strings.Join([]string{
		"> x = 1",
		"> x + 1",
		"2", // вывод REPL (без операторов) — отбрасывается
		"> y = x * 2",
		"",
	}, "\n")
	if err := parseRepl(src); err != nil {
		t.Fatalf("parseRepl: %v", err)
	}

	// Ошибка лексера: одиночный '%' — КР-005.
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
