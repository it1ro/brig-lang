package compiler_test

import "testing"

// T-130 (T-120, вариант A): коллекция — первый аргумент, pipe (§7.5)
// подставляет её первой.
func TestPreludePipeMap(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    assert([1, 2] |> Enum.map(x -> x * 2) == [2, 4])
    assert([1, 2, 3] |> Enum.filter(x -> x > 1) |> Enum.map(x -> x + 1) == [3, 4])
    assert([1, 2] |> Enum.find(x -> x == 2) == Some(2))
    assert([1, 2] |> Enum.all?(x -> x > 0))
    assert([1, 2] |> Enum.any?(x -> x > 1))
`)
}

func TestPreludePipeFold(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    assert([1, 2, 3] |> Enum.fold(0, fn (acc, x) -> acc + x) == 6)
    assert(Enum.fold([1, 2, 3], 10, fn (acc, x) -> acc - x) == 4)
`)
}

// Старый порядок (функция первой) — ловимый (:type_error, (:op, значение)),
// а не падение внутри колбэка.
func TestPreludeOldArgOrderIsTypeError(t *testing.T) {
	runModule(t, `module Main
fn is_map_err(r) ->
    match r
        Error((:type_error, ((:enum, :map), _))) -> true
        _ -> false

fn is_fold_err(r) ->
    match r
        Error((:type_error, ((:enum, :fold), _))) -> true
        _ -> false

fn main() ->
    r = trap(Enum.map(fn (x) -> x, [1]))
    assert(is_map_err(r))
    r2 = trap(Enum.fold(fn (a, x) -> a, 0, [1]))
    assert(is_fold_err(r2))
`)
}
