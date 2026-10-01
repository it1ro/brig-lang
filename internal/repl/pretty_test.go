package repl_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/highlight"
	"github.com/it1ro/brig-lang/internal/repl"
	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/vm"
)

func colorOn() highlight.Palette {
	return highlight.PaletteFromEnv(func(string) (string, bool) { return "", false })
}

func colorOff() highlight.Palette {
	return highlight.PaletteFromEnv(func(k string) (string, bool) {
		if k == "NO_COLOR" {
			return "", true
		}
		return "", false
	})
}

func evalOne(t *testing.T, src string) runtime.Value {
	t.Helper()
	if !strings.HasSuffix(src, "\n") {
		src += "\n"
	}
	var buf bytes.Buffer
	s := repl.New(vm.New(), &buf)
	defer s.Close()
	res, err := s.Eval(src)
	if err != nil || len(res) == 0 {
		t.Fatalf("eval %q: %v diag %s", src, err, buf.String())
	}
	return res[len(res)-1].Value
}

// TestPrettyWidth — значение шире терминала печатается с отступом по
// вложенности; помещающееся — одной строкой, как Inspect.
func TestPrettyWidth(t *testing.T) {
	cases := []runtime.Value{
		runtime.List(runtime.Int(1), runtime.Int(2)),
		runtime.Vector(runtime.Int(1), runtime.Int(2)),
		runtime.Map([]runtime.MapEntry{{Key: runtime.Int(1), Val: runtime.Int(2)}}),
		runtime.Set(runtime.Int(1), runtime.Int(2)),
		runtime.Tuple(runtime.Int(1), runtime.Int(2)),
		runtime.Record("", []runtime.RecordField{{Name: "a", Val: runtime.Int(1)}}),
		runtime.Variant("Some", runtime.Int(1)),
	}
	for _, v := range cases {
		wide := repl.Format(v, repl.Print{Limits: repl.NoLimits()})
		if wide != v.Inspect() {
			t.Errorf("wide %s: got %s", v.Inspect(), wide)
		}
		narrow := repl.Format(v, repl.Print{Width: 1, Limits: repl.NoLimits()})
		if !strings.Contains(narrow, "\n") {
			t.Errorf("%s did not break:\n%s", v.Inspect(), narrow)
		}
		if strings.Contains(narrow, "\x1b") {
			t.Errorf("escape without palette:\n%s", narrow)
		}
	}

	nested := runtime.List(
		runtime.List(runtime.Int(1), runtime.Int(2)),
		runtime.List(runtime.Int(3)),
	)
	text := repl.Format(nested, repl.Print{Width: 8, Limits: repl.NoLimits()})
	lines := strings.Split(text, "\n")
	if len(lines) < 3 {
		t.Fatalf("nested:\n%s", text)
	}
	var indents []int
	for _, ln := range lines {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		indents = append(indents, len(ln)-len(strings.TrimLeft(ln, " ")))
	}
	deeper := false
	for i := 1; i < len(indents); i++ {
		if indents[i] > indents[0] {
			deeper = true
		}
	}
	if !deeper {
		t.Fatalf("no nested indent:\n%s", text)
	}

	short := runtime.List(runtime.Int(1), runtime.Atom("ok"))
	got, ok := repl.FormatAnswer("xs", short, repl.Print{Width: 40, Limits: repl.NoLimits()})
	if !ok || got != "xs = "+short.Inspect() {
		t.Fatalf("binding: %q ok=%v", got, ok)
	}
	if _, ok := repl.FormatAnswer("x", runtime.Unit, repl.Print{}); ok {
		t.Fatal("unit was printed")
	}

	env := highlight.REPLEnv()
	pal := colorOn()
	in := highlight.Highlight(":ok", -1, env, pal)
	out := repl.Format(runtime.Atom("ok"), repl.Print{Pal: pal, Env: env, Limits: repl.NoLimits()})
	if in != out {
		t.Fatalf("atom color input %q output %q", in, out)
	}
	plain := repl.Format(runtime.Atom("ok"), repl.Print{Pal: colorOff(), Limits: repl.NoLimits()})
	if plain != ":ok" || strings.Contains(plain, "\x1b") {
		t.Fatalf("NO_COLOR: %q", plain)
	}
}

// TestPrettyLimits — 50 элементов, глубина 8, строка 2 000 кодпоинтов;
// обрезка помечена `… N more`.
func TestPrettyLimits(t *testing.T) {
	xs := make([]runtime.Value, 1000)
	for i := range xs {
		xs[i] = runtime.Int(int64(i + 1))
	}
	got := repl.Format(runtime.List(xs...), repl.Print{Width: 0})
	if !strings.Contains(got, "… 950 more") {
		t.Fatalf("elems: %s", got)
	}
	if strings.Contains(got, "51") {
		t.Fatalf("element 51 leaked: %s", tail(got, 40))
	}

	deep := runtime.Int(1)
	for i := 0; i < 8; i++ {
		deep = runtime.List(deep)
	}
	got = repl.Format(deep, repl.Print{})
	if strings.Contains(got, "1") || !strings.Contains(got, "…") {
		t.Fatalf("depth 9: %s", got)
	}
	shown := runtime.Int(1)
	for i := 0; i < 7; i++ {
		shown = runtime.List(shown)
	}
	if !strings.Contains(repl.Format(shown, repl.Print{}), "1") {
		t.Fatal("depth 8 hid the int")
	}

	s := strings.Repeat("a", 2500)
	got = repl.Format(runtime.Str(s), repl.Print{})
	if strings.Count(got, "a") != 2000 || !strings.HasSuffix(got, "… 500 more") {
		t.Fatalf("string: suffix %q count %d", tail(got, 20), strings.Count(got, "a"))
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if strings.HasPrefix(s[i:], "\x1b[") {
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			if j < len(s) {
				j++
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// TestPrettyRoundTrip — вывод без цвета и лимитов разбирается обратно
// в равное значение, и узкой шириной тоже.
func TestPrettyRoundTrip(t *testing.T) {
	srcs := []string{
		"1",
		":ok",
		"true",
		"1.5",
		`[1, :ok, "a b"]`,
		`%{1 => 2, "a" => [3]}`,
		"(:ok, 42)",
		"%[1, 2, 3]",
		"set(1, 2, 1)",
		`{id: 1, name: "a"}`,
		"Some(1)",
		"None",
		"1 to 3",
		`b"hi"`,
		`dec"1.5"`,
		"[[1, 2], [3, 4]]",
	}
	for _, src := range srcs {
		v := evalOne(t, src)
		for _, w := range []int{0, 8} {
			text := repl.Format(v, repl.Print{Width: w, Limits: repl.NoLimits()})
			if strings.Contains(text, "\x1b") {
				t.Fatalf("color in %q", text)
			}
			if w == 0 && text != v.Inspect() {
				t.Fatalf("inspect %s: got %s", v.Inspect(), text)
			}
			got := evalOne(t, text)
			if !runtime.Equal(got, v) {
				t.Fatalf("width %d src %s\nprinted %s\ngot %s", w, src, text, got.Inspect())
			}
		}
	}
}

// TestReplErrorCaret — ошибка разбора: E.1, строка ввода и `^`.
// info о затенении прелюдии приглушён и не ошибка. raise печатает
// значение и кадры trace.
func TestReplErrorCaret(t *testing.T) {
	var errOut, out bytes.Buffer
	s := repl.New(vm.New(), &errOut)
	t.Cleanup(s.Close)
	fe := repl.Plain{Out: &out, Err: &errOut}

	if err := fe.Eval(s, "y = )\n"); err != nil {
		t.Fatal(err)
	}
	got := errOut.String()
	if !strings.Contains(got, "error: <repl>:1:5:") || !strings.Contains(got, "y = )\n") || !strings.Contains(got, "\n    ^\n") {
		t.Fatalf("parse caret:\n%s", got)
	}

	errOut.Reset()
	out.Reset()
	fe.Pal = colorOn()
	if err := fe.Eval(s, "len = 1\n"); err != nil {
		t.Fatal(err)
	}
	info := errOut.String()
	if !strings.Contains(info, "shadows prelude") || !strings.Contains(info, "\x1b[90m") {
		t.Fatalf("info not dimmed:\n%s", info)
	}
	if strings.Contains(info, "error:") {
		t.Fatalf("info counted as error:\n%s", info)
	}
	if stripANSI(out.String()) != "len = 1\n" {
		t.Fatalf("binding after info: %q", out.String())
	}

	errOut.Reset()
	s.SetPalette(highlight.Palette{})
	if _, err := s.Eval("fn f(x) ->\n    x = 1\n    x\n\n"); err == nil {
		t.Fatal("rebinding should fail")
	}
	semaErr := errOut.String()
	if !strings.Contains(semaErr, "error: <repl>:2:") || !strings.Contains(semaErr, "rebinding") || !strings.Contains(semaErr, "\n    ^\n") {
		t.Fatalf("sema caret:\n%s", semaErr)
	}
	if strings.Contains(semaErr, "sema:") {
		t.Fatalf("sema error printed twice:\n%s", semaErr)
	}

	errOut.Reset()
	fe.Pal = highlight.Palette{}
	if err := fe.Eval(s, "raise(:boom)\n"); err != nil {
		t.Fatal(err)
	}
	raise := errOut.String()
	if !strings.Contains(raise, "error: raise: :boom\n") || !strings.Contains(raise, "\n  at ") {
		t.Fatalf("raise:\n%s", raise)
	}
}
