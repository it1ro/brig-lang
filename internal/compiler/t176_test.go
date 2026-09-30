package compiler_test

import "testing"

// T-176: Json.at(v, path) и Map.get_or(m, k, default) (L16).
func TestJsonAtMapGetOr(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    v = %{"a" => [1, %{"b" => 2}]}
    assert(Json.at(v, ["a", 1, "b"]) == Some(2))
    assert(Json.at(v, []) == Some(v))
    assert(Json.at(v, ["a", 0]) == Some(1))
    assert(Json.at(v, ["x"]) == None)
    assert(Json.at(v, ["a", 2]) == None)
    assert(Json.at(v, ["a", -1]) == None)
    assert(Json.at(v, [0]) == None)
    assert(Json.at(v, ["a", "b"]) == None)
    assert(Json.at(v, ["a", 0, "b"]) == None)
    assert(Json.at(%{:a => 1}, [:a]) == None)
    assert(Json.at(%[1], [0]) == None)
    ok = Json.decode("{\"xs\": [10, 20]}")
    assert(Json.at(ok, ["xs", 1]) == None)
    Ok(d) = ok
    assert(Json.at(d, ["xs", 1]) == Some(20))
    assert(v |> Json.at(["a", 1, "b"]) == Some(2))
    r = trap(Json.at(v, "a"))
    assert(r == Error((:type_error, ((:json, :at), "a"))))

    assert(Map.get_or(%{}, :k, 0) == 0)
    assert(Map.get_or(%{:k => 1}, :k, 0) == 1)
    assert(Map.get_or(%{1 => :int}, 1.0, :none) == :int)
    assert(Map.get_or(%{"1" => :str}, 1, :none) == :none)
    assert(%{"a" => 1} |> Map.get_or("a", 0) == 1)
    r2 = trap(Map.get_or(5, :k, 0))
    assert(r2 == Error((:type_error, ((:map, :get_or), 5))))
`)
}
