package highlight_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/highlight"
	"github.com/it1ro/brig-lang/internal/sema"
)

func classAt(src string, off, cursor int, env highlight.Env) (highlight.Class, bool) {
	for _, sp := range highlight.Classify(src, cursor, env).Spans {
		if sp.Start <= off && off < sp.End {
			return sp.Class, true
		}
	}
	return "", false
}

func classesOf(src string, cursor int, env highlight.Env) map[highlight.Class]bool {
	m := map[highlight.Class]bool{}
	for _, sp := range highlight.Classify(src, cursor, env).Spans {
		m[sp.Class] = true
	}
	return m
}

// TestHighlightClasses — каждый вид токена получает свой класс.
func TestHighlightClasses(t *testing.T) {
	env := highlight.REPLEnv()
	env.Bindings["x"] = true
	cases := []struct {
		src   string
		class highlight.Class
	}{
		{"fn", highlight.Keyword},
		{":ok", highlight.Atom},
		{`"hi"`, highlight.String},
		{`b"ab"`, highlight.Bytes},
		{`rx"a"`, highlight.Regex},
		{"42", highlight.Number},
		{"1.5", highlight.Number},
		{`dec"1"`, highlight.Number},
		{"# note", highlight.Comment},
		{"## doc", highlight.Doc},
		{"Vec", highlight.Module},
		{"Option", highlight.Type},
		{"+", highlight.Op},
		{",", highlight.Punct},
		{"x", highlight.Binding},
		{"map", highlight.Prelude},
		{"h", highlight.Helper},
		{"nope", highlight.Unknown},
	}
	seen := map[highlight.Class]bool{}
	for _, c := range cases {
		got, ok := classAt(c.src, 0, -1, env)
		if !ok || got != c.class {
			t.Errorf("%q: class %q, want %q", c.src, got, c.class)
		}
		seen[c.class] = true
	}
	for c := range classesOf(`"\(map)"`, -1, env) {
		seen[c] = true
	}
	for c := range classesOf("fn f() ->\n    x\n  y\n", -1, env) {
		seen[c] = true
	}
	for _, c := range highlight.Classes() {
		if !seen[c] {
			t.Errorf("class %s not produced", c)
		}
	}
}

// TestHighlightIncomplete — незакрытая строка и открытый блок не паникуют.
func TestHighlightIncomplete(t *testing.T) {
	env := highlight.REPLEnv()
	pal := highlight.PaletteFromEnv(func(string) (string, bool) { return "", false })
	cases := []string{
		`"unterminated`,
		`"a \(b"`,
		`"a \(b`,
		"fn f() ->",
		"fn f() ->\n    ",
		"(",
		"%[",
		`b"`,
		`rx"`,
		`dec"`,
		"#",
		"##",
		"\tx",
		"x = 1\n    +",
		"",
	}
	for _, src := range cases {
		res := highlight.Classify(src, 0, env)
		out := highlight.Highlight(src, 0, env, pal)
		if strings.Count(out, "\n") != strings.Count(src, "\n") {
			t.Errorf("%q: highlight changed the number of lines\n%s", src, out)
		}
		if src == `"unterminated` {
			if c, ok := classAt(src, 1, -1, env); !ok || c != highlight.String {
				t.Errorf("unclosed string class %q", c)
			}
		}
		_ = res
	}
}

// TestHighlightInterp — код внутри \(…) красится как код, не как строка.
func TestHighlightInterp(t *testing.T) {
	src := `"pre\(map)post"`
	env := highlight.REPLEnv()
	off := strings.Index(src, "map")
	if c, ok := classAt(src, off, -1, env); !ok || c != highlight.Prelude {
		t.Errorf("map inside interp: %q, want prelude", c)
	}
	if c, _ := classAt(src, strings.Index(src, `\(`), -1, env); c != highlight.Interp {
		t.Errorf(`\( class %q, want interp`, c)
	}
	if c, _ := classAt(src, 1, -1, env); c != highlight.String {
		t.Errorf("string body class %q", c)
	}
	bad := `"\(nope)"`
	if c, _ := classAt(bad, strings.Index(bad, "nope"), -1, env); c != highlight.Unknown {
		t.Errorf("unknown inside interp: %q", c)
	}
}

// TestHighlightUnknownName — чужое имя и M.f без модуля или функции.
func TestHighlightUnknownName(t *testing.T) {
	env := highlight.REPLEnv()
	env.Bindings["x"] = true
	env.Modules["M"] = map[string]bool{"f": true}

	one := func(src string, off int, want highlight.Class) {
		t.Helper()
		if c, ok := classAt(src, off, -1, env); !ok || c != want {
			t.Errorf("%q at %d: %q, want %q", src, off, c, want)
		}
	}
	one("x", 0, highlight.Binding)
	one("map", 0, highlight.Prelude)
	one("h", 0, highlight.Helper)
	one("nope", 0, highlight.Unknown)
	one("M.f", 0, highlight.Module)
	one("M.f", 2, highlight.Binding)
	one("M.g", 0, highlight.Module)
	one("M.g", 2, highlight.Unknown)
	one("Z.f", 0, highlight.Unknown)
	one("Z.f", 2, highlight.Unknown)
	one("Vec.len", strings.Index("Vec.len", "len"), highlight.Prelude)
	one("Vec.nope", strings.Index("Vec.nope", "nope"), highlight.Unknown)

	same := "n = n"
	one(same, 0, highlight.Binding)
	one(same, strings.LastIndex(same, "n"), highlight.Unknown)
	later := "n = 1\nn"
	one(later, strings.LastIndex(later, "n"), highlight.Binding)

	with := "with\n    Ok(a) <- f(a)\n    Ok(a)\n"
	one(with, strings.Index(with, "a"), highlight.Binding)
	one(with, strings.Index(with, "f(a)")+2, highlight.Unknown)
	one(with, strings.LastIndex(with, "a"), highlight.Binding)

	body := "fn fact(n) ->\n    n * fact(n - 1)\n"
	for _, name := range []string{"fact", "n"} {
		if c, _ := classAt(body, strings.LastIndex(body, name), -1, env); c == highlight.Unknown {
			t.Errorf("%s in fn body is unknown", name)
		}
	}
}

// TestBracketMatch — пара скобки под курсором выделена, лишняя
// закрывающая — error.
func TestBracketMatch(t *testing.T) {
	env := highlight.Env{}
	res := highlight.Classify("(a)", 0, env)
	var open, end highlight.Span
	for _, sp := range res.Spans {
		if sp.Start == 0 {
			open = sp
		}
		if sp.End == len("(a)") {
			end = sp
		}
	}
	if open.Class != highlight.Punct || !open.Match || end.Class != highlight.Punct || !end.Match {
		t.Errorf("pair: open %+v close %+v", open, end)
	}

	res = highlight.Classify("a)", 1, env)
	if c, _ := classAt("a)", 1, 1, env); c != highlight.Error {
		t.Errorf("unmatched class %q", c)
	}
	for _, sp := range res.Spans {
		if sp.Match {
			t.Errorf("unmatched span has match: %+v", sp)
		}
	}

	res = highlight.Classify("()", -1, env)
	for _, sp := range res.Spans {
		if sp.Class == highlight.Error || sp.Match {
			t.Errorf("matched pair without cursor: %+v", sp)
		}
	}
}

// TestIndentGuides — направляющие открытых уровней и чужой отступ.
func TestIndentGuides(t *testing.T) {
	src := "fn f() ->\n    x\n"
	res := highlight.Classify(src, -1, highlight.REPLEnv())
	off := strings.Index(src, "    x")
	found := false
	for _, g := range res.Guides {
		if g == off {
			found = true
		}
	}
	if !found {
		t.Errorf("guides %v, want column of indented line %d", res.Guides, off)
	}
	pal := highlight.PaletteFromEnv(func(string) (string, bool) { return "", false })
	if out := highlight.Highlight(src, -1, highlight.REPLEnv(), pal); !strings.Contains(out, "│") {
		t.Errorf("paint has no guide:\n%s", out)
	}

	bad := "fn f() ->\n    x\n  y\n"
	boff := strings.Index(bad, "  y")
	if c, ok := classAt(bad, boff, -1, highlight.REPLEnv()); !ok || c != highlight.Error {
		t.Errorf("bad indent class %q", c)
	}
}

// TestBrigColorsEnv — BRIG_COLORS переопределяет SGR; NO_COLOR и TERM=dumb
// убирают escape-коды.
func TestBrigColorsEnv(t *testing.T) {
	src := ":a \"s\""
	env := highlight.REPLEnv()
	pal := highlight.PaletteFromEnv(func(k string) (string, bool) {
		if k == "BRIG_COLORS" {
			return "atom=35:string=32", true
		}
		return "", false
	})
	out := highlight.Highlight(src, -1, env, pal)
	if !strings.Contains(out, "\x1b[35m") || !strings.Contains(out, "\x1b[32m") {
		t.Errorf("BRIG_COLORS paint = %q", out)
	}

	for _, key := range []string{"NO_COLOR", "TERM"} {
		k := key
		pal = highlight.PaletteFromEnv(func(name string) (string, bool) {
			if name == "BRIG_COLORS" {
				return "atom=35:string=32", true
			}
			if name == k {
				if k == "TERM" {
					return "dumb", true
				}
				return "1", true
			}
			return "", false
		})
		out = highlight.Highlight(src, -1, env, pal)
		if strings.Contains(out, "\x1b") {
			t.Errorf("%s paint has escape: %q", k, out)
		}
	}
}

// TestHighlightCorpus — классификатор проходит corpus/ и examples/ без паники.
func TestHighlightCorpus(t *testing.T) {
	roots := []string{"../../corpus", "../../examples"}
	n := 0
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".brig") {
				return nil
			}
			n++
			b, err := os.ReadFile(path)
			if err != nil {
				t.Errorf("read %s: %v", path, err)
				return nil
			}
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("%s: panic %v", path, r)
					}
				}()
				highlight.Highlight(string(b), 0, highlight.REPLEnv(), highlight.PaletteFromEnv(nil))
				highlight.Classify(string(b), -1, highlight.Env{})
			}()
			return nil
		})
		if err != nil {
			t.Errorf("walk %s: %v", root, err)
		}
	}
	if n == 0 {
		t.Fatal("no .brig files")
	}
}

// TestBuiltinNamesMatchSema — прелюдия и функции встроенных модулей
// подсветки совпадают с sema; акторных примитивов в Prelude.* нет.
func TestBuiltinNamesMatchSema(t *testing.T) {
	env := highlight.REPLEnv()
	for _, n := range sema.PreludeNames() {
		if c, _ := classAt(n, 0, -1, env); c != highlight.Prelude {
			t.Errorf("%s: %q, want prelude", n, c)
		}
	}
	for mod, fns := range sema.BuiltinModules() {
		for _, f := range fns {
			src := mod + "." + f
			if c, _ := classAt(src, len(mod)+1, -1, env); c != highlight.Prelude {
				t.Errorf("%s: %q, want prelude", src, c)
			}
		}
	}
	for _, src := range []string{"Prelude.send", "Prelude.self"} {
		if c, _ := classAt(src, len("Prelude."), -1, env); c != highlight.Unknown {
			t.Errorf("%s: %q, want unknown", src, c)
		}
	}
}

func noEnv(string) (string, bool) { return "", false }

// TestDefaultPalette — сдержанная палитра: операторы, пунктуация и
// привязки цвета терминала, красный только у error.
func TestDefaultPalette(t *testing.T) {
	want := map[highlight.Class]string{
		highlight.Keyword: "35",
		highlight.Atom:    "36",
		highlight.String:  "32",
		highlight.Interp:  "33",
		highlight.Bytes:   "32",
		highlight.Regex:   "33",
		highlight.Number:  "33",
		highlight.Comment: "90",
		highlight.Doc:     "3;90",
		highlight.Module:  "34",
		highlight.Type:    "34",
		highlight.Op:      "",
		highlight.Punct:   "",
		highlight.Binding: "",
		highlight.Prelude: "36",
		highlight.Helper:  "1;36",
		highlight.Unknown: "4",
		highlight.Error:   "1;31",
	}
	pal := highlight.PaletteFromEnv(noEnv)
	for _, c := range highlight.Classes() {
		code, ok := want[c]
		if !ok {
			t.Errorf("class %s has no expected code", c)
			continue
		}
		got := pal.Paint("x", highlight.Result{Spans: []highlight.Span{{Start: 0, End: 1, Class: c}}})
		exp := "x"
		if code != "" {
			exp = "\x1b[" + code + "mx\x1b[0m"
		}
		if got != exp {
			t.Errorf("%s: %q, want %q", c, got, exp)
		}
		if c != highlight.Error {
			for _, p := range strings.Split(code, ";") {
				if p == "31" || p == "91" {
					t.Errorf("%s is red", c)
				}
			}
		}
	}
}

// TestBracketMatchStyle — пара скобок жирная и подчёркнутая в цвете
// своего класса, без инверсии.
func TestBracketMatchStyle(t *testing.T) {
	pal := highlight.PaletteFromEnv(noEnv)
	out := highlight.Highlight("(a)", 0, highlight.Env{Bindings: map[string]bool{"a": true}}, pal)
	if strings.Contains(out, ";7m") || strings.Contains(out, "[7m") {
		t.Errorf("match uses reverse video: %q", out)
	}
	if strings.Count(out, "\x1b[1;4m") != 2 {
		t.Errorf("match paint = %q, want two bold underlined brackets", out)
	}
	pal = highlight.PaletteFromEnv(func(k string) (string, bool) {
		if k == "BRIG_COLORS" {
			return "punct=34", true
		}
		return "", false
	})
	out = highlight.Highlight("(a)", 0, highlight.Env{Bindings: map[string]bool{"a": true}}, pal)
	if strings.Count(out, "\x1b[34;1;4m") != 2 {
		t.Errorf("colored match paint = %q", out)
	}
}

// TestOpenBracketNotError — незакрытая открывающая скобка — обычный
// неполный ввод; error — лишняя закрывающая и чужой вид.
func TestOpenBracketNotError(t *testing.T) {
	env := highlight.REPLEnv()
	for _, src := range []string{"(", "print(", "%[1, ", "%{", "f(%[1, (2"} {
		for _, sp := range highlight.Classify(src, len(src), env).Spans {
			if sp.Class == highlight.Error {
				t.Errorf("%q: error span %+v", src, sp)
			}
		}
	}
	cases := []struct {
		src string
		off int
	}{
		{"(]", 1},
		{")", 0},
		{"f(a))", 4},
		{"%[1}", 3},
	}
	for _, c := range cases {
		if got, _ := classAt(c.src, c.off, -1, env); got != highlight.Error {
			t.Errorf("%q at %d: %q, want error", c.src, c.off, got)
		}
	}
	if got, _ := classAt("(]", 0, -1, env); got == highlight.Error {
		t.Errorf("open bracket before wrong close is error")
	}
}
