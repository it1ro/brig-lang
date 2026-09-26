package parser

import (
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/ast"
)

// S-F4: guard из одного идентификатора не должен уходить в tryLambda
// (AUDIT_REPORT.md:73-77). Пробы fn_guard_ident / recv_guard_ident.
func TestParseGuardSingleIdent(t *testing.T) {
	cases := []struct {
		name string
		mode Mode
		src  string
	}{
		{"fn_guard_ident", ModeModule, "module M\nfn f(x) when x -> 1\n"},
		{"recv_guard_ident", ModeRepl, "x = recv n when ok -> n\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := Parse(tc.mode, tc.src); err != nil {
				t.Fatalf("Parse(%v, %q): %v", tc.mode, tc.src, err)
			}
		})
	}
}

// S-F12: stripOuterParens не должен считать скобки внутри строковых
// литералов (AUDIT_REPORT.md:123-126). Проба guard_paren_string.
func TestRoundTripGuardParenString(t *testing.T) {
	src := "module M\nfn f(x) when x == \")\" -> 1\n"
	prog1, err := ParseProgram(ModeModule, src)
	if err != nil {
		t.Fatalf("first parse: %v", err)
	}
	f1 := ast.Format(prog1)
	prog2, err := ParseProgram(ModeModule, f1)
	if err != nil {
		t.Fatalf("second parse: %v\nformatted:\n%s", err, f1)
	}
	f2 := ast.Format(prog2)
	equal := ast.Equal(prog1, prog2)
	idempotent := f1 == f2
	if !equal || !idempotent {
		t.Fatalf("EQUAL=%v IDEMPOTENT=%v\n--- f1 ---\n%s\n--- f2 ---\n%s", equal, idempotent, f1, f2)
	}
}

// S-F3: guard в ветках recv разбирается и выбрасывается (AUDIT_REPORT.md:67-71).
// RecvBranchArg должен сохранять Guard, Format должен его печатать, а
// повторный парсинг форматированного вывода должен давать эквивалентный AST.
func TestAuditRecvGuardSurvivesRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		mode Mode
		src  string
	}{
		{"inline", ModeRepl, `x = recv (:reply, id, v) when has_pending(id) -> v
`},
		{"block", ModeModule, `module Main
fn worker_loop(state) ->
    recv
        (:reply, id, v) when has_pending(state, id) ->
            v
        (:reply, id, v) ->
            v
        (:stop) -> :ok
    else msg
        msg
    after 5000 -> :timeout
`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prog1, err := ParseProgram(tc.mode, tc.src)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}

			formatted := ast.Format(prog1)
			if !strings.Contains(formatted, "when") {
				t.Fatalf("Format output lost guard, want it to contain %q, got:\n%s", "when", formatted)
			}

			prog2, err := ParseProgram(tc.mode, formatted)
			if err != nil {
				t.Fatalf("re-parse of formatted output: %v\n%s", err, formatted)
			}
			if !ast.Equal(prog1, prog2) {
				t.Fatalf("ast.Equal(prog1, prog2) = false after round-trip through Format;\nformatted:\n%s", formatted)
			}
		})
	}
}
