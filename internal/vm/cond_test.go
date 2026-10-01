package vm_test

import "testing"

// T-253: `cond` (§8.4) — ветки сверху вниз; ни одной истинной →
// raise((:cond_clause, ())), ловится trap; не-Bool условие → :type_error.
func TestCondNoClause(t *testing.T) {
	runModuleSync(t, `module Main
fn sign(n) ->
    cond
        n < 0 -> :neg
        n == 0 -> :zero
        true -> :pos

fn pick(n) ->
    cond
        n > 0 -> 1

fn label(n) ->
    s = cond
        n > 100 ->
            t = "big"
            t
        true -> "small"
    s

fn main() ->
    assert(sign(-3) == :neg)
    assert(sign(0) == :zero)
    assert(sign(5) == :pos)
    assert(label(500) == "big")
    assert(label(5) == "small")
    assert(pick(1) == 1)
    assert(trap(pick(0)) == Error((:cond_clause, ())))
    r = trap(cond
        1 -> 2)
    assert(r == Error((:type_error, (:expected_bool, 1))))
`)
}
