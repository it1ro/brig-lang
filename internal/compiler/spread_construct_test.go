package compiler_test

import (
	"strings"
	"testing"
)

func TestListSpreadConstruct(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    xs = [1, 2]
    assert([0, ..xs, 99] == [0, 1, 2, 99])
    assert([..xs] == [1, 2])
    assert([..xs, ..[3]] == [1, 2, 3])
    assert([..[]] == [])
    v = %[4, 5]
    rv = trap([..v])
    assert(rv == Error((:type_error, (:spread, v))))
    r = trap([..5])
    assert(r == Error((:type_error, (:spread, 5))))
    m = %{ "a" => 1 }
    r2 = trap([..m])
    assert(r2 == Error((:type_error, (:spread, m))))
`)
}

func TestVectorSpreadConstruct(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    xs = [1, 2]
    assert(%[0, ..xs] == %[0, 1, 2])
    assert(%[..xs, 99] == %[1, 2, 99])
    v = %[3]
    assert(%[0, ..v, ..xs] == %[0, 3, 1, 2])
    r = trap(%[..5])
    assert(r == Error((:type_error, (:spread, 5))))
`)
}

func TestMapSpreadConstruct(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    m = %{ "a" => 1, "b" => 2 }
    assert(%{ ..m, "c" => 3 } == %{ "a" => 1, "b" => 2, "c" => 3 })
    assert(%{ ..m, "a" => 9 } == %{ "a" => 9, "b" => 2 })
    assert(%{ "a" => 0, ..m } == %{ "a" => 1, "b" => 2 })
    n = %{ "b" => 8, "d" => 4 }
    assert(%{ ..m, ..n } == %{ "a" => 1, "b" => 8, "d" => 4 })
    assert(%{ ..%{} } == %{})
    r = trap(%{ ..[1] })
    assert(r == Error((:type_error, (:spread, [1]))))
    r2 = trap(%{ ..5 })
    assert(r2 == Error((:type_error, (:spread, 5))))
`)
}

func TestCallSpreadAnyPosition(t *testing.T) {
	runModule(t, `module Main
fn f(a, b, c) -> (a, b, c)
fn g(head, ..rest) -> (head, rest)
fn main() ->
    assert(f(1, ..[2], 3) == (1, 2, 3))
    assert(f(..[1, 2], 3) == (1, 2, 3))
    assert(f(1, 2, ..[3]) == (1, 2, 3))
    assert(f(..[1], ..[2], ..[3]) == (1, 2, 3))
    assert(f(1, ..[], 2, ..[], 3) == (1, 2, 3))
    assert(g(0, ..[1, 2], 9) == (0, [1, 2, 9]))
    empty = trap(g(..[]))
    assert(empty == Error((:function_clause, [])))
    ys = [2]
    assert(1 |> f(..ys, 3) == (1, 2, 3))
    assert(len(..[[1, 2, 3]]) == 3)
    r = trap(f(1, ..5, 3))
    assert(r == Error((:type_error, (:spread, 5))))
`)
}

func TestCallSpreadVariadicCaptureMid(t *testing.T) {
	runModule(t, `module Main
fn run(k) ->
    fn go(acc) -> acc + k
    fn go(acc, y, ..ys) -> go(acc + y + k, ..ys)
    go(0, 1, ..[2], 3)
fn main() ->
    assert(run(10) == 46)
`)
}

func TestCallSpreadLocalCaptureStillRejected(t *testing.T) {
	err := runModuleErr(t, `module Main
fn outer(k) ->
    fn add(a, b) -> a + b + k
    add(1, ..[2])
fn main() ->
    outer(1)
`)
	if err == nil || !strings.Contains(err.Error(), "спред-вызов локальной fn с захватом") {
		t.Fatalf("want capture-spread compile error, got: %v", err)
	}
}
