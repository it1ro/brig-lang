package parser

import (
	"strings"
	"testing"
)

func mustParse(t *testing.T, mode Mode, src string) {
	t.Helper()
	if err := Parse(mode, src); err != nil {
		t.Fatalf("Parse(%v, %q) failed: %v", mode, src, err)
	}
}

func mustFail(t *testing.T, mode Mode, src, wantSub string) {
	t.Helper()
	err := Parse(mode, src)
	if err == nil {
		t.Fatalf("Parse(%v, %q): want error %q, got nil", mode, src, wantSub)
	}
	if wantSub != "" && !strings.Contains(err.Error(), wantSub) {
		t.Fatalf("Parse(%v, %q): error %q does not contain %q", mode, src, err, wantSub)
	}
}

// ---- Module-mode ----

func TestParseModuleModeRejectsTopLevelLet(t *testing.T) {
	mustFail(t, ModeModule, "x = 1\n", "module top-level")
}

func TestParseModuleModeRejectsTopLevelExpr(t *testing.T) {
	mustFail(t, ModeModule, "1 + 1\n", "module top-level")
}

func TestParseModuleHeaderOnly(t *testing.T) {
	mustParse(t, ModeModule, "module Http.Client\n")
}

func TestParseModuleWithImportsAndAlias(t *testing.T) {
	src := `module Http.Client
import Json
alias Http.Client as Http
`
	mustParse(t, ModeModule, src)
}

func TestParseModuleTypeVariant(t *testing.T) {
	src := `module X
type Color { Red, Green, Blue }
type Result<T, E> { Ok(T), Error(E) }
`
	mustParse(t, ModeModule, src)
}

func TestParseModuleTypeRecord(t *testing.T) {
	mustParse(t, ModeModule, "module X\ntype User { id: Int, name: Str }\n")
}

func TestParseModuleTypeAlias(t *testing.T) {
	mustParse(t, ModeModule, "module X\ntype UserId = Int\n")
	mustParse(t, ModeModule, "module X\ntype Callback = (Int, Str) -> Bool\n")
}

func TestParseModuleFnSingleClause(t *testing.T) {
	src := `module M
fn main() ->
    print("hi")
`
	mustParse(t, ModeModule, src)
}

func TestParseModuleFnMultiClause(t *testing.T) {
	src := `module M
fn classify(0) -> "zero"
fn classify(1) -> "one"
fn classify(n) -> "many"
`
	mustParse(t, ModeModule, src)
}

func TestParseModuleFnGuard(t *testing.T) {
	src := `module M
fn classify(n) when n > 0 -> "pos"
fn classify(0) -> "zero"
fn classify(n) -> "neg"
`
	mustParse(t, ModeModule, src)
}

func TestParseModuleFnVariadic(t *testing.T) {
	mustParse(t, ModeModule, "module M\nfn sum(x, ..rest) -> x\n")
}

// ---- Repl-mode ----

func TestParseReplAllowsTopLevelExpr(t *testing.T) {
	mustParse(t, ModeRepl, "1 + 1\n")
}

func TestParseReplAllowsTopLevelLet(t *testing.T) {
	mustParse(t, ModeRepl, "x = 1\n")
}

func TestParseReplPipe(t *testing.T) {
	mustParse(t, ModeRepl, "1 to 10 |> list\n")
}

// ---- Expressions ----

func TestParsePrecedence(t *testing.T) {
	cases := []string{
		"1 + 2 * 3\n",
		"1 + 2 * 3 ** 4\n",
		"-x ** 2\n",
		"2 ** 3 ** 2\n",
		"a and b or c\n",
		"not a and b\n",
		"1 to 10 |> list\n",
		"a |> f == c\n",
		"xs |> M.f(1, 2)\n",
	}
	for _, src := range cases {
		mustParse(t, ModeRepl, src)
	}
}

func TestParseNonAssocRejected(t *testing.T) {
	mustFail(t, ModeRepl, "a == b == c\n", "non-associative")
	mustFail(t, ModeRepl, "1 to 2 to 3\n", "non-associative")
}

func TestParseLambdas(t *testing.T) {
	cases := []string{
		"pow = x -> x ** 2\n",
		"f = () -> 42\n",
		"g = fn (x, y) ->\n    x + y\n",
		"nums |> map(x -> x * 2)\n",
	}
	for _, src := range cases {
		mustParse(t, ModeRepl, src)
	}
}

func TestParseCollections(t *testing.T) {
	cases := []string{
		"t = (1, \"a\", :ok)\n",
		"t1 = (1,)\n",
		"g = (1)\n",
		"l = [1, 2, 3]\n",
		"v = %[1, 2, 3]\n",
		"m = %{ \"a\" => 1, \"b\" => 2 }\n",
		"r = User{ id: 1, name: \"a\" }\n",
		"an = { id: 1 }\n",
	}
	for _, src := range cases {
		mustParse(t, ModeRepl, src)
	}
}

func TestParsePostfix(t *testing.T) {
	cases := []string{
		"x.name\n",
		"List.map(xs, f)\n",
		"v[0]\n",
		"v[0][1]\n",
		"obj.method(a).field\n",
	}
	for _, src := range cases {
		mustParse(t, ModeRepl, src)
	}
}

// ---- Control flow ----

func TestParseIfInline(t *testing.T) {
	mustParse(t, ModeRepl, "x = if ready then 1 else 0\n")
}

func TestParseIfBlock(t *testing.T) {
	src := "if ready\n    start()\nelse\n    wait()\n"
	mustParse(t, ModeRepl, src)
}

func TestParseMatch(t *testing.T) {
	src := `x = match v
    Some(u) -> u
    None -> 0
`
	mustParse(t, ModeRepl, src)
}

func TestParseWithElse(t *testing.T) {
	src := `with
    Ok(a) <- validate(x)
    Ok(b) <- transform(a)
    Ok(b)
`
	mustParse(t, ModeRepl, src)
}

func TestParseTrapInline(t *testing.T) {
	mustParse(t, ModeRepl, "result = trap(1 + 1)\n")
}

func TestParseTrapBlock(t *testing.T) {
	// ensure — trap_item внутри INDENT-блока trap (T-156 #211: раньше
	// хвост после первого trap_item молча отбрасывался, и этот тест не
	// проверял ни ensure, ни f2()).
	src := `result = trap
    f1()
    ensure close_f1()
    f2()
`
	mustParse(t, ModeRepl, src)
}

// ---- Patterns ----

func TestParsePatterns(t *testing.T) {
	cases := []string{
		"fn f(_) -> 1\n",
		"fn f(x) -> x\n",
		"fn f(1) -> 1\n",
		"fn f(Some(x)) -> x\n",
		"fn f((a, b)) -> a\n",
		"fn f([1, ..rest]) -> rest\n",
		"fn f(%{ \"a\" => a }) -> a\n",
		"fn f(User{ id: id }) -> id\n",
		"fn f((:ok, _) as full) -> full\n",
	}
	for _, src := range cases {
		mustParse(t, ModeModule, "module M\n"+src)
	}
}

// ---- Types ----

func TestParseTypes(t *testing.T) {
	cases := []string{
		"module M\ntype T = Int\n",
		"module M\ntype T = List<Int>\n",
		"module M\ntype T = Map<Str, Int>\n",
		"module M\ntype T = (Int, Str) -> Bool\n",
		"module M\ntype T = () -> Int\n",
		"module M\ntype T = Option<Result<Int, Str>>\n",
		"module M\ntype T = Tuple<Int, Str, Bool>\n",
	}
	for _, src := range cases {
		mustParse(t, ModeModule, src)
	}
}

// ---- Errors ----

func TestParseErrors(t *testing.T) {
	cases := []struct {
		mode Mode
		src  string
		sub  string
	}{
		{ModeModule, "fn main() ->\n", "INDENT"},
		{ModeRepl, "1 +\n", "expression"},
		{ModeRepl, "x = \n", "expression"},
		{ModeRepl, "f(1,\n", ""},
		{ModeModule, "type\n", "type name"},
	}
	for _, c := range cases {
		mustFail(t, c.mode, c.src, c.sub)
	}
}

// TestParseReplRejectsTrailingTokens — T-156 (#211): repl_line ::= stmt
// разбирает ровно один стейтмент; всё, что осталось после него, раньше
// молча отбрасывалось (`parseRepl` не проверял NEWLINE/EOF после стейтмента).
func TestParseReplRejectsTrailingTokens(t *testing.T) {
	// Хвост на той же строке (короткая лямбда без скобок вокруг параметра
	// съедает только "1", "-> a" остаётся неразобранным).
	mustFail(t, ModeRepl, "f = 1 -> a\n", "->")

	// Второй стейтмент после NEWLINE — тоже не может быть тихо
	// отброшен: порция ввода — repl_input (T-201), он разбирается
	// следующей repl_line.
	prog, err := ParseProgram(ModeRepl, "x = 1\ny = 2\n")
	if err != nil || len(prog.Stmts) != 2 {
		t.Fatalf("Parse(repl, \"x = 1\\ny = 2\\n\"): stmts=%v err=%v, want 2 stmts", prog, err)
	}

	// Тот же вход в module-режиме — предсказуемая, отдельно проверенная
	// ошибка (не тема этой задачи, но фиксирует контраст из issue).
	mustFail(t, ModeModule, "module M\nfn main() ->\n    f = 1 -> a\n", "NEWLINE")

	// `(a, 1)` — не валидный список параметров короткой лямбды (`1` не
	// идентификатор), поэтому `(a, 1)` разбирается как кортеж, а
	// оставшееся `-> a` теперь тоже ошибка, а не тихо отброшенный хвост.
	mustFail(t, ModeRepl, "f = (a, 1) -> a\n", "->")
}

// TestParsePubQuoteReservedAsFnName — T-132 (#190): `pub`/`quote` —
// ключевые слова лексера, парсер отвергает их в позиции идентификатора
// (имя fn, LHS присваивания) как любое другое ключевое слово.
func TestParsePubQuoteReservedAsFnName(t *testing.T) {
	mustFail(t, ModeModule, "module M\nfn pub() -> 1\n", "")
	mustFail(t, ModeRepl, "quote = 1\n", "")
}

// ---- Fuzz ----

func FuzzParse(f *testing.F) {
	f.Add("fn main() ->\n    1 + 2\n")
	f.Add("x = 1\n")
	f.Add("module M\nimport A\ntype T = Int\n")
	f.Fuzz(func(_ *testing.T, data string) {
		_ = Parse(ModeRepl, data)
		_ = Parse(ModeModule, data)
		_ = Parse(ModeScript, data)
	})
}

// TestParseScriptMode — T-207 (§11.3): script — те же инструкции, что REPL;
// type, pub fn и module в нём — ошибка парсинга. Shebang — комментарий.
func TestParseScriptMode(t *testing.T) {
	mustParse(t, ModeScript, "x = 1\nprint(x)\n")
	mustParse(t, ModeScript, "#!/usr/bin/env brig\nprint(1)\n")
	mustParse(t, ModeScript, "fn main() -> 1\nprint(2)\n")
	mustParse(t, ModeScript, "\n\nx = 1\n")
	mustFail(t, ModeScript, "type User { id: Int }\n", "type")
	mustFail(t, ModeScript, "pub fn f() -> 1\n", "pub")
	mustFail(t, ModeScript, "module Main\nfn main() -> 1\n", "module")
}

// TestParseReplInputLines — T-201: repl_input — одна или несколько
// repl_line по порядку, в том числе после блочной инструкции (DEDENT
// без NEWLINE) и вперемешку с import.
func TestParseReplInputLines(t *testing.T) {
	src := "fn f(x) ->\n    x + 1\nimport Map\ny = f(1)\n"
	lines, err := ParseReplInput(src)
	if err != nil {
		t.Fatalf("ParseReplInput: %v", err)
	}
	if len(lines) != 3 || len(lines[0].Stmts) != 1 || len(lines[1].Decls) != 1 || len(lines[2].Stmts) != 1 {
		t.Fatalf("ParseReplInput(%q): got %v, want fn, import, let", src, lines)
	}
	if _, err := ParseReplInput("x = 1 y = 2\n"); err == nil {
		t.Fatalf("two statements on one line: want error")
	}
}
