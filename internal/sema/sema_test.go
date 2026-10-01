package sema_test

import (
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/ast"
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
		"send", "spawn", "spawn_linked", "spawn_watched", "link", "watch",
		"unwatch", "exit", "self", "make_ref", "mailbox_size",
		"register", "unregister", "whereis", "await", "reply",
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

func TestPipeBanPreludeActorPrimitive(t *testing.T) {
	wantErr(t, "module Main\nfn main() ->\n    x |> Prelude.send(:m)\n", `actor primitive "send"`)
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

// T-133, §5.1: имя, повторённое в одном паттерне связывания, — rebinding
// в одной области; диагностика указывает на повтор (line:col).
func TestLetBindPatternRepeatedName(t *testing.T) {
	cases := []struct {
		src       string
		line, col int
	}{
		{"module Main\nfn main() ->\n    (a, a) = (1, 2)\n    a\n", 3, 9},
		{"module Main\nfn main() ->\n    [h, ..h] = [1, 2]\n    h\n", 3, 5},
		{"module Main\nfn main() ->\n    Ok(x) as x = Ok(1)\n    x\n", 3, 5},
		{"module Main\nfn main() ->\n    y = 0\n    (y, z) = (1, 2)\n    z\n", 4, 6},
	}
	for _, tc := range cases {
		r := check(t, tc.src)
		found := false
		for _, d := range r.Diagnostics {
			if d.Severity == sema.SeverityError && strings.Contains(d.Message, "rebinding") {
				found = true
				if d.Line != tc.line || d.Col != tc.col {
					t.Errorf("%q: diag at %d:%d, want %d:%d", tc.src, d.Line, d.Col, tc.line, tc.col)
				}
			}
		}
		if !found {
			t.Errorf("%q: no rebinding error; got %v", tc.src, r.Diagnostics)
		}
	}
	wantOK(t, "module Main\nfn main() ->\n    (a, b) = (1, 2)\n    [c, ..d] = [a, b]\n    d\n")
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
    len = 1
    len
`, "shadows prelude")
}

func TestShadowingPreludeViaParamInfo(t *testing.T) {
	wantInfo(t, `module Main
fn f(print) ->
    print
`, "shadows prelude")
}

// Диагностика параметра указывает на сам параметр, а не на `fn`:
// у `fn f(a, print)` — строка 2, колонка 9.
func TestParamDiagnosticPosition(t *testing.T) {
	r := check(t, `module Main
fn f(a, print) ->
    print
fn g(..xs, b) -> b
`)
	var shadow, variadic bool
	for _, d := range r.Diagnostics {
		switch {
		case strings.Contains(d.Message, "shadows prelude"):
			shadow = true
			if d.Line != 2 || d.Col != 9 {
				t.Errorf("shadow info at %d:%d, want 2:9", d.Line, d.Col)
			}
		case strings.Contains(d.Message, "must be last"):
			variadic = true
			if d.Line != 4 || d.Col != 6 {
				t.Errorf("variadic error at %d:%d, want 4:6", d.Line, d.Col)
			}
		}
	}
	if !shadow || !variadic {
		t.Fatalf("diagnostics: %v", r.Diagnostics)
	}
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

// T-52: guard ветки recv — обычное выражение §F.3, видит связывания
// паттерна; trap и акторный примитив в pipe в нём запрещены.
func TestRecvGuardChecked(t *testing.T) {
	wantOK(t, `module Main
fn main() ->
    recv
        (:v, n) when n > 0 -> n
`)
	wantErr(t, `module Main
fn main() ->
    recv
        (:v, n) when trap(n) -> n
`, "trap is not allowed")
	wantErr(t, `module Main
fn main() ->
    recv
        (:v, n) when (n |> send(1)) -> n
`, "send")
}

// T-57: guard клоза fn (объявления и локальной fn) — обычное выражение
// §F.3, видит параметры клоза; trap и акторный примитив в pipe в нём
// запрещены.
func TestFnGuardChecked(t *testing.T) {
	wantOK(t, `module Main
fn f(n) when n > 0 -> n
fn g(x) ->
    fn h(n) when n > x -> n
    h(1)
`)
	wantErr(t, "module Main\nfn f(n) when trap(n) -> n\n", "trap is not allowed")
	wantErr(t, "module Main\nfn f(n) when (n |> send(1)) -> n\n", "send")
	wantErr(t, `module Main
fn g() ->
    fn h(n) when trap(n) -> n
    h(1)
`, "trap is not allowed")
	wantErr(t, `module Main
fn g() ->
    fn h(n) when (n |> send(1)) -> n
    h(1)
`, "send")
}

// ---- shadowing встроенных вариантов декларацией типа (§14.7, T-136) ----

func TestUserVariantShadowBuiltinInfo(t *testing.T) {
	src := "module Main\ntype Opt { Some(Int), None, Ok(Int), Error(Int) }\nfn main() -> 1\n"
	for _, name := range []string{"Some", "None", "Ok", "Error"} {
		wantInfo(t, src, "constructor `"+name+"` shadows built-in variant")
	}
	wantOK(t, src)
	wantInfo(t, "module Main\ntype Result { Good }\nfn main() -> 1\n", "type `Result` shadows built-in type")
	if r := check(t, "module Main\ntype Color { Red, Green }\nfn main() -> 1\n"); len(r.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %v", r.Diagnostics)
	}
}

// ---- именованный wildcard `_name` не связывается (§1.2, T-132) ----

func TestSemaUnderscoreNameNotBound(t *testing.T) {
	// Параметр функции.
	wantErr(t, `module Main
fn f(_unused) -> _unused
fn main() -> f(1)
`, "named wildcard")

	// Паттерн match.
	wantErr(t, `module Main
fn main() ->
    match (1)
        _msg -> _msg
`, "named wildcard")

	// Лямбда.
	wantErr(t, `module Main
fn main() ->
    g = fn (_x) -> _x
    g(1)
`, "named wildcard")

	// Именованный wildcard, который просто игнорируется (не читается в
	// теле) — не ошибка.
	wantOK(t, `module Main
fn f(_unused) -> 1
fn main() -> f(1)
`)
}

func checkNames(t *testing.T, src string, world *sema.World) *sema.Result {
	t.Helper()
	prog, err := parser.ParseProgram(parser.ModeModule, src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return sema.CheckNames(prog, world)
}

func TestNames(t *testing.T) {
	errAt := func(src, msg string, line, col int) {
		t.Helper()
		r := checkNames(t, src, nil)
		for _, d := range r.Diagnostics {
			if d.Severity == sema.SeverityError && d.Message == msg && d.Line == line && d.Col == col {
				return
			}
		}
		t.Fatalf("want %d:%d %q, got %v", line, col, msg, r.Diagnostics)
	}
	ok := func(src string) {
		t.Helper()
		if r := checkNames(t, src, nil); r.HasErrors() {
			t.Fatalf("unexpected: %v", r.Diagnostics)
		}
	}

	errAt("fn main() -> nope(1)\n", "undefined function nope/1", 1, 14)
	errAt("fn main() -> Json.nope(1)\n", "undefined function Json.nope/1", 1, 14)
	errAt("fn main() -> Util.nope()\n", "undefined function Util.nope/0", 1, 14)
	errAt("fn main() -> len(1, 2)\n", "undefined function len/2", 1, 14)
	errAt("fn f(a) -> a\nfn main() -> f()\n", "undefined function f/0", 2, 14)
	errAt("fn g(x, ..xs) -> x\nfn main() -> g()\n", "undefined function g/0", 2, 14)

	ok("fn main() -> print(1, 2, 3)\n")
	ok("fn g(x, ..xs) -> x\nfn main() -> g(1, 2)\n")
	ok("fn main() -> Json.encode(1)\n")
	ok("fn main() -> Json.encode(1, %{})\n")
	ok("fn main() -> Timer.send_after(0, self(), :tick)\n")
	ok("fn main() -> Timer.cancel(make_ref())\n")
	ok("fn main() -> Time.monotonic_ms() - Time.now()\n")
	ok("fn main() -> Telemetry.attach(:id, [:app], fn (e, m, meta) -> ())\n")
	ok("fn main() -> Telemetry.detach(:id)\n")
	ok("fn main() -> Telemetry.emit([:app], {}, {})\n")
	errAt("fn main() -> Telemetry.emit([:app])\n", "undefined function Telemetry.emit/1", 1, 14)
	errAt("fn main() -> Timer.send_after(0)\n", "undefined function Timer.send_after/1", 1, 14)
	errAt("fn main() -> Time.now(1)\n", "undefined function Time.now/1", 1, 14)
	ok("fn main() ->\n    f = len\n    f(1, 2)\n")
	ok("fn main() ->\n    xs = [1]\n    len(..xs)\n")
	ok("fn main() ->\n    fn a() -> b()\n    fn b() -> 1\n    a()\n")
	ok("fn add(a, b) -> a + b\nfn main() -> [1, 2] |> Enum.map(add) |> len()\n")
	ok("fn main() -> mailbox_size()\n")

	// spawn_behavior — голое имя модуля stdlib (§13.2, T-171): арность 2.
	ok("fn main() -> spawn_behavior(Behavior{ handlers: %{} }, %{})\n")
	errAt("fn main() -> spawn_behavior(1)\n", "undefined function spawn_behavior/1", 1, 14)
	ok("fn main() -> mailbox_size(self())\n")
	errAt("fn main() -> self(1)\n", "undefined function self/1", 1, 14)
	// Акторный примитив, затенённый fn модуля, — через Prelude (§11.5).
	ok("fn reply(from, v) -> Prelude.reply(from, make_ref(), v)\nfn main() -> reply(self(), 1)\n")
	ok("fn main() -> Prelude.send(Prelude.self(), :hi)\n")
	errAt("fn reply(from, v) -> reply(from, make_ref(), v)\n", "undefined function reply/3", 1, 22)
	errAt("fn main() -> Prelude.reply(self(), 1)\n", "undefined function Prelude.reply/2", 1, 14)

	util := checkNamesProg(t, "module Util\npub fn twice(x) -> scale(x)\nfn scale(x) -> x * 2\n")
	world := sema.NewWorld([]sema.Module{{Name: "Util", Prog: util}})
	mainSrc := "module Main\nimport Util\nfn main() -> Util.twice(3)\n"
	if r := checkNames(t, mainSrc, world); r.HasErrors() {
		t.Fatalf("Util.twice: %v", r.Diagnostics)
	}
	bad := "module Main\nimport Util\nfn main() -> Util.twice()\n"
	r := checkNames(t, bad, world)
	if !hasMsg(r, "undefined function Util.twice/0") {
		t.Fatalf("arity: %v", r.Diagnostics)
	}
	noimp := "module Main\nfn main() -> Util.twice(1)\n"
	r = checkNames(t, noimp, world)
	if !hasMsg(r, "module Util is not imported") {
		t.Fatalf("import: %v", r.Diagnostics)
	}

	// T-143: не-pub функция другого модуля — ошибка, в том числе через |>.
	for _, src := range []string{
		"module Main\nimport Util\nfn main() -> Util.scale(3)\n",
		"module Main\nimport Util\nfn main() -> 3 |> Util.scale()\n",
	} {
		if r := checkNames(t, src, world); !hasMsg(r, "scale/1 is private to Util") {
			t.Fatalf("private %q: %v", src, r.Diagnostics)
		}
	}
}

// T-144 (§7.6): ссылка `M.f` без вызова проверяется как вызов — модуль,
// функция, приватность (арность — из объявления); имя модуля без `.f`
// значением не является; акторный примитив — не значение.
func TestModuleFunctionRef(t *testing.T) {
	util := checkNamesProg(t, "module Util\npub fn twice(x) -> scale(x)\nfn scale(x) -> x * 2\nfn many(a, ..r) -> a\n")
	world := sema.NewWorld([]sema.Module{{Name: "Util", Prog: util}})
	errAt := func(src, msg string, line, col int) {
		t.Helper()
		r := checkNames(t, src, world)
		for _, d := range r.Diagnostics {
			if d.Severity == sema.SeverityError && d.Message == msg && d.Line == line && d.Col == col {
				return
			}
		}
		t.Fatalf("%q: want %d:%d %q, got %v", src, line, col, msg, r.Diagnostics)
	}
	ok := func(src string) {
		t.Helper()
		if r := checkNames(t, src, world); r.HasErrors() {
			t.Fatalf("%q: unexpected: %v", src, r.Diagnostics)
		}
	}

	ok("module Main\nimport Util\nfn main() -> Enum.map([1], Util.twice)\n")
	ok("module Main\nalias Util as U\nfn main() ->\n    f = U.twice\n    f(1)\n")
	ok("module Main\nfn main() -> Enum.map([[1]], Json.encode)\n")
	ok("module Main\nfn main() -> Enum.reverse\n")
	ok("module Main\ntype T { Json }\nfn main() -> Json\n")
	ok("module Main\nimport Util\nfn main() -> Util.twice(1) + len(Util.twice(2))\n")

	errAt("module Main\nimport Util\nfn main() -> Util.scale\n", "scale/1 is private to Util", 3, 14)
	errAt("module Main\nimport Util\nfn main() -> Util.many\n", "many/1.. is private to Util", 3, 14)
	errAt("module Main\nimport Util\nfn main() -> Util.nope\n", "undefined function Util.nope", 3, 14)
	errAt("module Main\nfn main() -> Json.nope\n", "undefined function Json.nope", 2, 14)
	errAt("module Main\nfn main() -> Util.twice\n", "module Util is not imported", 2, 14)
	errAt("module Main\nimport Util\nfn main() -> Util\n", "module Util is not a value (§7.6)", 3, 14)
	errAt("module Main\nfn main() -> print(Json)\n", "module Json is not a value (§7.6)", 2, 20)
	errAt("module Main\nfn main() -> Prelude.send\n", "actor primitive Prelude.send cannot be used as a value (§12.6)", 2, 14)

	// Акторный примитив ловит и Check (REPL); остальное — только проход имён.
	for src, want := range map[string]bool{
		"fn main() -> Prelude.self\n": true,
		"fn main() -> Util.nope\n":    false,
		"fn main() -> print(Json)\n":  false,
	} {
		if got := sema.Check(checkNamesProg(t, src)).HasErrors(); got != want {
			t.Fatalf("Check %q: errors = %v, want %v", src, got, want)
		}
	}
}

// T-143: клозы одной функции с pub и без — ошибка (§11.2).
func TestPubMixedClauses(t *testing.T) {
	prog := checkNamesProg(t, "module M\npub fn f(0) -> 0\nfn f(n) -> n\n")
	r := sema.Check(prog)
	if !hasMsg(r, "clauses of f mix pub and non-pub (§11.2)") {
		t.Fatalf("mixed: %v", r.Diagnostics)
	}
	for _, src := range []string{
		"module M\npub fn f(0) -> 0\npub fn f(n) -> n\n",
		"module M\nfn f(0) -> 0\nfn f(n) -> n\n",
	} {
		if r := sema.Check(checkNamesProg(t, src)); r.HasErrors() {
			t.Fatalf("%q: %v", src, r.Diagnostics)
		}
	}
}

func checkNamesProg(t *testing.T, src string) *ast.Program {
	t.Helper()
	prog, err := parser.ParseProgram(parser.ModeModule, src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return prog
}

func hasMsg(r *sema.Result, msg string) bool {
	for _, d := range r.Diagnostics {
		if d.Message == msg {
			return true
		}
	}
	return false
}

// T-242 (§F.3): ссылка на имя, которое не связано в области и не является
// функцией своего модуля, прелюдии или видимого модуля, — ошибка прохода
// имён `undefined name x`. Check (REPL) и CheckNamesSession (модули сессии,
// ввод -i) имена не проверяют: там это ошибка рантайма (§11.4).
func TestUnboundName(t *testing.T) {
	errAt := func(src, name string, line, col int) {
		t.Helper()
		r := checkNames(t, src, nil)
		for _, d := range r.Diagnostics {
			if d.Severity == sema.SeverityError && d.Message == "undefined name "+name && d.Line == line && d.Col == col {
				return
			}
		}
		t.Fatalf("%q: want %d:%d undefined name %s, got %v", src, line, col, name, r.Diagnostics)
	}
	ok := func(src string) {
		t.Helper()
		if r := checkNames(t, src, nil); r.HasErrors() {
			t.Fatalf("%q: unexpected: %v", src, r.Diagnostics)
		}
	}

	errAt("module Main\nfn main() ->\n    print(y)\n", "y", 3, 11)
	errAt("module Main\nfn main() ->\n    f = x -> x + zz\n    f(1)\n", "zz", 3, 18)
	// Тело локальной fn не видит имя, связанное ниже в блоке.
	errAt("module Main\nfn main() ->\n    fn g() -> z\n    z = 1\n    g()\n", "z", 3, 15)
	// Правая часть связывания не видит своё имя.
	errAt("module Main\nfn main() ->\n    y = y + 1\n    y\n", "y", 3, 9)
	errAt("module Main\nfn main() ->\n    with\n        Ok(a) <- Ok(a)\n        a\n", "a", 4, 21)
	// Имя ветки не видно после неё; параметр — вне своей fn.
	errAt("module Main\nfn main() ->\n    match 1\n        v -> v\n    v\n", "v", 5, 5)
	errAt("module Main\nfn f(p) -> p\nfn main() -> p\n", "p", 3, 14)
	errAt("module Main\nfn main() -> (1, Nope)\n", "Nope", 2, 18)
	errAt("module Main\nfn main() -> { a: b }\n", "b", 2, 19)

	// Охватывающая область, параметры, паттерны, локальные fn.
	ok("module Main\nfn main() ->\n    x = 1\n    f = () -> x + 1\n    if x > 0\n        y = x\n        y + f()\n    else\n        x\n")
	ok("module Main\nfn f(a, (b, c), [h, ..t], ..rest) -> (a, b, c, h, t, rest)\nfn main() -> f(1, (2, 3), [4], 5)\n")
	ok("module Main\nfn main() ->\n    g = (a, b) -> a + b\n    h = fn ((p, q)) -> p + q\n    k = () -> g(1, 2)\n    k() + h((1, 2))\n")
	ok("module Main\nfn main() ->\n    (a, [b, ..c]) = (1, [2, 3])\n    %{ :k => v } = %{ :k => 4 }\n    (a, b, c, v)\n")
	ok("module Main\nfn main() ->\n    match Some(1)\n        Some(x) as s -> (x, s)\n        _ -> 0\n")
	ok("module Main\nfn main() ->\n    recv\n        (:m, x) when x > 0 -> x\n    else other\n        other\n")
	ok("module Main\nfn main() ->\n    with\n        Ok(a) <- Ok(1)\n        Ok(b) <- Ok(a)\n        a + b\n    else\n        e -> e\n")
	ok("module Main\nfn main() ->\n    fn a() -> b\n    fn b() -> 1\n    a()\n")
	ok("module Main\nfn main() ->\n    fn loop(n) -> if n == 0 then n else loop(n - 1)\n    f = loop\n    f(3)\n")
	ok("module Main\nfn main() ->\n    r = trap\n        x = 1\n        x + 1\n    r\n")
	// ensure видит имена тела trap (компилируется в его области).
	ok("module Main\nfn main() ->\n    r = trap\n        f = 1\n        ensure print(f)\n        f + 1\n    r\n")
	errAt("module Main\nfn main() ->\n    r = trap\n        f = 1\n        f + 1\n    f\n", "f", 6, 5)
	// Функции модуля, прелюдии, конструкторы, видимые модули.
	ok("module Main\ntype T { A, B(Int) }\nfn g(x) -> x\nfn main() -> (g, len, Enum.map, print, Some, None, Ok, Error, A, B, Json.encode, Enum.reverse)\n")
	ok("module Main\ntype User { name: Str }\nfn main() ->\n    n = \"a\"\n    (User{ name: n }, { name: n })\n")
	ok("module Main\nfn main() -> spawn_behavior\n")
	// Акторный примитив, затенённый привязкой, параметром или паттерном, —
	// связанное имя (T-261).
	ok("module Main\nfn main() ->\n    send = (a, b) -> (:mine, a, b)\n    send(self(), 2)\n")
	ok("module Main\nfn call(spawn) -> spawn(1)\nfn main() -> call(x -> x + 1)\n")
	ok("module Main\nfn main() ->\n    (reply, _) = (x -> x * 2, 0)\n    reply(21)\n")
	ok("module Main\nfn main() ->\n    f = self\n    f\n")

	// REPL и модули сессии (-i) имена не проверяют.
	src := "module Main\nfn main() ->\n    print(y)\n"
	if r := sema.Check(checkNamesProg(t, src)); r.HasErrors() {
		t.Fatalf("Check: %v", r.Diagnostics)
	}
	if r := sema.CheckNamesSession(checkNamesProg(t, src), nil); r.HasErrors() {
		t.Fatalf("CheckNamesSession: %v", r.Diagnostics)
	}
}
