package compiler_test

import (
	"strings"
	"testing"
)

// T-72: `x |> f(a…)` ≡ `f(x, a…)` (§7.5).
func TestPipe(t *testing.T) {
	runModule(t, `module Main
fn double(x) -> x * 2
fn sub(a, b) -> a - b
fn sum3(a, b, c) -> a + b + c
fn count(..xs) -> len(xs)

fn main() ->
    assert(3 |> double() == 6)
    assert(3 |> double == 6)
    assert(10 |> sub(4) == 6)
    assert(1 |> sum3(2, 3) == 6)
    assert(1 |> double() |> double() |> sub(3) == 1)
    ys = [2, 3]
    assert(1 |> count(..ys) == 3)
    assert([1, 2] |> len() == 2)
    assert(Json.encode(1) == 1 |> Json.encode())
`)
}

func TestPipeTail(t *testing.T) {
	runModule(t, `module Main
fn dec(n) -> n - 1

fn loop(n) ->
    if n == 0 then :done else n |> dec() |> loop()

fn main() -> assert(loop(1000000) == :done)
`)
}

func TestPipeMethodFormRejected(t *testing.T) {
	err := runModuleErr(t, `module Main
fn main() ->
    r = 1
    1 |> r.area()
`)
	if err == nil || !strings.Contains(err.Error(), "|>") {
		t.Fatalf("want compile error mentioning |>, got %v", err)
	}
}
