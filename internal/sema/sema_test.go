package sema_test

import (
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/parser"
	"github.com/it1ro/brig-lang/internal/sema"
)

func check(t *testing.T, src string) *sema.Result {
	t.Helper()
	prog, err := parser.ParseProgram(parser.ModeModule, src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return sema.Check(prog)
}

func wantErr(t *testing.T, src, substr string) {
	t.Helper()
	r := check(t, src)
	if !r.HasErrors() {
		t.Fatalf("expected error containing %q, got none; diags=%v", substr, r.Diagnostics)
	}
	for _, d := range r.Diagnostics {
		if strings.Contains(d.Message, substr) {
			return
		}
	}
	t.Fatalf("no diagnostic contains %q; got: %v", substr, r.Diagnostics)
}

func wantOK(t *testing.T, src string) {
	t.Helper()
	r := check(t, src)
	if r.HasErrors() {
		t.Fatalf("unexpected errors: %v", r.Diagnostics)
	}
}

func wantInfo(t *testing.T, src, substr string) {
	t.Helper()
	r := check(t, src)
	for _, d := range r.Diagnostics {
		if d.Severity == sema.SeverityInfo && strings.Contains(d.Message, substr) {
			return
		}
	}
	t.Fatalf("no info containing %q; got: %v", substr, r.Diagnostics)
}

// ---- pipe-ban actor primitives (§7.5) ----

func TestPipeBanActorPrimitives(t *testing.T) {
	names := []string{
		"send", "spawn", "spawn_linked", "link", "watch",
		"unwatch", "self", "make_ref", "mailbox_size",
	}
	for _, name := range names {
		src := "module Main\nfn main() ->\n    x |> " + name + "\n"
		wantErr(t, src, "not allowed as pipe RHS")
	}
}

func TestPipeAllowsNormalFunctions(t *testing.T) {
	wantOK(t, "module Main\nfn main() ->\n    xs |> map(f)\n")
}

func TestPipeAllowsQualifiedModule(t *testing.T) {
	wantOK(t, "module Main\nfn main() ->\n    x |> Foo.send\n")
}

func TestPipeChain(t *testing.T) {
	wantErr(t,
		"module Main\nfn main() ->\n    xs |> map(f) |> send\n",
		"not allowed as pipe RHS")
}

// ---- variadic must be last (§6.3) ----

func TestVariadicMustBeLast(t *testing.T) {
	wantErr(t, "module Main\nfn f(..args, x) -> x\n", "must be last")
}

func TestVariadicLastOK(t *testing.T) {
	wantOK(t, "module Main\nfn f(x, ..args) -> x\n")
}

func TestVariadicOnlyParam(t *testing.T) {
	wantOK(t, "module Main\nfn f(..args) -> args\n")
}

func TestVariadicInLambda(t *testing.T) {
	wantErr(t,
		"module Main\nfn main() ->\n    g = fn (..a, x) ->\n        x\n    g\n",
		"must be last")
}

func TestVariadicInLocalFn(t *testing.T) {
	wantErr(t,
		"module Main\nfn main() ->\n    fn inner(..a, b) -> b\n    inner\n",
		"must be last")
}

// ---- trap position (§10.2) ----

func TestTrapInArgPosition(t *testing.T) {
	wantErr(t,
		"module Main\nfn main() ->\n    print(trap(1 + 1))\n",
		"trap is not allowed")
}

func TestTrapInListElement(t *testing.T) {
	wantErr(t,
		"module Main\nfn main() ->\n    xs = [1, trap(2), 3]\n",
		"trap is not allowed")
}

func TestTrapInTupleElement(t *testing.T) {
	wantErr(t,
		"module Main\nfn main() ->\n    t = (1, trap(2))\n",
		"trap is not allowed")
}

func TestTrapInMapValue(t *testing.T) {
	wantErr(t,
		`module Main
fn main() ->
    m = %{ "k" => trap(1) }
`,
		"trap is not allowed")
}

func TestTrapInRecordField(t *testing.T) {
	wantErr(t,
		`module Main
fn main() ->
    r = { x: trap(1) }
`,
		"trap is not allowed")
}

// TestTrapPositionRestricted — полная проверка позиции trap (§10.2 / I-F15):
// trap запрещён внутри операторов и в ветках if; разрешён только как
// RHS let_bind или отдельный expr_stmt.
func TestTrapPositionRestricted(t *testing.T) {
	wantErr(t,
		"module Main\nfn main() ->\n    x = 1 + trap(y)\n    x\n",
		"trap is not allowed")
	wantErr(t,
		"module Main\nfn main() ->\n    if c then trap(y) else z\n",
		"trap is not allowed")
	wantOK(t, "module Main\nfn main() ->\n    x = trap(y)\n    x\n")
	wantOK(t, "module Main\nfn main() ->\n    trap(y)\n")
}

func TestTrapAsLetRHS(t *testing.T) {
	wantOK(t, "module Main\nfn main() ->\n    x = trap(1 + 1)\n    x\n")
}

func TestTrapAsExprStmt(t *testing.T) {
	wantOK(t, "module Main\nfn main() ->\n    trap(1 + 1)\n")
}

func TestTrapBlockAsLetRHS(t *testing.T) {
	wantOK(t, `module Main
fn main() ->
    result = trap
        x = 1
        x + 2
    result
`)
}

func TestTrapNestedInTrapBody(t *testing.T) {
	wantOK(t, `module Main
fn main() ->
    outer = trap
        inner = trap(1)
        inner
    outer
`)
}

func TestTrapInRecvBranchBody(t *testing.T) {
	// §10.2 + §10.2 «trap внутри ветки recv»: trap — expr_stmt в блоке ветки,
	// а не голый expression в `-> expr` (как в if-then).
	wantOK(t, `module Main
fn main() ->
    x = recv
        :stop ->
            trap(1)
    x
`)
	wantErr(t, `module Main
fn main() ->
    x = recv
        :stop -> trap(1)
    x
`, "trap is not allowed")
}

// ---- rebinding (§6.6, principle #12) ----

func TestRebindingSameScope(t *testing.T) {
	wantErr(t, `module Main
fn main() ->
    x = 1
    x = 2
    x
`, "rebinding")
}

func TestRebindingSameScopeAsParam(t *testing.T) {
	wantErr(t, `module Main
fn f(x) ->
    x = 1
    x
`, "rebinding")
}

func TestShadowingNestedBlockOK(t *testing.T) {
	// Внутренняя область может затенять внешнее имя.
	wantOK(t, `module Main
fn main() ->
    x = 1
    f = fn (x) ->
        x + 1
    f(2)
`)
}

// TestShadowingInLambdaBodyOK — полная лямбда `fn () ->` с блочным
// телом создаёт вложенную область; `y = 2` внутри — не конфликт
// с внешним `x`.
//
// Короткая лямбда `() ->` тут не подходит: по §6.2 её тело —
// одно выражение до NEWLINE, блочная форма для неё не существует.
func TestShadowingInLambdaBodyOK(t *testing.T) {
	wantOK(t, `module Main
fn main() ->
    x = 1
    g = fn () ->
        y = 2
        y
    g()
`)
}

func TestDistinctScopesNoConflict(t *testing.T) {
	wantOK(t, `module Main
fn f() -> 1
fn g() -> 2
fn main() ->
    a = f()
    b = g()
    a + b
`)
}

// ---- local fn position (§6.5) ----

func TestLocalFnAtStartOK(t *testing.T) {
	wantOK(t, `module Main
fn main() ->
    fn add(x, y) -> x + y
    print(add(1, 2))
`)
}

func TestLocalFnAfterStatementError(t *testing.T) {
	wantErr(t, `module Main
fn main() ->
    print("first")
    fn add(x, y) -> x + y
    add(1, 2)
`, "must appear at the start")
}

func TestLocalFnMultipleAtStartOK(t *testing.T) {
	wantOK(t, `module Main
fn main() ->
    fn a() -> 1
    fn b() -> 2
    a() + b()
`)
}

// ---- shadowing prelude (info) ----
//
// Проверяем только LOWER-имена, которые биндятся через let/params:
// Upper-имена прелюдии (`Some`, `None`, `Ok`, `Error`) в паттернах
// разбираются как constructor-паттерны (см. §14.2 — конструктор без
// аргументов есть значение, но не identifier-паттерн), поэтому
// через `Some = 1` их затенять нельзя. Shadowing встроенных типов —
// отдельная фича §14.7, реализуется через type-decls (не через scope
// let-привязок), вне рамок этой заготовки sema.

func TestShadowingPreludeInfo(t *testing.T) {
	wantInfo(t, `module Main
fn main() ->
    map = 1
    map
`, "shadows prelude")
}

func TestShadowingPreludeViaParamInfo(t *testing.T) {
	wantInfo(t, `module Main
fn f(print) ->
    print
`, "shadows prelude")
}

func TestNoShadowNoInfo(t *testing.T) {
	r := check(t, `module Main
fn main() ->
    x = 1
    x
`)
	for _, d := range r.Diagnostics {
		if strings.Contains(d.Message, "shadows prelude") {
			t.Fatalf("unexpected info: %v", d)
		}
	}
}

// ---- комбинированные сценарии ----

func TestEmptyModule(t *testing.T) {
	wantOK(t, "module Main\n")
}

func TestCleanModule(t *testing.T) {
	wantOK(t, `module Main
fn sum(x, ..rest) -> x
fn classify(0) -> "zero"
fn classify(n) -> "many"
fn main() ->
    xs = [1, 2, 3]
    xs |> map(f)
`)
}

// TestInterpExprsVisibleToSema — T-53 / S-F1: выражение внутри \(...)
// обходится sema. Имя, встречающееся только в интерполяции, видно
// проверке позиции trap (§10.2).
func TestInterpExprsVisibleToSema(t *testing.T) {
	wantErr(t, `module Main
fn main() ->
    s = "hello \(trap(1))"
    s
`, "trap is not allowed")
}
