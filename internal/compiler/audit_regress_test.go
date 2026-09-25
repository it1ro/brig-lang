package compiler_test

import (
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/parser"
)

func compileSrc(t *testing.T, src string) error {
	t.Helper()
	prog, err := parser.ParseProgram(parser.ModeModule, src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, err = compiler.New().Compile(prog)
	return err
}

// S-F2: мультиклозы, guard и параметры-паттерны fn не компилируются
// (T-50, T-51) — Compile обязан вернуть ошибку, а не взять clauses[0].
func TestAuditMultiClauseNotSilentlyDropped(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"t10_multiclause", `module Main
fn fact(0) -> 1
fn fact(n) -> n * fact(n - 1)
fn main() ->
    print(fact(5))
`, "мультиклозные fn"},
		{"u2_guard_fn", `module Main
fn classify(n) when n > 0 -> "positive"
fn classify(0)             -> "zero"
fn classify(n)             -> "negative"
fn main() ->
    print(classify(-5))
`, "мультиклозные fn"},
		{"u1_spec65", `module Main
fn pow(base, exp) ->
    fn go(acc, 0) -> acc
    fn go(acc, n) -> go(acc * base, n - 1)
    go(1, exp)
fn main() ->
    print(pow(2, 10))
`, "мультиклозные fn"},
		{"guard_single_clause", `module Main
fn positive(n) when n > 0 -> "positive"
fn main() ->
    print(positive(-5))
`, "guard"},
		{"local_guard_single_clause", `module Main
fn main() ->
    fn positive(n) when n > 0 -> "positive"
    print(positive(-5))
`, "guard"},
		{"literal_param", `module Main
fn is_zero(0) -> true
fn main() ->
    print(is_zero(5))
`, "параметр-паттерн"},
		{"bool_literal_param", `module Main
fn yes(true) -> 1
fn main() ->
    print(yes(false))
`, "параметр-паттерн"},
		{"wildcard_param", `module Main
fn one(_) -> 1
fn main() ->
    print(one(5))
`, "параметр-паттерн"},
		{"tuple_param", `module Main
fn first((a, b)) -> a
fn main() ->
    print(first((1, 2)))
`, "параметр-паттерн"},
		{"local_literal_param", `module Main
fn main() ->
    fn is_zero(0) -> true
    print(is_zero(5))
`, "параметр-паттерн"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := compileSrc(t, tc.src)
			if err == nil {
				t.Fatalf("Compile: want error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Compile: want error containing %q, got %v", tc.want, err)
			}
		})
	}
}

// S-F3: guard в ветках recv не должен молча игнорироваться компилятором.
// Compile обязан вернуть ошибку, а не матчить ветку по паттерну без
// проверки условия (T-02).
func TestAuditRecvGuardRejected(t *testing.T) {
	src := `module Main
fn main() ->
    recv (:msg) when true -> :ok
`
	err := compileSrc(t, src)
	if err == nil {
		t.Fatalf("Compile: want error containing %q, got nil", "guard")
	}
	if !strings.Contains(err.Error(), "guard") {
		t.Fatalf("Compile: want error containing %q, got %v", "guard", err)
	}
}

func TestAuditSimpleParamsStillCompile(t *testing.T) {
	src := `module Main
fn head(a, ..rest) -> a
fn ok?(ready?) -> ready?
fn main() ->
    fn add(x, y) -> x + y
    print(add(head(1, 2, 3), 2), ok?(true))
`
	if err := compileSrc(t, src); err != nil {
		t.Fatalf("Compile: %v", err)
	}
}
