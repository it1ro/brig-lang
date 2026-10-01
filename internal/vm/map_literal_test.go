package vm_test

import "testing"

// T-241 (G-16): литерал Map схлопывает повторные ключи: правый побеждает
// (§5.2), 1 и 1.0 — один ключ (§4.8), печать — по term order (§7.4).
func TestMapLiteralDuplicateKeys(t *testing.T) {
	got := runTimerSrc(t, `module Main
fn main() ->
    %{"a" => 1, "a" => 2}
`)
	if got.Inspect() != `%{"a" => 2}` {
		t.Fatalf("Inspect = %s, want %%{\"a\" => 2}", got.Inspect())
	}
	if got.Len() != 1 {
		t.Fatalf("len = %d, want 1", got.Len())
	}

	runModuleSync(t, `module Main
fn main() ->
    assert(%{"a" => 1, "a" => 2} == %{"a" => 2})
    assert(len(%{"a" => 1, "a" => 2}) == 1)
    m = %{1 => :int, 1.0 => :float}
    assert(len(m) == 1)
    assert(Map.get(m, 1) == Some(:float))
    assert(m == Map.put(Map.put(%{}, 1, :int), 1.0, :float))
`)

	order := runTimerSrc(t, `module Main
fn main() ->
    %{"a" => 1, "b" => 2, "a" => 3}
`)
	if order.Inspect() != `%{"a" => 3, "b" => 2}` {
		t.Fatalf("order Inspect = %s", order.Inspect())
	}
}
