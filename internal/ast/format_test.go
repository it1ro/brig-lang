package ast_test

import (
	"testing"

	"github.com/it1ro/brig-lang/internal/ast"
	"github.com/it1ro/brig-lang/internal/parser"
)

// roundTrip: parse → format → parse, Equal(ast, ast').
func roundTrip(t *testing.T, mode parser.Mode, src string) {
	t.Helper()
	a1, err := parser.ParseProgram(mode, src)
	if err != nil {
		t.Fatalf("first parse %q: %v", src, err)
	}
	out := ast.Format(a1)
	a2, err := parser.ParseProgram(mode, out)
	if err != nil {
		t.Fatalf("second parse %q\nformatted:\n%s\n%v", src, out, err)
	}
	if !ast.Equal(a1, a2) {
		t.Fatalf("round-trip mismatch for %q\nformatted:\n%s", src, out)
	}
}

func TestRoundTripModule(t *testing.T) {
	cases := []string{
		"module Http.Client\n",
		"module M\nimport Json\nimport A.B\n",
		"module M\nalias Http.Client as Http\n",
		"module M\ntype UserId = Int\n",
		"module M\ntype User { id: Int, name: Str }\n",
		"module M\ntype Color { Red, Green, Blue }\n",
		"module M\ntype Result2<T, E> { Ok(T), Error(E) }\n",
		"module M\ntype Callback = (Int, Str) -> Bool\n",
		"module M\nfn main() ->\n    print(\"hi\")\n",
		"module M\nfn classify(0) -> \"zero\"\nfn classify(n) -> \"many\"\n",
		"module M\nfn f(x, ..rest) -> x\n",
	}
	for _, src := range cases {
		roundTrip(t, parser.ModeModule, src)
	}
}

func TestRoundTripRepl(t *testing.T) {
	cases := []string{
		"1 + 2 * 3\n",
		"1 + 2 * 3 ** 4\n",
		"-x ** 2\n",
		"a and b or c\n",
		"not x\n",
		"xs |> map(f) |> filter(g)\n",
		"1 to 10 |> list\n",
		"x.name.field\n",
		"v[0][1]\n",
		"t = (1, \"a\", :ok)\n",
		"(x,)\n",
		"(x)\n",
		"()\n",
		"l = [1, 2, 3]\n",
		"v = %[1, 2, 3]\n",
		"m = %{ \"a\" => 1, \"b\" => 2 }\n",
		"r = User{ id: 1, name: \"a\" }\n",
		"an = { id: 1 }\n",
		"f = x -> x * 2\n",
		"f = () -> 42\n",
		"g = fn (x, y) ->\n    x + y\n",
		"x = if c then a else b\n",
		"if ready\n    start()\nelse\n    wait()\n",
		"match v\n    Some(u) -> u\n    None -> 0\n",
		"result = trap(1 + 1)\n",
		"result = trap\n    f1()\nensure close_f1()\nresult\n",
	}
	for _, src := range cases {
		roundTrip(t, parser.ModeRepl, src)
	}
}

// Идемпотентность: format(parse(format(ast))) == format(ast).
func TestFormatIdempotent(t *testing.T) {
	src := "module M\nfn main() ->\n    x = 1\n    x + 2\n"
	a1, err := parser.ParseProgram(parser.ModeModule, src)
	if err != nil {
		t.Fatal(err)
	}
	f1 := ast.Format(a1)
	a2, err := parser.ParseProgram(parser.ModeModule, f1)
	if err != nil {
		t.Fatal(err)
	}
	f2 := ast.Format(a2)
	if f1 != f2 {
		t.Fatalf("not idempotent:\n--- f1 ---\n%s\n--- f2 ---\n%s", f1, f2)
	}
}

func FuzzRoundTrip(f *testing.F) {
	f.Add("fn main() ->\n    1 + 2\n")
	f.Add("x = %{ \"a\" => 1 }\n")
	f.Fuzz(func(t *testing.T, src string) {
		for _, m := range []parser.Mode{parser.ModeModule, parser.ModeRepl} {
			prog, err := parser.ParseProgram(m, src)
			if err != nil {
				continue
			}
			out := ast.Format(prog)
			prog2, err := parser.ParseProgram(m, out)
			if err != nil {
				t.Fatalf("re-parse failed:\n--- in ---\n%s\n--- out ---\n%s\n%v",
					src, out, err)
			}
			if !ast.Equal(prog, prog2) {
				t.Fatalf("round-trip mismatch:\n--- in ---\n%s\n--- out ---\n%s",
					src, out)
			}
		}
	})
}
