package compiler_test

import (
	"strings"
	"testing"
)

// A-F3 / T-81 (design decision #41, variant A): if-условие, оба операнда
// and/or и guard требуют строгий Bool; иначе (:type_error, (:expected_bool, v)).
func TestStrictBoolNonBoolRaisesTypeError(t *testing.T) {
	cases := map[string]string{
		"if": `module Main
fn main() ->
    if 5 then :yes else :no
`,
		"or_left": `module Main
fn main() ->
    1 or 2
`,
		"and_left": `module Main
fn main() ->
    1 and true
`,
		"and_right": `module Main
fn main() ->
    true and 5
`,
		"or_right": `module Main
fn main() ->
    false or 5
`,
		"fn_guard": `module Main
fn f(x) when x -> :yes
fn main() ->
    f(5)
`,
		"recv_guard": `module Main
fn main() ->
    send(self(), 5)
    recv
        n when n -> :yes
        _ -> :no
`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			err := runModuleErr(t, src)
			if err == nil || !strings.Contains(err.Error(), "type_error") ||
				!strings.Contains(err.Error(), "expected_bool") {
				t.Fatalf("want (:type_error, (:expected_bool, ...)), got %v", err)
			}
		})
	}
}

func TestStrictBoolShortCircuitAndBoolPassThrough(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    assert((false and 5) == false)
    assert((true or 5) == true)
    assert((true and false) == false)
    assert((false or true) == true)
    assert((if true then 1 else 2) == 1)
`)
}
