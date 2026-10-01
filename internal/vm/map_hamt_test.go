package vm_test

import "testing"

// T-272: Map и Set на HAMT — ключи по KeyEqual, порядок обхода — term order.
func TestMapHamtSemantics(t *testing.T) {
	runModuleSync(t, `module Main
fn fill(i, m) -> if i == 0 then m else fill(i - 1, Map.put(m, i, i * 2))

fn drop(i, m) -> if i == 0 then m else drop(i - 1, Map.remove(m, i))

fn main() ->
    m = fill(2000, %{})
    assert(len(m) == 2000)
    assert(Map.get(m, 1234) == Some(2468))
    assert(Map.get(m, 1234.0) == Some(2468))
    assert(Map.get(m, 2001) == None)
    half = drop(1000, m)
    assert(len(half) == 1000)
    assert(Map.get(half, 1000) == None)
    assert(Map.get(half, 1001) == Some(2002))
    assert(len(m) == 2000)
    assert(Map.keys(%{3 => :c, 1 => :a, 2 => :b}) == [1, 2, 3])
    assert(%{"k" => 1, "j" => 2} == %{"j" => 2, "k" => 1})
    assert(Map.get(%{%{1 => 2, 3 => 4} => :v}, %{3 => 4, 1.0 => 2}) == Some(:v))
    assert(Map.get(%{set(1, 2) => :s}, set(2, 1)) == Some(:s))
    assert(%{ ..%{"a" => 1, "b" => 2}, "b" => 3, "c" => 4 } == %{"a" => 1, "b" => 3, "c" => 4})
`)
}

func TestMapPatternKeyKind(t *testing.T) {
	runModuleSync(t, `module Main
fn pick(m) ->
    match m
        %{ 1 => v } -> v
        _ -> :none

fn main() ->
    assert(pick(%{1 => :a}) == :a)
    assert(pick(%{1.0 => :a}) == :none)
    assert(pick(%{2 => :a}) == :none)
`)
}

func TestSetPrintsInTermOrder(t *testing.T) {
	got := runTimerSrc(t, `module Main
fn main() ->
    set(3, 1, 2, 1.0)
`)
	if got.Inspect() != `set(1, 2, 3)` {
		t.Fatalf("Inspect = %s, want set(1, 2, 3)", got.Inspect())
	}
}
