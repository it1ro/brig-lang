package compiler_test

import "testing"

// T-140 (#203): `(a, b) -> expr` и `fn ->` исполняются (§6.2).
func TestRunShortLambdaMultiParam(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    add = (a, b) -> a + b
    assert(add(2, 3) == 5)
    assert(Enum.fold([1, 2, 3, 4], 0, (acc, x) -> acc + x) == 10)
    base = 100
    f3 = (a, b, c) -> base + a * b - c
    assert(f3(2, 3, 1) == 105)
    a = 4
    b = 5
    pair = (a, b)
    assert(pair == (4, 5))
    one = (x) -> x + 1
    assert(one(1) == 2)
    thunk = fn ->
        y = base * 2
        y + 1
    assert(thunk() == 201)
    k = fn -> 7
    assert(k() == 7)
`)
}
