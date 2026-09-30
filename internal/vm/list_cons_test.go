package vm_test

import "testing"

// T-271 (DD #397, §4.2): спреды и разбор списка на cons-ячейках —
// последний спред разделяется хвостом, остальные копируются; `[h, ..t]`
// отдаёт тот же хвост. Семантика — прежняя.
func TestListConsSpreadAndPattern(t *testing.T) {
	runModuleSync(t, `module Main
fn sum([], acc) -> acc
fn sum([h, ..t], acc) -> sum(t, acc + h)

fn shape(xs) ->
    match xs
        [_, _] -> :two
        [_, _, _, _] -> :four
        [_, _, _, _, _, ..] -> :five_plus
        _ -> :other

fn main() ->
    xs = [2, 3]
    ys = [0, 1, ..xs]
    assert(ys == [0, 1, 2, 3])
    assert(xs == [2, 3])
    assert(len(ys) == 4)
    assert([..xs] == xs)
    assert([..xs, ..xs] == [2, 3, 2, 3])
    assert([..xs, 4] == [2, 3, 4])
    assert([..[], ..xs] == xs)
    assert([1, ..[]] == [1])
    assert([..xs, ..[]] == xs)
    [a, b, ..rest] = ys
    assert((a, b, rest) == (0, 1, [2, 3]))
    [_, _, _, _, ..empty] = ys
    assert(empty == [])
    assert(shape(ys) == :four)
    assert(shape([1, ..ys]) == :five_plus)
    assert(shape([9]) == :other)
    assert(ys[3] == 3)
    assert(sum(ys, 0) == 6)
    assert(map(ys, x -> x * 2) == [0, 2, 4, 6])
    assert(filter(ys, x -> x > 1) == xs)
    assert(fold(ys, 0, (acc, x) -> acc * 10 + x) == 123)
    assert(find(ys, x -> x > 1) == Some(2))
    assert(find(ys, x -> x > 9) == None)
    assert(any(ys, x -> x == 3))
    assert(not all(ys, x -> x < 3))
    assert(to_str(ys) == "[0, 1, 2, 3]")
    assert([0, 1, 2] < ys)
`)
}

// TestListConsTypeErrors — спред не-List в списке по-прежнему ловимый
// :type_error, в том числе в последней позиции.
func TestListConsTypeErrors(t *testing.T) {
	runModuleSync(t, `module Main
fn tail_spread(v) ->
    r = trap([1, ..v])
    r

fn main() ->
    assert(tail_spread(%[2]) == Error((:type_error, (:spread, %[2]))))
    assert(tail_spread(3) == Error((:type_error, (:spread, 3))))
    assert(tail_spread([2]) == Ok([1, 2]))
`)
}

// TestListPrependAllocBudget — `[x, ..acc]` списывает с бюджета хода
// только новые ячейки: 2000 prepend укладываются в линейный лимит
// (раньше списывался весь список на каждом шаге — квадрат).
func TestListPrependAllocBudget(t *testing.T) {
	runModuleSync(t, budgetPrelude+`
fn build(0, acc) -> acc
fn build(n, acc) -> build(n - 1, [n, ..acc])

fn main() ->
    (_p, ref) = spawn_watched(() -> build(2_000, []), { turn_alloc_bytes: 5_000_000 })
    assert(take() == (:down, ref, :normal))
`)
}
