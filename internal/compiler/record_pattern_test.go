package compiler_test

import (
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/parser"
)

// T-74, §9.6: record-паттерны в match, параметре fn, recv и with;
// сопоставление частичное по полям.
func TestRecordPatterns(t *testing.T) {
	runModule(t, `module Main
type User { id: Int, name: Str }
type Admin { id: Int, name: Str }

fn full(u) ->
    match u
        User{ id: id, name: name } -> (id, name)
        _ -> :no

fn partial(u) ->
    match u
        User{ id: id } -> id
        _ -> :no

fn param(User{ id: id }) -> id

fn anon(r) ->
    match r
        { id: id } -> id
        _ -> :no

fn nested(r) ->
    match r
        User{ id: 1, name: n } -> n
        User{ id: _, name: n } -> "other"
        _ -> :no

fn main() ->
    u = User{ id: 1, name: "a" }
    assert(full(u) == (1, "a"))
    assert(partial(u) == 1)
    assert(param(u) == 1)
    assert(nested(u) == "a")
    assert(nested(User{ id: 2, name: "b" }) == "other")

    assert(full(Admin{ id: 1, name: "a" }) == :no)
    assert(partial(Admin{ id: 1, name: "a" }) == :no)
    assert(partial({ id: 1, name: "a" }) == :no)
    assert(full({ id: 1, name: "a" }) == :no)
    assert(anon({ id: 7 }) == 7)
    assert(anon({ id: 7, name: "a" }) == 7)
    assert(anon(u) == :no)
    assert(anon(Admin{ id: 1, name: "a" }) == :no)
    assert(partial(User{ name: "a", id: 3 }) == 3)
    assert(partial(User{ name: "a" }) == :no)
    assert(partial(42) == :no)
    assert(partial((1, 2)) == :no)

    send(self(), User{ id: 5, name: "x" })
    got = recv
        Admin{ id: id } -> (:admin, id)
        User{ id: id } -> (:user, id)
    assert(got == (:user, 5))

    send(self(), { id: 9 })
    got2 = recv
        User{ id: id } -> (:user, id)
        { id: id } -> (:anon, id)
    assert(got2 == (:anon, 9))

    v = with
        User{ id: id } <- u
        id + 1
    assert(v == 2)
    w = with
        Admin{ id: id } <- u
        id + 1
    assert(w == u)
`)
}

// T-74, §9.6: `..` в record-паттерне отвергается на этапе компиляции
// с позицией.
func TestRecordPatternRejectsSpread(t *testing.T) {
	src := `module Main
type User { id: Int }
fn main() ->
    match User{ id: 1 }
        User{ ..r } -> r
        _ -> 0
`
	_, err := parser.ParseProgram(parser.ModeModule, src)
	if err == nil {
		t.Fatal("want compile error for `..` in record pattern, got nil")
	}
	if !strings.Contains(err.Error(), "5:") {
		t.Fatalf("want position 5:.. in error, got %v", err)
	}
}

// T-74: неизвестный тип, неизвестное и повторное поле в record-паттерне —
// ошибка компиляции с позицией.
func TestRecordPatternCompileErrors(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"unknown type", `module Main
fn main() ->
    match 1
        Nope{ id: x } -> x
        _ -> 0
`, "4:"},
		{"unknown field", `module Main
type User { id: Int }
fn main() ->
    match 1
        User{ nme: x } -> x
        _ -> 0
`, "5:"},
		{"duplicate field", `module Main
fn main() ->
    match 1
        { a: x, a: y } -> x
        _ -> 0
`, "4:"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runModuleErr(t, tc.src)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error with %q, got %v", tc.want, err)
			}
		})
	}
}
