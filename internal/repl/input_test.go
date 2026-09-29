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
		// T-145, §3.5: """ не закрыта — ввод продолжается построчно, как
		// для обычной незакрытой строки (regression: сообщение об ошибке
		// раньше указывало на строку открывающих """, а не на последнюю
		// введённую, и NeedMore ошибочно завершал ввод).
		{"\"\"\"\n", true},
		{"\"\"\"\nhello\n", true},
		{"\"\"\"\nhello\n\"\"\"\n", false},
	})
}

// TestIndent — T-202: отступ новой строки — ведущие пробелы последней
// строки текста до курсора; если она кончается заголовком блока, на
// уровень глубже. Открытая скобка и инлайн-форма уровень не добавляют.
func TestIndent(t *testing.T) {
	const (
		lv1 = "    "
		lv2 = "        "
		lv3 = "            "
	)
	cases := []struct {
		src  string
		want string
	}{
		{"", ""},
		{"x = 1", ""},
		{"fn f(x) -> x + 1", ""},
		{"x = if ready then 1 else 0", ""},
		{"if ready then", ""}, // блочный if — без then
		{"xs |> map(x -> x + 1)", ""},
		{"r = trap(f())", ""},
		{"ensure cleanup", ""}, // ensure expr — инлайн
		{"1 +", ""},
		{"\"abc", ""}, // оборванный литерал: уровень не добавляется
		{"xs = [1,", ""},

		{"fn f(x) ->", lv1},
		{"match v", lv1},
		{"y = match v", lv1},
		{"recv", lv1},
		{"r = trap", lv1},
		{"with", lv1},
		{"if ready", lv1},
		{"g = fn (x) ->", lv1},
		{"else", lv1},
		{"else reason", lv1},
		{"ensure", lv1},
		{"0 ->", lv1},

		{"fn f(x) ->\n    x + 1", lv1},
		{"    x + 1", lv1},
		{"    xs = [1,", lv1},
		{"fn f(x) ->\n    match x", lv2},
		{"    match x", lv2},
		{"        0 -> 1", lv2},
		{"        0 ->", lv3},

		{"fn f(x) ->\n", ""}, // курсор в начале пустой строки
		{"    ", lv1},
		{"# комментарий", ""},
		{"    # комментарий", lv1},
		{"    x = 1\nfn f() ->", lv1}, // смотрит только последнюю строку
	}
	for _, c := range cases {
		if got := repl.Indent(c.src); got != c.want {
			t.Errorf("Indent(%q) = %q, want %q", c.src, got, c.want)
		}
	}
	if repl.IndentWidth != 4 {
		t.Errorf("IndentWidth = %d, want 4", repl.IndentWidth)
	}
}
