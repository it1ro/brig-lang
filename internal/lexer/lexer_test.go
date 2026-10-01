package lexer

import (
	"strings"
	"testing"
)

// stripOffside убирает NEWLINE/INDENT/DEDENT для компактного сравнения.
func types(toks []Token) []TokenType {
	out := make([]TokenType, 0, len(toks))
	for _, t := range toks {
		out = append(out, t.Type)
	}
	return out
}

func eqTypes(t *testing.T, src string, want ...TokenType) {
	t.Helper()
	toks, err := Lex(src)
	if err != nil {
		t.Fatalf("Lex(%q) error: %v", src, err)
	}
	got := types(toks)
	if len(got) != len(want) {
		t.Fatalf("Lex(%q)\n got: %v\nwant: %v", src, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("Lex(%q)[%d]\n got: %v\nwant: %v", src, i, got, want)
		}
	}
}

func TestLexBasics(t *testing.T) {
	cases := []struct {
		src  string
		want []TokenType
	}{
		// Классика из дизайна.
		{`x = 1`, []TokenType{LOWER_IDENT, OP_ASSIGN, INT, NEWLINE, EOF}},
		{
			`t = (1, "a", :ok)`,
			[]TokenType{LOWER_IDENT, OP_ASSIGN, LPAREN, INT, COMMA, STRING, COMMA, ATOM, RPAREN, NEWLINE, EOF},
		},
		{
			`v = %[1, 2, 3]`,
			[]TokenType{LOWER_IDENT, OP_ASSIGN, VEC_OPEN, INT, COMMA, INT, COMMA, INT, RBRACKET, NEWLINE, EOF},
		},
		{
			`m = %{ "a" => 1 }`,
			[]TokenType{LOWER_IDENT, OP_ASSIGN, MAP_OPEN, STRING, OP_FATARROW, INT, RBRACE, NEWLINE, EOF},
		},
		{`f(x)`, []TokenType{LOWER_IDENT, LPAREN, LOWER_IDENT, RPAREN, NEWLINE, EOF}},
		{
			`xs |> f(a, b)`,
			[]TokenType{LOWER_IDENT, OP_PIPE, LOWER_IDENT, LPAREN, LOWER_IDENT, COMMA, LOWER_IDENT, RPAREN, NEWLINE, EOF},
		},
		{`:ready?`, []TokenType{ATOM, NEWLINE, EOF}},
		{`and?`, []TokenType{LOWER_IDENT, NEWLINE, EOF}}, // шаг 11a: не keyword
		{`map?`, []TokenType{LOWER_IDENT, NEWLINE, EOF}},
		{`_`, []TokenType{WILDCARD, NEWLINE, EOF}},
		{
			`User{ id: 1 }`,
			[]TokenType{UPPER_IDENT, LBRACE, LOWER_IDENT, COLON, INT, RBRACE, NEWLINE, EOF},
		},
		{`0xFF`, []TokenType{INT, NEWLINE, EOF}},
		{`0b101`, []TokenType{INT, NEWLINE, EOF}},
		{`1_000_000`, []TokenType{INT, NEWLINE, EOF}},
		{`1.5`, []TokenType{FLOAT, NEWLINE, EOF}},
		{`1e9`, []TokenType{FLOAT, NEWLINE, EOF}},
		{`1.5e-3`, []TokenType{FLOAT, NEWLINE, EOF}},
		{`b"\x89PNG"`, []TokenType{BYTES, NEWLINE, EOF}},
		{`dec"1_000.5"`, []TokenType{DECIMAL, NEWLINE, EOF}},
		{`rx"[a-z]+"`, []TokenType{REGEX, NEWLINE, EOF}},
		{`"Привет, \(name)!"`, []TokenType{STRING, NEWLINE, EOF}},
		{`1 to 10`, []TokenType{INT, KW_TO, INT, NEWLINE, EOF}},
		{`-x**2`, []TokenType{OP_MINUS, LOWER_IDENT, OP_POW, INT, NEWLINE, EOF}},
		{
			`"a" <> name`,
			[]TokenType{STRING, OP_CONCAT, LOWER_IDENT, NEWLINE, EOF},
		},
	}
	for _, c := range cases {
		eqTypes(t, c.src, c.want...)
	}
}

// TestLexOffside — примеры A5.5 и A5.6 из спецификации.
func TestLexOffside(t *testing.T) {
	src := "fn main() ->\n    x = 1\n    y = 2\n    x + y\n"
	toks, err := Lex(src)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tk := range toks {
		got = append(got, tk.Type.String())
	}
	want := []string{
		"fn", "LOWER_IDENT", "(", ")", "->", "NEWLINE", "INDENT",
		"LOWER_IDENT", "=", "INT", "NEWLINE",
		"LOWER_IDENT", "=", "INT", "NEWLINE",
		"LOWER_IDENT", "+", "LOWER_IDENT", "NEWLINE", "DEDENT", "EOF",
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("offside (A5.5)\n got: %v\nwant: %v", got, want)
	}
}

func TestLexContinuation(t *testing.T) {
	// A5.6: '*' на строке-продолжении не порождает NEWLINE/INDENT/DEDENT,
	// stmt_indent остаётся 4 (СУ-004).
	src := "fn main() ->\n    x = 1\n        * 2\n    x\n"
	toks, err := Lex(src)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tk := range toks {
		got = append(got, tk.Type.String())
	}
	want := []string{
		"fn", "LOWER_IDENT", "(", ")", "->", "NEWLINE", "INDENT",
		"LOWER_IDENT", "=", "INT", "*", "INT", "NEWLINE",
		"LOWER_IDENT", "NEWLINE", "DEDENT", "EOF",
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("continuation (A5.6)\n got: %v\nwant: %v", got, want)
	}
}

func TestLexContinuationConcat(t *testing.T) {
	// A5.6: '<>' на строке-продолжении не порождает NEWLINE/INDENT/DEDENT.
	src := "fn main() ->\n    x = \"a\"\n        <> \"b\"\n    x\n"
	toks, err := Lex(src)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tk := range toks {
		got = append(got, tk.Type.String())
	}
	want := []string{
		"fn", "LOWER_IDENT", "(", ")", "->", "NEWLINE", "INDENT",
		"LOWER_IDENT", "=", "STRING", "<>", "STRING", "NEWLINE",
		"LOWER_IDENT", "NEWLINE", "DEDENT", "EOF",
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("continuation <> \n got: %v\nwant: %v", got, want)
	}
}

func TestLexContinuationTooShallow(t *testing.T) {
	_, err := Lex("x = 1\n* 2\n")
	if err == nil || !strings.Contains(err.Error(), "continuation") {
		t.Fatalf("want continuation error, got %v", err)
	}
}

// TestUnaryMinusStartsStatement: '+' и '-' не продолжают строку (§2.2,
// T-255) — строка с ведущим '-' или '+' начинает новый стейтмент.
func TestUnaryMinusStartsStatement(t *testing.T) {
	for _, op := range []string{"-", "+"} {
		src := "fn f(x) ->\n    y = x\n    " + op + "y\n"
		toks, err := Lex(src)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, tk := range toks {
			got = append(got, tk.Type.String())
		}
		want := []string{
			"fn", "LOWER_IDENT", "(", "LOWER_IDENT", ")", "->", "NEWLINE", "INDENT",
			"LOWER_IDENT", "=", "LOWER_IDENT", "NEWLINE",
			op, "LOWER_IDENT", "NEWLINE", "DEDENT", "EOF",
		}
		if strings.Join(got, " ") != strings.Join(want, " ") {
			t.Fatalf("leading %s\n got: %v\nwant: %v", op, got, want)
		}
	}
}

// TestLexBracketContinuation: внутри скобок строка, которая кончается
// бинарным оператором или начинается с оператора продолжения (кроме `..`),
// не отделяется NEWLINE; ведущий '-' — новый элемент (§D.5, T-255).
func TestLexBracketContinuation(t *testing.T) {
	cases := []struct{ src, want string }{
		{"(a +\n  b -\n  c)\n", "( LOWER_IDENT + LOWER_IDENT - LOWER_IDENT ) NEWLINE EOF"},
		{"(xs\n  |> f)\n", "( LOWER_IDENT |> LOWER_IDENT ) NEWLINE EOF"},
		{"(a\n  * b)\n", "( LOWER_IDENT * LOWER_IDENT ) NEWLINE EOF"},
		{"[a\n  -b]\n", "[ LOWER_IDENT NEWLINE - LOWER_IDENT ] NEWLINE EOF"},
		{"[a\n  ..b]\n", "[ LOWER_IDENT NEWLINE .. LOWER_IDENT ] NEWLINE EOF"},
		{"[\"+\"\n  b]\n", "[ STRING NEWLINE LOWER_IDENT ] NEWLINE EOF"},
	}
	for _, tc := range cases {
		toks, err := Lex(tc.src)
		if err != nil {
			t.Fatalf("Lex(%q): %v", tc.src, err)
		}
		var got []string
		for _, tk := range toks {
			got = append(got, tk.Type.String())
		}
		if strings.Join(got, " ") != tc.want {
			t.Fatalf("Lex(%q)\n got: %v\nwant: %v", tc.src, strings.Join(got, " "), tc.want)
		}
	}
}

func TestLexWithinBracketsNoOffside(t *testing.T) {
	// Внутри скобок переносы не порождают NEWLINE (A5.2: paren_depth > 0).
	src := "x = f(1,\n      2)\n"
	toks, err := Lex(src)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tk := range toks {
		got = append(got, tk.Type.String())
	}
	want := []string{
		"LOWER_IDENT", "=", "LOWER_IDENT", "(", "INT", ",",
		"INT", ")", "NEWLINE", "EOF",
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("bracket offside\n got: %v\nwant: %v", got, want)
	}
}

func TestLexComments(t *testing.T) {
	// Комментарий отбрасывается, NEWLINE сохраняется.
	toks, err := Lex("x = 1  # комментарий\n")
	if err != nil {
		t.Fatal(err)
	}
	got := types(toks)
	want := []TokenType{LOWER_IDENT, OP_ASSIGN, INT, NEWLINE, EOF}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestLexPositions(t *testing.T) {
	toks, err := Lex("x = 1\n    y\n")
	if err != nil {
		t.Fatal(err)
	}
	if toks[0].Line != 1 || toks[0].Col != 1 {
		t.Fatalf("x pos: got %d:%d want 1:1", toks[0].Line, toks[0].Col)
	}
	// y на строке 2, колонка 5.
	for _, tk := range toks {
		if tk.Type == LOWER_IDENT && tk.Lit == "y" {
			if tk.Line != 2 || tk.Col != 5 {
				t.Fatalf("y pos: got %d:%d want 2:5", tk.Line, tk.Col)
			}
			return
		}
	}
	t.Fatal("y not found")
}

// TestLexErrors — негативные кейсы (A3.3, A4, КР-004/005/006).
// TestLexPubQuoteKeywords — T-132 (#190, G-6/R-7/S-4): `pub` и `quote`
// становятся ключевыми словами; `:pub`/`:quote` остаются атомами.
func TestLexPubQuoteKeywords(t *testing.T) {
	eqTypes(t, "pub", []TokenType{KW_PUB, NEWLINE, EOF}...)
	eqTypes(t, "quote", []TokenType{KW_QUOTE, NEWLINE, EOF}...)
	eqTypes(t, ":pub", []TokenType{ATOM, NEWLINE, EOF}...)
	eqTypes(t, ":quote", []TokenType{ATOM, NEWLINE, EOF}...)
	// `pub?`/`quote?` — не keyword (шаг 11a), как и все остальные.
	eqTypes(t, "pub?", []TokenType{LOWER_IDENT, NEWLINE, EOF}...)
	eqTypes(t, "quote?", []TokenType{LOWER_IDENT, NEWLINE, EOF}...)

	// В позиции идентификатора (имя fn, LHS присваивания) `pub`/`quote`
	// теперь лексятся как ключевые слова, а не LOWER_IDENT — грамматика
	// (parser) отвергает их там же, где раньше стоял идентификатор.
	eqTypes(t, "fn pub() -> 1", []TokenType{
		KW_FN, KW_PUB, LPAREN, RPAREN, OP_ARROW, INT, NEWLINE, EOF,
	}...)
	eqTypes(t, "quote = 1", []TokenType{KW_QUOTE, OP_ASSIGN, INT, NEWLINE, EOF}...)
}

// TestLexUnderscoreName — T-132 (#190, §1.2): `_msg`, `_unused` лексятся
// как LOWER_IDENT (именованный wildcard), не как ошибка. `_1`/`_X` —
// по-прежнему ошибка ("identifier must not start with '_'").
func TestLexUnderscoreName(t *testing.T) {
	eqTypes(t, "_unused", []TokenType{LOWER_IDENT, NEWLINE, EOF}...)
	eqTypes(t, "_msg", []TokenType{LOWER_IDENT, NEWLINE, EOF}...)
	eqTypes(t, "_x1", []TokenType{LOWER_IDENT, NEWLINE, EOF}...)
	eqTypes(t, "_ready?", []TokenType{LOWER_IDENT, NEWLINE, EOF}...)
	eqTypes(t, "_", []TokenType{WILDCARD, NEWLINE, EOF}...)

	for _, src := range []string{"_1", "_X"} {
		_, err := Lex(src)
		if err == nil || !strings.Contains(err.Error(), "start with '_'") {
			t.Errorf("Lex(%q): want \"start with '_'\" error, got %v", src, err)
		}
	}
}

func TestLexErrors(t *testing.T) {
	cases := []struct {
		src string
		sub string // подстрока сообщения об ошибке
	}{
		{"%", "lone '%' is not"},
		{"1e", "exponent"},
		{"1__0", "underscore"},
		{"1_", "underscore"},
		{"_1", "start with '_'"},
		{"_X", "start with '_'"},
		{"0x", "digit expected"},
		{`dec".5"`, "invalid decimal"},
		{`dec"1."`, "invalid decimal"},
		{`dec"1e9"`, "invalid decimal"},
		{`dec"1__0"`, "invalid decimal"},
		{`"незакрытая`, "unclosed string"},
		{`"a \(b"`, "unclosed interpolation"}, // П-003
		{`"\u{110000}"`, "out of range"},      // П-002
		{`"\u{D800}"`, "surrogate"},           // П-002
		{`"\u{}"`, "empty"},
		{`"\x41"`, "invalid escape"}, // \x в Str запрещён (A4.1)
		{`b"\(x)"`, "forbidden"},     // интерполяция в Bytes запрещена (A4.2)
		{`b"\u{41}"`, "forbidden"},
		{"\tx = 1", "tab"},                                  // табы запрещены (§1)
		{"x = (1\n", "unclosed bracket"},                    // незакрытая скобка на EOF
		{"x = 1\n  y = 2\n z = 3\n", "inconsistent dedent"}, // 4→2→1
	}
	for _, c := range cases {
		_, err := Lex(c.src)
		if err == nil {
			t.Errorf("Lex(%q): want error containing %q, got nil", c.src, c.sub)
			continue
		}
		if !strings.Contains(err.Error(), c.sub) {
			t.Errorf("Lex(%q) error %q: want substring %q", c.src, err, c.sub)
		}
	}
}

func TestLexAtomVsColon(t *testing.T) {
	// KR-006: ':' перед UpperIdent/цифрой — COLON, не ATOM.
	toks, err := Lex("%{ :K => 1 }")
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range toks {
		if tk.Type == ATOM {
			t.Fatalf(":K must not be ATOM (got %v)", tk)
		}
	}
}

// TestLexUnclosedStringTrailingBackslash — регрессия на панику в firstToken,
// найденную FuzzLex. Вход, оканчивающийся на '\' внутри незакрытой строки,
// раньше давал k = len(s)+1 и slice out of range; теперь — обычная ошибка.
// Зеркалит testdata/fuzz/FuzzLex/ff7bb51f08e94b47 (сохранён fuzzer'ом).
func TestLexUnclosedStringTrailingBackslash(t *testing.T) {
	for _, src := range []string{
		`"\\`,
		`"a\`,
		`"ab\`,
		`"\"`,
		`a = "x\`,
	} {
		if _, err := Lex(src); err == nil {
			t.Errorf("Lex(%q): want error, got nil", src)
		}
	}
}

func FuzzLex(f *testing.F) {
	f.Add("x = 1\nfn main() ->\n    1 + 2\n")
	f.Fuzz(func(_ *testing.T, data string) {
		_, _ = Lex(data)
	})
}

// TestLexSF11 — S-F11 / T-23: числовые литералы и ATOM/COLON после ')'.
func TestLexSF11(t *testing.T) {
	t.Run("0x_1 rejects underscore after radix", func(t *testing.T) {
		_, err := Lex("0x_1")
		if err == nil {
			t.Fatal(`Lex("0x_1"): want error, got nil`)
		}
		if !strings.Contains(err.Error(), "underscore") {
			t.Fatalf(`Lex("0x_1") error %q: want substring "underscore"`, err)
		}
	})
	t.Run("0b102 rejects invalid binary digit", func(t *testing.T) {
		_, err := Lex("0b102")
		if err == nil {
			t.Fatal(`Lex("0b102"): want error, got nil`)
		}
		if !strings.Contains(err.Error(), "invalid digit") && !strings.Contains(err.Error(), "digit") {
			t.Fatalf(`Lex("0b102") error %q: want digit-related message`, err)
		}
	})
	t.Run("colon after RPAREN is COLON not ATOM", func(t *testing.T) {
		// Probe p/z4.brig: f():x — после ')' ':' эмитируется как COLON (§1.5).
		eqTypes(t, "f():x",
			LOWER_IDENT, LPAREN, RPAREN, COLON, LOWER_IDENT, NEWLINE, EOF)
	})
}

// TestLexTripleQuoteIndentStrip — §3.5, research L9: отступ строки с
// закрывающими """ снимается со всех строк (как в Swift); перевод строки
// сразу после открывающих и перед закрывающими в значение не входит.
func TestLexTripleQuoteIndentStrip(t *testing.T) {
	src := "x = \"\"\"\n    a\n    b\n    \"\"\"\n"
	toks, err := Lex(src)
	if err != nil {
		t.Fatalf("Lex(%q): %v", src, err)
	}
	var str *Token
	for i := range toks {
		if toks[i].Type == STRING {
			str = &toks[i]
			break
		}
	}
	if str == nil {
		t.Fatalf("no STRING token in %v", toks)
	}
	if want := "a\nb"; str.Lit != want {
		t.Fatalf("Lit = %q, want %q", str.Lit, want)
	}

	// Отступ содержимого может быть глубже закрывающих """ — лишнее не
	// снимается, а остаётся частью значения.
	src2 := "x = \"\"\"\n        create table t (\n            id int\n        )\n        \"\"\"\n"
	toks2, err := Lex(src2)
	if err != nil {
		t.Fatalf("Lex(%q): %v", src2, err)
	}
	str = nil
	for i := range toks2 {
		if toks2[i].Type == STRING {
			str = &toks2[i]
			break
		}
	}
	if str == nil {
		t.Fatalf("no STRING token in %v", toks2)
	}
	want := "create table t (\n    id int\n)"
	if str.Lit != want {
		t.Fatalf("Lit = %q, want %q", str.Lit, want)
	}
}

// TestLexTripleQuoteLessIndentError — строка с меньшим отступом, чем у
// закрывающих """, — ошибка лексера с line:col (§3.5).
func TestLexTripleQuoteLessIndentError(t *testing.T) {
	src := "x = \"\"\"\n  a\n    \"\"\"\n"
	_, err := Lex(src)
	if err == nil {
		t.Fatalf("Lex(%q): want error, got nil", src)
	}
	le, ok := err.(*Error)
	if !ok {
		t.Fatalf("error %v is not *lexer.Error", err)
	}
	if le.Line != 2 || le.Col != 3 {
		t.Fatalf("error at %d:%d, want 2:3 (%v)", le.Line, le.Col, err)
	}
	if !strings.Contains(err.Error(), "less indent") {
		t.Fatalf("error %q: want substring %q", err, "less indent")
	}

	// Пустая (из пробелов) строка внутри содержимого не считается
	// нарушением отступа.
	src2 := "x = \"\"\"\n    a\n\n    b\n    \"\"\"\n"
	toks, err := Lex(src2)
	if err != nil {
		t.Fatalf("Lex(%q): %v", src2, err)
	}
	var str *Token
	for i := range toks {
		if toks[i].Type == STRING {
			str = &toks[i]
			break
		}
	}
	if str == nil || str.Lit != "a\n\nb" {
		got := ""
		if str != nil {
			got = str.Lit
		}
		t.Fatalf("Lit = %q, want %q", got, "a\n\nb")
	}
}

// TestLexTripleQuoteInterp — интерполяция \(expr) и escape внутри """
// работают как в обычном Str (§3.5).
func TestLexTripleQuoteInterp(t *testing.T) {
	src := "x = \"\"\"\n    hi \\(name)!\\n\n    \"\"\"\n"
	toks, err := Lex(src)
	if err != nil {
		t.Fatalf("Lex(%q): %v", src, err)
	}
	var str *Token
	for i := range toks {
		if toks[i].Type == STRING {
			str = &toks[i]
			break
		}
	}
	if str == nil {
		t.Fatalf("no STRING token in %v", toks)
	}
	if want := `hi \(name)!\n`; str.Lit != want {
		t.Fatalf("Lit = %q, want %q", str.Lit, want)
	}
	parts, exprs, err := SplitInterp(str.Lit)
	if err != nil {
		t.Fatalf("SplitInterp(%q): %v", str.Lit, err)
	}
	if len(exprs) != 1 || exprs[0] != "name" {
		t.Fatalf("exprs = %v, want [name]", exprs)
	}
	if parts[0] != "hi " || parts[1] != `!\n` {
		t.Fatalf("parts = %v", parts)
	}

	// Многострочная строка внутри списка с закрывающими """ на своей строке
	// и последующим токеном (','), как в корпусе (corpus/lookout).
	src2 := "xs = [\n    \"\"\"\n    a\n    \"\"\",\n    \"b\",\n]\n"
	if _, err := Lex(src2); err != nil {
		t.Fatalf("Lex(%q): %v", src2, err)
	}

	// Ошибка escape внутри """ ловится так же, как в обычном Str.
	if _, err := Lex("x = \"\"\"\n    \\x41\n    \"\"\"\n"); err == nil {
		t.Fatal("want error for \\x in triple-quoted string, got nil")
	}
}
