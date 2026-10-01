package repl_test

import (
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/runtime"
)

// T-146: встроенные модули на Brig видны в сессии без import; как у
// зависимости, вводу видны только pub (§11.4), h берёт `##` из исходника.
func TestSessionStdlib(t *testing.T) {
	s, out := helperSession(t)

	got := mustEval(t, s, out, "[3, 1, 2] |> Enum.sort() |> Enum.take(2)\n")
	if !runtime.Equal(got, runtime.List(runtime.Int(1), runtime.Int(2))) {
		t.Fatalf("List: %s", got.Inspect())
	}
	got = mustEval(t, s, out, "Option.unwrap_or(None, 7)\n")
	if !runtime.Equal(got, runtime.Int(7)) {
		t.Fatalf("Option: %s", got.Inspect())
	}

	out.Reset()
	if _, err := s.Eval("List.subject([1], :x)\n"); err == nil {
		t.Fatal("private List.subject callable from input")
	}
	if !strings.Contains(out.String(), "subject/2 is private to List") {
		t.Fatalf("private: %q", out.String())
	}

	out.Reset()
	mustEval(t, s, out, "h(List)\n")
	for _, frag := range []string{"Функции над `List`", "concat/2"} {
		if !strings.Contains(out.String(), frag) {
			t.Fatalf("h(List) missing %q:\n%s", frag, out.String())
		}
	}
	if strings.Contains(out.String(), "subject") {
		t.Fatalf("h(List) lists private fn:\n%s", out.String())
	}
	out.Reset()
	mustEval(t, s, out, "h(Result.all)\n")
	if !strings.Contains(out.String(), "Result.all(rs)") || !strings.Contains(out.String(), "первый `Error`") {
		t.Fatalf("h(Result.all):\n%s", out.String())
	}
}
