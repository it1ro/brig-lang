package parser

import (
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/ast"
)

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
