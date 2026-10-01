package repl_test

import (
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/repl"
	"github.com/it1ro/brig-lang/internal/runtime"
)

// TestAnswerStringForms — T-290 (A.1–A.4): ответ REPL печатает строку
// литералом, управляющие символы экранированы, многострочная строка —
// литерал `"""`; каждый ответ, введённый обратно, даёт равное значение.
func TestAnswerStringForms(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"", `"sdffd"`, `"sdffd"`},
		{"", `""`, `""`},
		{"", `"1"`, `"1"`},
		{"", `"\u{1b}]0;x\u{7}"`, `"\u{1b}]0;x\u{7}"`},
		{"", `["a", "\u{1b}"]`, `["a", "\u{1b}"]`},
		{"", `"<html>\n  <p>hi</p>\n</html>"`, "\"\"\"\n<html>\n  <p>hi</p>\n</html>\n\"\"\""},
		{"", `"a\n\n  \nb\"\"\"c\n"`, "\"\"\"\na\n\n\\u{20} \nb\\\"\"\"c\n\n\"\"\""},
		{"body", `"x\ny"`, "body = \"\"\"\n       x\n       y\n       \"\"\""},
	}
	s, out := helperSession(t)
	for _, c := range cases {
		v := mustEval(t, s, out, c.src+"\n")
		got, ok := repl.FormatAnswer(c.name, v, repl.Print{})
		if !ok || got != c.want {
			t.Errorf("answer %s:\n got %q\nwant %q", c.src, got, c.want)
			continue
		}
		back := strings.TrimPrefix(got, c.name+" = ")
		if c.name != "" {
			back = c.name + " = " + back
		}
		res, err := s.Eval(back + "\n")
		if err != nil || len(res) == 0 {
			t.Fatalf("eval answer %q: %v\n%s", back, err, out.String())
		}
		if !runtime.Equal(res[len(res)-1].Value, v) {
			t.Errorf("round-trip %q = %s, want %s", back, res[len(res)-1].Value.Inspect(), v.Inspect())
		}
	}
}

// TestInspectFunctionForm — T-290 (A.7): функция печатается без имён
// компилятора: именованная — `#<fn name/N>`, лямбда — `#<clsr/N место>`,
// variadic — арность `*`.
func TestInspectFunctionForm(t *testing.T) {
	s, out := helperSession(t)
	mustEval(t, s, out, "load(\""+replFile("tools.brig")+"\")\n")
	mustEval(t, s, out, "fn sq(n) -> n * n\n")
	cases := []struct{ src, want string }{
		{"sq", "#<fn sq/1>"},
		{"() -> ()", "#<clsr/0 <repl>:2>"},
		{"print", "#<fn print/*>"},
		{"Tools.routes", "#<fn Tools.routes/0>"},
		{"Enum.map", "#<fn Enum.map/2>"},
		{"fn (..xs) -> xs", "#<clsr/* <repl>:6>"},
	}
	for _, c := range cases {
		if got := mustEval(t, s, out, c.src+"\n").Inspect(); got != c.want {
			t.Errorf("%s = %s, want %s", c.src, got, c.want)
		}
	}
	if got := mustEval(t, s, out, "fn g(a) ->\n    fn hh(b) -> b\n    hh\n\ng(1)\n").Inspect(); got != "#<fn hh/1>" {
		t.Errorf("local fn = %s, want #<fn hh/1>", got)
	}
}
