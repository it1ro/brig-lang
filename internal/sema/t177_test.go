package sema_test

import (
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/parser"
	"github.com/it1ro/brig-lang/internal/sema"
)

// resultInfos — info-диагностики «result of X is ignored» (T-177) из
// обоих проходов sema: Check и CheckNames.
func resultInfos(t *testing.T, src string) (check, names []string) {
	t.Helper()
	prog, err := parser.ParseProgram(parser.ModeModule, src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pick := func(r *sema.Result) []string {
		if r.HasErrors() {
			t.Fatalf("unexpected errors: %v", r.Diagnostics)
		}
		var out []string
		for _, d := range r.Diagnostics {
			if d.Severity == sema.SeverityInfo && strings.HasPrefix(d.Message, "result of ") {
				out = append(out, d.Message)
			}
		}
		return out
	}
	return pick(sema.Check(prog)), pick(sema.CheckNames(prog, nil))
}

func TestSemaInfoUnderscoreResultBind(t *testing.T) {
	cases := []struct {
		name, body, want string
	}{
		{"port_write", `_ <- Port.write(p, "x")`, "Port.write"},
		{"send", `_ <- send(p, 1)`, "send"},
		{"prelude_send", `_ <- Prelude.send(p, 1)`, "Prelude.send"},
		{"await", `_ <- await(p, 10)`, "await"},
		{"register", `_ <- register(:n, p)`, "register"},
		{"json_decode_pipe", `_ <- "1" |> Json.decode()`, "Json.decode"},
		{"telemetry", `_ <- Telemetry.attach(:id, [:a], p)`, "Telemetry.attach"},
		{"http_respond", `_ <- HttpServer.respond(p, 200, [])`, "HttpServer.respond"},
		{"print", `_ <- print(p)`, ""},
		{"exit_always_ok", `_ <- exit(p, :normal)`, ""},
		{"port_close_unit", `_ <- Port.close(p)`, ""},
		{"ok_pattern", `Ok(_) <- Port.write(p, "x")`, ""},
		{"named", `r <- Port.write(p, "x")`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := "module Main\nfn f(p) ->\n    with\n        " + tc.body + "\n        :ok\n"
			for i, got := range func() [][]string { c, n := resultInfos(t, src); return [][]string{c, n} }() {
				if tc.want == "" {
					if len(got) != 0 {
						t.Fatalf("pass %d: want no info, got %v", i, got)
					}
					continue
				}
				want := "result of " + tc.want + " is ignored; use Ok(_) <-"
				if len(got) != 1 || got[0] != want {
					t.Fatalf("pass %d: want [%q], got %v", i, want, got)
				}
			}
		})
	}
}

// Своё имя затеняет встроенное: fn модуля, переменная, alias модуля.
func TestSemaInfoUnderscoreResultBindShadowed(t *testing.T) {
	for name, src := range map[string]string{
		"own_fn": `module Main
fn send(a, b) -> (a, b)
fn f(p) ->
    with
        _ <- send(p, 1)
        :ok
`,
		"local_var": `module Main
fn f(p, send) ->
    with
        _ <- send(p, 1)
        :ok
`,
		"local_fn": `module Main
fn f(p) ->
    fn await(a, b) -> a
    with
        _ <- await(p, 1)
        :ok
`,
	} {
		t.Run(name, func(t *testing.T) {
			c, n := resultInfos(t, src)
			if len(c)+len(n) != 0 {
				t.Fatalf("want no info, got %v / %v", c, n)
			}
		})
	}
}
