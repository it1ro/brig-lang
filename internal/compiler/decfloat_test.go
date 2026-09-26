package compiler_test

import "testing"

// I-F8 / T-86 (#111, решение #43 п.3, вариант A): Decimal×Float — ловимый
// :type_error в операторах == и <; в паттернах и ключах Map/Set/INDEX/set/
// Map.* — разные виды, разные значения (false без ошибки).
// Восстановление пробной программы p/v6_mixed.brig из AUDIT_REPORT.md.
func TestDecimalFloatMixed(t *testing.T) {
	cases := []struct{ name, body string }{
		{"eq", `r = trap(dec"1" == 1.0)
    assert(r == Error((:type_error, (:eq, (dec"1", 1.0)))))`},
		{"eq_swapped", `r = trap(1.0 == dec"1")
    assert(r == Error((:type_error, (:eq, (1.0, dec"1")))))`},
		{"neq", `r = trap(dec"1" != 1.5)
    assert(r == Error((:type_error, (:eq, (dec"1", 1.5)))))`},
		{"lt", `r = trap(dec"1" < 1.0)
    assert(r == Error((:type_error, (:compare, (dec"1", 1.0)))))`},
		{"lt_swapped", `r = trap(1.0 < dec"1")
    assert(r == Error((:type_error, (:compare, (1.0, dec"1")))))`},
		{"index_key", `m = %{dec"1" => :d}
    assert(m[1.0] == None)
    assert(m[dec"1"] == Some(:d))
    n = %{1.0 => :f}
    assert(n[dec"1"] == None)
    assert(n[1.0] == Some(:f))`},
		{"set", `assert(len(set(dec"1", 1.0)) == 2)
    assert(len(set(1.0, dec"1")) == 2)
    assert(len(set(dec"1", dec"1")) == 1)`},
		{"index_key_dec_int", `m = %{dec"1" => :d}
    assert(m[1] == None)
    n = %{1 => :i}
    assert(n[dec"1"] == None)
    k = %{(dec"1", 2) => :t}
    assert(k[(1, 2)] == None)
    assert(k[(dec"1", 2)] == Some(:t))`},
		{"set_dec_int", `assert(len(set(dec"1", 1)) == 2)
    assert(len(set(1, dec"1")) == 2)
    assert(len(set(1, 1.0, dec"1")) == 2)
    assert(len(set(dec"1", dec"1.0")) == 1)`},
		{"map_dec_int", `m = %{dec"1" => :d, 1 => :i}
    assert(len(m) == 2)
    assert(Map.get(m, dec"1") == Some(:d))
    assert(Map.get(m, 1) == Some(:i))
    assert(len(Map.put(%{dec"1" => :d}, 1, :i)) == 2)
    assert(len(Map.remove(%{dec"1" => :d}, 1)) == 1)
    assert(len(Map.remove(%{1 => :i}, dec"1")) == 1)`},
		{"operators_dec_int_unchanged", `assert(dec"1" == 1)
    assert(dec"1" < 2)`},
		{"map_get", `m = %{dec"1" => :d, 1.0 => :f}
    assert(len(m) == 2)
    assert(Map.get(m, dec"1") == Some(:d))
    assert(Map.get(m, 1.0) == Some(:f))
    assert(Map.get(%{dec"1" => :d}, 1.0) == None)
    assert(Map.get(%{1.0 => :f}, dec"1") == None)`},
		{"map_put", `m = Map.put(%{dec"1" => :d}, 1.0, :f)
    assert(len(m) == 2)
    assert(Map.get(m, dec"1") == Some(:d))
    k = Map.put(%{1.0 => :f}, dec"1", :d)
    assert(len(k) == 2)
    assert(Map.get(k, 1.0) == Some(:f))`},
		{"map_remove", `m = %{dec"1" => :d, 1.0 => :f}
    assert(len(Map.remove(m, dec"1")) == 1)
    assert(Map.get(Map.remove(m, dec"1"), 1.0) == Some(:f))
    assert(len(Map.remove(%{dec"1" => :d}, 1.0)) == 1)
    assert(len(Map.remove(%{1.0 => :f}, dec"1")) == 1)`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := runModuleErr(t, "module Main\nfn main() ->\n    "+c.body+"\n"); err != nil {
				t.Fatalf("%v", err)
			}
		})
	}
}

func TestDecimalFloatPattern(t *testing.T) {
	cases := []struct{ name, msg, pat string }{
		{"lit-dec-vs-float", `dec"1"`, "1.0"},
		{"lit-float-vs-dec", "1.0", `dec"1"`},
		{"map-key-dec-vs-float", `%{dec"1" => :a}`, `%{1.0 => _}`},
		{"map-key-float-vs-dec-lit", `%{1.0 => :a}`, `%{dec"1" => _}`},
		{"tuple-nested", `(dec"1", 2)`, "(1.0, 2)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := "module Main\nfn main() ->\n    send(self(), " + c.msg + ")\n" +
				"    r = recv\n        " + c.pat + " -> :hit\n        _ -> :other\n" +
				"    assert(r == :other)\n"
			if err := runModuleErr(t, src); err != nil {
				t.Errorf("%v", err)
			}
		})
	}
}

func TestDecimalMapPatternKeyHit(t *testing.T) {
	src := "module Main\nfn main() ->\n    send(self(), %{dec\"1\" => :a})\n" +
		"    r = recv\n        %{dec\"1\" => _} -> :hit\n        _ -> :other\n" +
		"    assert(r == :hit)\n"
	if err := runModuleErr(t, src); err != nil {
		t.Errorf("%v", err)
	}
}
