package lexer

import (
	"strings"
	"testing"
)

// T-138 (#196): offside-мини-блоки внутри скобок (§2.5, §D.6). Якорь
// мини-блока — отступ логической строки с открывателем (T-124), тело —
// строго глубже; после блока внешний скобочный контекст восстановлен.

func lexString(t *testing.T, src string) string {
	t.Helper()
	toks, err := Lex(src)
	if err != nil {
		t.Fatalf("Lex(%q): %v", src, err)
	}
	var got []string
	for _, tk := range toks {
		got = append(got, tk.Type.String())
	}
	return strings.Join(got, " ")
}

func wantStream(t *testing.T, src string, want ...string) {
	t.Helper()
	if got := lexString(t, src); got != strings.Join(want, " ") {
		t.Fatalf("Lex(%q)\n got: %s\nwant: %s", src, got, strings.Join(want, " "))
	}
}

func TestLexMiniBlockLambdaInCall(t *testing.T) {
	// Пример §2.5: `)` на последней строке тела закрывает мини-блок.
	wantStream(t, "fn main() ->\n    ys = map([1, 2], fn (x) ->\n        y = x * 2\n        y + 1)\n    ys\n",
		"fn", "LOWER_IDENT", "(", ")", "->", "NEWLINE", "INDENT",
		"LOWER_IDENT", "=", "LOWER_IDENT", "(", "[", "INT", ",", "INT", "]", ",",
		"fn", "(", "LOWER_IDENT", ")", "->", "NEWLINE", "INDENT",
		"LOWER_IDENT", "=", "LOWER_IDENT", "*", "INT", "NEWLINE",
		"LOWER_IDENT", "+", "INT", "NEWLINE", "DEDENT", ")", "NEWLINE",
		"LOWER_IDENT", "NEWLINE", "DEDENT", "EOF")

	// После мини-блока следующий аргумент после `,` разбирается во
	// внешнем скобочном контексте.
	wantStream(t, "x = f(fn (a) ->\n    a + 1, 2)\n",
		"LOWER_IDENT", "=", "LOWER_IDENT", "(",
		"fn", "(", "LOWER_IDENT", ")", "->", "NEWLINE", "INDENT",
		"LOWER_IDENT", "+", "INT", "NEWLINE", "DEDENT", ",", "INT", ")", "NEWLINE", "EOF")

	// Закрывающая скобка на своей строке.
	wantStream(t, "x = f(fn (a) ->\n    a\n)\n",
		"LOWER_IDENT", "=", "LOWER_IDENT", "(",
		"fn", "(", "LOWER_IDENT", ")", "->", "NEWLINE", "INDENT",
		"LOWER_IDENT", "NEWLINE", "DEDENT", ")", "NEWLINE", "EOF")

	// Инлайн-лямбда внутри скобок мини-блок не открывает.
	wantStream(t, "x = f(fn (a) -> a,\n      2)\n",
		"LOWER_IDENT", "=", "LOWER_IDENT", "(",
		"fn", "(", "LOWER_IDENT", ")", "->", "LOWER_IDENT", ",", "INT", ")", "NEWLINE", "EOF")
}

func TestLexMiniBlockMatchInList(t *testing.T) {
	wantStream(t, "r = [match v\n    1 -> :a\n    _ -> :b\n]\n",
		"LOWER_IDENT", "=", "[", "match", "LOWER_IDENT", "NEWLINE", "INDENT",
		"INT", "->", "ATOM", "NEWLINE",
		"WILDCARD", "->", "ATOM", "NEWLINE", "DEDENT", "]", "NEWLINE", "EOF")

	// Следующий элемент списка после `,` на новой строке.
	wantStream(t, "r = [match v\n    _ -> 1\n, 2]\n",
		"LOWER_IDENT", "=", "[", "match", "LOWER_IDENT", "NEWLINE", "INDENT",
		"WILDCARD", "->", "INT", "NEWLINE", "DEDENT", ",", "INT", "]", "NEWLINE", "EOF")

	// if/else: клауза `else` на уровне якоря продолжает конструкцию.
	wantStream(t, "r = [if c\n    1\nelse\n    2]\n",
		"LOWER_IDENT", "=", "[", "if", "LOWER_IDENT", "NEWLINE", "INDENT",
		"INT", "NEWLINE", "DEDENT", "else", "NEWLINE", "INDENT",
		"INT", "NEWLINE", "DEDENT", "]", "NEWLINE", "EOF")
}

func TestLexMiniBlockFnInMap(t *testing.T) {
	// Пример §13.2: элементы %{…} на своих строках; тело каждой лямбды —
	// глубже строки своего элемента, следующий элемент закрывает блок.
	src := `h = %{
    :get => fn (s, k) ->
        match get(s, k)
            Some(v) -> v
            None -> 0
    :put => fn (s, k, v) ->
        put(s, k, v)
}
`
	wantStream(t, src,
		"LOWER_IDENT", "=", "%{",
		"ATOM", "=>", "fn", "(", "LOWER_IDENT", ",", "LOWER_IDENT", ")", "->", "NEWLINE", "INDENT",
		"match", "LOWER_IDENT", "(", "LOWER_IDENT", ",", "LOWER_IDENT", ")", "NEWLINE", "INDENT",
		"UPPER_IDENT", "(", "LOWER_IDENT", ")", "->", "LOWER_IDENT", "NEWLINE",
		"UPPER_IDENT", "->", "INT", "NEWLINE", "DEDENT", "DEDENT", "NEWLINE",
		"ATOM", "=>", "fn", "(", "LOWER_IDENT", ",", "LOWER_IDENT", ",", "LOWER_IDENT", ")", "->", "NEWLINE", "INDENT",
		"LOWER_IDENT", "(", "LOWER_IDENT", ",", "LOWER_IDENT", ",", "LOWER_IDENT", ")", "NEWLINE", "DEDENT",
		"}", "NEWLINE", "EOF")
}

func TestLexMiniBlockNested(t *testing.T) {
	// Мини-блок внутри скобок внутри мини-блока.
	wantStream(t, "x = f(fn (a) ->\n    g(fn (b) ->\n        b)\n    )\n",
		"LOWER_IDENT", "=", "LOWER_IDENT", "(",
		"fn", "(", "LOWER_IDENT", ")", "->", "NEWLINE", "INDENT",
		"LOWER_IDENT", "(", "fn", "(", "LOWER_IDENT", ")", "->", "NEWLINE", "INDENT",
		"LOWER_IDENT", "NEWLINE", "DEDENT", ")", "NEWLINE", "DEDENT", ")", "NEWLINE", "EOF")
}

func TestLexMiniBlockUnclosed(t *testing.T) {
	_, err := Lex("x = f(fn (a) ->\n    a\n")
	if err == nil || !strings.Contains(err.Error(), "unclosed bracket") {
		t.Fatalf("want unclosed bracket error, got %v", err)
	}
}
