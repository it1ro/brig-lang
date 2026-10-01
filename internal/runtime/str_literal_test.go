package runtime_test

import (
	"bytes"
	"testing"

	"github.com/it1ro/brig-lang/internal/repl"
	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/vm"
)

// TestInspectStrLiteral — T-290 (A.1, A.3): Inspect строки — литерал Brig с
// escape по §C, который разбирается обратно в ту же строку; Display —
// исходный текст.
func TestInspectStrLiteral(t *testing.T) {
	cases := []struct{ in, want string }{
		{"sdffd", `"sdffd"`},
		{"", `""`},
		{"1", `"1"`},
		{"a\nb", `"a\nb"`},
		{"\t\r", `"\t\r"`},
		{"\x00", `"\0"`},
		{`say "hi"`, `"say \"hi\""`},
		{`a\b`, `"a\\b"`},
		{`\(x)`, `"\\(x)"`},
		{"\x1b]0;x\x07", `"\u{1b}]0;x\u{7}"`},
		{"​", `"\u{200b}"`},
		{"  ", `"\u{2028}\u{2029}"`},
		{"ёлка 日本 🙂", `"ёлка 日本 🙂"`},
	}
	var out bytes.Buffer
	s := repl.New(vm.New(), &out)
	t.Cleanup(s.Close)
	for _, c := range cases {
		v := runtime.Str(c.in)
		if got := v.Inspect(); got != c.want {
			t.Errorf("Inspect(%q) = %s, want %s", c.in, got, c.want)
		}
		if got := v.Display(); got != c.in {
			t.Errorf("Display(%q) = %q", c.in, got)
		}
		res, err := s.Eval(v.Inspect() + "\n")
		if err != nil || len(res) != 1 {
			t.Fatalf("eval %s: %v %v (%s)", v.Inspect(), res, err, out.String())
		}
		if got := res[0].Value; got.Kind != runtime.KindStr || got.Str != c.in {
			t.Errorf("round-trip %s = %q, want %q", v.Inspect(), got.Str, c.in)
		}
	}
	if got := runtime.List(runtime.Str("\x07")).Inspect(); got != `["\u{7}"]` {
		t.Errorf(`Inspect(["\u{7}"]) = %s`, got)
	}
}
