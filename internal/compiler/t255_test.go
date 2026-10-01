package compiler_test

import "testing"

// T-255: '+'/'-' не продолжают строку (§2.2) — строка с ведущим '-' —
// новый стейтмент с унарным минусом.
func TestUnaryMinusLineIsStatement(t *testing.T) {
	runModule(t, `module Main
fn f(x) ->
    y = x
    -y

fn g() ->
    print("a")
    -1

fn main() ->
    assert(f(5) == -5)
    assert(g() == -1)
`)
	out := captureStdout(t, func() {
		runModule(t, `module Main
fn main() ->
    print("a")
    -1
`)
	})
	if out != "a\n" {
		t.Fatalf("stdout = %q, want %q", out, "a\n")
	}
}
