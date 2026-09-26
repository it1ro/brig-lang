package compiler_test

import "testing"

// T-84 (#109): паттерны с числовым литералом — строго по виду.
func TestLiteralPatternExactKinds(t *testing.T) {
	cases := []struct{ name, msg, pat string }{
		{"float-vs-int", "1.0", "1"},
		{"int-vs-float", "1", "1.0"},
		{"decimal-vs-int", `dec"1"`, "1"},
		{"int-vs-decimal", "1", `dec"1"`},
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

func TestLiteralPatternSameKindMatches(t *testing.T) {
	src := "module Main\nfn main() ->\n    send(self(), 1.0)\n" +
		"    r = recv\n        1.0 -> :hit\n        _ -> :other\n    assert(r == :hit)\n"
	if err := runModuleErr(t, src); err != nil {
		t.Errorf("%v", err)
	}
}

// Ключи Map-паттерна сопоставляются строго по виду (§4.8).
func TestMapPatternKeyExactKind(t *testing.T) {
	src := "module Main\nfn main() ->\n    send(self(), %{1.0 => :a})\n" +
		"    r = recv\n        %{1 => _} -> :hit\n        _ -> :other\n    assert(r == :other)\n"
	if err := runModuleErr(t, src); err != nil {
		t.Errorf("%v", err)
	}
}
