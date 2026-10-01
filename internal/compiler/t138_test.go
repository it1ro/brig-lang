package compiler_test

import "testing"

// T-138 (#196): блочные конструкции внутри скобок (§2.5, §D.6) исполняются.
func TestRunMiniBlockExamples(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    ys = Enum.map([1, 2], fn (x) ->
        y = x * 2
        y + 1)
    assert(ys == [3, 5])
    zs = Enum.map([1, 2, 3], fn (x) ->
        match x
            1 -> :one
            _ -> :many
    )
    assert(zs == [:one, :many, :many])
    v = 2
    r = [match v
        1 -> :a
        _ -> :b
    , 7]
    assert(r == [:b, 7])
    c = [if v == 2
        :yes
    else
        :no]
    assert(c == [:yes])
    s = Enum.fold([1, 2, 3], 0, fn (acc, x) ->
        t = acc + x
        t * 1, )
    assert(s == 6)
    h = %{
        :get => fn (st, k) ->
            match Map.get(st, k)
                Some(v) -> (:ok, v)
                None -> (:error, :not_found)
        :put => fn (st, k, val) ->
            (:ok, Map.put(st, k, val))
    }
    Some(get_h) = Map.get(h, :get)
    Some(put_h) = Map.get(h, :put)
    assert(get_h(%{:a => 1}, :a) == (:ok, 1))
    assert(get_h(%{}, :a) == (:error, :not_found))
    assert(put_h(%{}, :a, 2) == (:ok, %{:a => 2}))
`)
}
