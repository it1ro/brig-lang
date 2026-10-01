package vm_test

import "testing"

// TestMapExtras — Map.to_list, from_list, values, filter, update (T-281).
func TestMapExtras(t *testing.T) {
	runModuleSync(t, `module Main

fn main() ->
    m = %{"b" => 2, "a" => 1, "c" => 3}

    # to_list и values — в порядке печати (term order).
    assert(Map.to_list(m) == [("a", 1), ("b", 2), ("c", 3)])
    assert(Map.to_list(%{}) == [])
    assert(Map.values(m) == [1, 2, 3])
    assert(Map.values(%{}) == [])

    # from_list — правый побеждает (§5.2); обратима к to_list.
    assert(Map.from_list([("a", 1), ("b", 2), ("a", 3)]) == %{"a" => 3, "b" => 2})
    assert(Map.from_list([]) == %{})
    assert(Map.from_list(Map.to_list(m)) == m)

    # filter — предикат от (k, v), результат Map.
    assert(Map.filter(m, (k, v) -> v > 1) == %{"b" => 2, "c" => 3})
    assert(Map.filter(m, (k, v) -> k == "a") == %{"a" => 1})
    assert(Map.filter(m, (k, v) -> false) == %{})
    assert(Map.filter(%{}, (k, v) -> true) == %{})

    # update(m, k, default, f) — f от текущего значения или default.
    assert(Map.update(m, "a", 0, x -> x + 10) == %{"a" => 11, "b" => 2, "c" => 3})
    assert(Map.update(m, "z", 0, x -> x + 10) == %{"a" => 1, "b" => 2, "c" => 3, "z" => 10})
    assert(Map.update(%{}, :n, [], xs -> [1, ..xs]) == %{:n => [1]})

    # в конвейере субъект первым
    assert(m |> Map.filter((k, v) -> v < 3) |> Map.values() == [1, 2])

    # ошибки: неверный вид аргумента — ((:map, :f), v).
    assert(trap(Map.to_list(5)) == Error((:type_error, ((:map, :to_list), 5))))
    assert(trap(Map.values([1])) == Error((:type_error, ((:map, :values), [1]))))
    assert(trap(Map.from_list(5)) == Error((:type_error, ((:map, :from_list), 5))))
    assert(trap(Map.from_list([1])) == Error((:type_error, ((:map, :from_list), 1))))
    assert(trap(Map.from_list([(1, 2, 3)])) == Error((:type_error, ((:map, :from_list), (1, 2, 3)))))
    assert(trap(Map.filter(5, (k, v) -> true)) == Error((:type_error, ((:map, :filter), 5))))
    assert(trap(Map.update(5, 1, 0, x -> x)) == Error((:type_error, ((:map, :update), 5))))
    assert(trap(Map.filter(m, (k, v) -> 1)) == Error((:type_error, (:expected_bool, 1))))
`)
}
