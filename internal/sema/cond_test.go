package sema_test

import (
	"testing"

	"github.com/it1ro/brig-lang/internal/sema"
)

// T-253: `cond` без последней ветки `true ->` — info с позицией
// ключевого слова `cond` (§8.4); с `true ->` — без диагностик.
func TestCondLastBranchInfo(t *testing.T) {
	r := check(t, `module Main
fn f(n) ->
    cond
        n < 0 -> :neg
        n > 0 -> :pos
`)
	var got []sema.Diagnostic
	for _, d := range r.Diagnostics {
		if d.Severity == sema.SeverityInfo {
			got = append(got, d)
		}
	}
	if len(got) != 1 || got[0].Line != 3 || got[0].Col != 5 {
		t.Fatalf("want one info at 3:5, got %v", r.Diagnostics)
	}
	wantInfo(t, `module Main
fn f(n) ->
    cond
        n < 0 -> :neg
`, "last branch is not `true ->`")

	r = check(t, `module Main
fn f(n) ->
    cond
        n < 0 -> :neg
        true -> :pos
`)
	if len(r.Diagnostics) != 0 {
		t.Fatalf("want no diagnostics, got %v", r.Diagnostics)
	}
}
