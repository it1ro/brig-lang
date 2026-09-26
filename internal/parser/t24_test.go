package parser

import (
	"testing"

	"github.com/it1ro/brig-lang/internal/ast"
)

// S-F8: sep ::= NEWLINE in args / params / tuple (AUDIT_REPORT.md:104-107).
// Probes args_newline_sep, params_newline_sep; tuple after the mandatory first comma.
func TestParseNewlineSepArgsParamsTuple(t *testing.T) {
	cases := []struct {
		name string
		mode Mode
		src  string
	}{
		{
			name: "args_newline_sep",
			mode: ModeModule,
			src: `module M
fn main() ->
    f(
        1
        2
    )
`,
		},
		{
			name: "params_newline_sep",
			mode: ModeModule,
			src: `module M
fn f(
    a
    b
) -> a
`,
		},
		{
			name: "tuple_newline_sep",
			mode: ModeModule,
			src: `module M
fn main() ->
    t = (1,
        2
        3
    )
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := Parse(tc.mode, tc.src); err != nil {
				t.Fatalf("Parse(%v, %s): %v", tc.mode, tc.name, err)
			}
		})
	}
}

// S-F9: unit pattern () (AUDIT_REPORT.md:109-111). Probe unit_pattern.
func TestParseUnitPattern(t *testing.T) {
	src := `module M
fn f(()) -> 1
`
	prog, err := ParseProgram(ModeModule, src)
	if err != nil {
		t.Fatalf("ParseProgram: %v", err)
	}
	if len(prog.Decls) != 1 {
		t.Fatalf("want 1 decl, got %d", len(prog.Decls))
	}
	fn, ok := prog.Decls[0].(ast.FuncDecl)
	if !ok {
		t.Fatalf("want FuncDecl, got %T", prog.Decls[0])
	}
	clauses := fn.FuncClauses()
	if len(clauses) != 1 || len(clauses[0].Params) != 1 {
		t.Fatalf("want one clause with one param, got %+v", clauses)
	}
	if got := clauses[0].Params[0]; got != "()" {
		t.Fatalf("param = %q, want %q", got, "()")
	}
}

// S-F10: with_item ::= bind_stmt | stmt in any order (AUDIT_REPORT.md:113-115).
// Probe with_interleave.
func TestParseWithInterleave(t *testing.T) {
	src := `module M
fn main() ->
    with
        Ok(a) <- validate(x)
        print(a)
        Ok(b) <- transform(a)
        Ok(b)
`
	if err := Parse(ModeModule, src); err != nil {
		t.Fatalf("Parse with_interleave: %v", err)
	}
}
