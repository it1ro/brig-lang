package sema

import (
	"testing"

	"github.com/it1ro/brig-lang/internal/parser"
)

// T-243 (§11.3): info о fn main(), которую script не вызывает.
func TestCheckScriptMain(t *testing.T) {
	cases := []struct {
		src  string
		info bool
	}{
		{"x = 1\nfn main() ->\n    print(x)\n", true},
		{"fn main() -> print(1)\nmain()\n", false},
		{"fn main() -> print(1)\nfn run(n) ->\n    match n\n        0 -> main()\n        _ -> 1\nrun(0)\n", false},
		{"fn main() -> print(1)\npid = spawn(main)\n", false},
		{"fn run() -> print(1)\n", false},
	}
	for _, c := range cases {
		prog, err := parser.ParseProgram(parser.ModeScript, c.src)
		if err != nil {
			t.Fatalf("%q: %v", c.src, err)
		}
		res := CheckScriptMain(prog)
		if got := len(res.Diagnostics) == 1; got != c.info {
			t.Fatalf("%q: diagnostics %+v", c.src, res.Diagnostics)
		}
		if c.info {
			d := res.Diagnostics[0]
			if d.Severity != SeverityInfo || d.Line != 2 || d.Col != 1 || d.Message != ScriptMainMessage {
				t.Fatalf("%q: %+v", c.src, d)
			}
		}
	}
}
