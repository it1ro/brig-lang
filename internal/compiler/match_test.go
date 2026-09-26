package compiler_test

import (
	"strings"
	"testing"
)

// T-70: match (§8.3) — литералы, конструкторы, кортежи, списки с ..rest,
// Map-паттерн, as; ветки проверяются по порядку, срабатывает первая.
func TestMatchPatterns(t *testing.T) {
	runModule(t, `module Main
fn lit(v) ->
    match v
        0 -> :zero
        "s" -> :str
        :a -> :atom
        true -> :yes
        _ -> :other

fn opt(v) ->
    match v
        Some(u) -> u
        None -> 0

fn tup(v) ->
    match v
        (:ok, x) -> x
        (:error, _) -> -1

fn lst(v) ->
    match v
        [] -> :empty
        [x] -> x
        [1, ..rest] -> rest
        [_, _, ..] -> :many

fn mp(v) ->
    match v
        %{ "a" => x } -> x
        _ -> :no_key

fn whole(v) ->
    match v
        (:ok, _) as full -> full
        _ -> :no

fn first(v) ->
    match v
        _ -> :first
        0 -> :second

fn main() ->
    assert(lit(0) == :zero)
    assert(lit("s") == :str)
    assert(lit(:a) == :atom)
    assert(lit(true) == :yes)
    assert(lit(1.5) == :other)
    assert(opt(Some(7)) == 7)
    assert(opt(None) == 0)
    assert(tup((:ok, 3)) == 3)
    assert(tup((:error, :boom)) == -1)
    assert(lst([]) == :empty)
    assert(lst([5]) == 5)
    assert(lst([1, 2, 3]) == [2, 3])
    assert(lst([2, 3, 4]) == :many)
    assert(mp(%{ "a" => 42 }) == 42)
    assert(mp(%{ "b" => 1 }) == :no_key)
    assert(whole((:ok, 1)) == (:ok, 1))
    assert(whole((:error, 1)) == :no)
    assert(first(0) == :first)
`)
}

// T-70: match в позиции значения и statement, вложенный match, блочные ветки.
func TestMatchValueAndStatement(t *testing.T) {
	runModule(t, `module Main
fn xs() -> [1, 2, 3]

fn main() ->
    pair = (1, (2, 3))
    s = match pair
        (a, (b, c)) ->
            t = a + b
            t + c
    assert(s == 6)
    n = match xs()
        [x, ..rest] ->
            match rest
                [y, z] -> x + y + z
                _ -> 0
        [] -> -1
    assert(n == 6)
    match pair
        (1, _) -> print("one")
        _ -> print("other")
    assert(pair == (1, (2, 3)))
`)
}

// T-70, §8.1: if c then a else b ≡ match c с ветками true/false.
func TestMatchIfEquivalence(t *testing.T) {
	runModule(t, `module Main
fn via_if(c) -> if c then :a else :b

fn via_match(c) ->
    match c
        true -> :a
        false -> :b

fn main() ->
    assert(via_if(true) == via_match(true))
    assert(via_if(false) == via_match(false))
`)
}

// T-70, §10.4: непокрытый match — авто-raise (:case_clause, val).
func TestMatchCaseClauseRaise(t *testing.T) {
	runModule(t, `module Main
fn pick(v) ->
    match v
        0 -> :zero
        (:ok, x) -> x

fn main() ->
    r = trap(pick(5))
    assert(r == Error((:case_clause, 5)))
    r2 = trap(pick((:err, 1)))
    assert(r2 == Error((:case_clause, (:err, 1))))
    r3 = trap(pick(0))
    assert(r3 == Ok(:zero))
`)
}

// T-70: хвостовая ветка match — TAILCALL, 10^6 итераций без роста кадров.
func TestMatchTailCall(t *testing.T) {
	src := `module Main
fn loop(n) ->
    match n
        0 -> :done
        _ -> loop(n - 1)

fn main() ->
    assert(loop(1000000) == :done)
`
	img := compileModule(t, src)
	dis := img.Functions["loop"].Disassemble()
	if !strings.Contains(dis, "TAILCALL") {
		t.Fatalf("loop lacks TAILCALL:\n%s", dis)
	}
	if strings.Contains(dis, " CALL ") {
		t.Fatalf("loop has non-tail CALL:\n%s", dis)
	}
	runModule(t, src)
}
