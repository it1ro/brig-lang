package parser

import (
	"strings"
	"testing"
)

func TestParseModuleModeRejectsTopLevelLet(t *testing.T) {
	err := Parse(ModeModule, "x = 1\n")
	if err == nil {
		t.Fatal("top-level let in module mode must fail (§9)")
	}
	if !strings.Contains(err.Error(), "module mode") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseModuleModeOK(t *testing.T) {
	src := `module Http.Client

import Json

type User { id: Int }

fn main() ->
    print("hi")
`
	if err := Parse(ModeModule, src); err != nil {
		t.Fatalf("parse failed: %v", err)
	}
}

func TestParseReplAllowsTopLevelExpr(t *testing.T) {
	if err := Parse(ModeRepl, "1 + 1\n"); err != nil {
		t.Fatalf("repl mode must allow top-level expr: %v", err)
	}
}

func FuzzParse(f *testing.F) {
	f.Add("fn main() ->\n    1 + 2\n")
	f.Add("x = 1\n")
	f.Fuzz(func(_ *testing.T, data string) {
		_ = Parse(ModeRepl, data)
	})
}
