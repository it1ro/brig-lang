package repl_test

import (
	"testing"

	"github.com/it1ro/brig-lang/internal/repl"
)

func checkNeedMore(t *testing.T, cases []struct {
	src  string
	want bool
}) {
	t.Helper()
	for _, c := range cases {
		if got := repl.NeedMore(c.src); got != c.want {
			t.Errorf("NeedMore(%q) = %v, want %v", c.src, got, c.want)
		}
	}
}

// TestNeedMoreOffsideBlock — T-201, §11.4 «Ввод» пп. 2–3: заголовок блока
// в конце строки и открытый INDENT-блок продолжают ввод, пустая строка
// его завершает.
func TestNeedMoreOffsideBlock(t *testing.T) {
	checkNeedMore(t, []struct {
		src  string
		want bool
	}{
		{"fn f(x) ->", true},
		{"fn f(x) ->\n", true},
		{"fn f(x) ->\n  x + 1", true},
		{"fn f(x) ->\n  x + 1\n", true},
		{"fn f(x) ->\n  x + 1\n\n", false},
		{"fn f(x) ->\n  x + 1\nf(1)\n", true},
		{"fn f(x) ->\n\n", false}, // пустая строка закрывает и пустой блок: ошибку покажет Eval
		{"fn f(x) -> x + 1\n", false},
		{"match v\n", true},
		{"y = match v\n", true},
		{"recv\n", true},
		{"r = trap\n", true},
		{"r = trap(f())\n", false},
		{"with\n", true},
		{"if ready\n", true},
		{"x = if ready then 1 else 0\n", false},
		{"g = fn (x) ->\n", true},
		{"xs |> map(x -> x + 1)\n", false},
		{"1 +\n", false}, // вне блока ведущий/висячий оператор ввод не продолжает
		{"x = 1\n", false},
		{"# комментарий\n", false},
		{"", false},
	})
}

// TestNeedMoreBrackets — T-201, §11.4 «Ввод» п. 1: открытые скобки,
// литералы и интерполяция продолжают ввод; пустая строка внутри скобок
// его не завершает.
func TestNeedMoreBrackets(t *testing.T) {
	checkNeedMore(t, []struct {
		src  string
		want bool
	}{
		{"[1,\n", true},
		{"[1,\n\n", true},
		{"[1,\n 2]\n", false},
		{"f(\n", true},
		{"%{\n", true},
		{"%[1\n", true},
		{"(1, [2\n", true},
		{"\"abc", true},
		{"\"abc\n", true},
		{"b\"ab\n", true},
		{"\"a \\(x + \n", true},
		{"\"abc\"\n", false},
		{"\"a (\"\n", false},    // скобка внутри строки не считается
		{"x = 1 # (\n", false},  // и в комментарии
		{"\"abc\nd\"\n", false}, // строка однострочная: оборвана не на последней строке — ошибка, не продолжение
		{"x\t= 1\n", false},     // прочие ошибки лексера ввод завершают
	})
}
