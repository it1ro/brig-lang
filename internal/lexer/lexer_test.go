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
	// A5.6: '+' на строке-продолжении не порождает NEWLINE/INDENT/DEDENT,
	// stmt_indent остаётся 4 (СУ-004).
	src := "fn main() ->\n    x = 1\n        + 2\n    x\n"
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
		"LOWER_IDENT", "=", "INT", "+", "INT", "NEWLINE",
		"LOWER_IDENT", "NEWLINE", "DEDENT", "EOF",
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("continuation (A5.6)\n got: %v\nwant: %v", got, want)
	}
}

func TestLexContinuationTooShallow(t *testing.T) {
	_, err := Lex("x = 1\n+ 2\n")
	if err == nil || !strings.Contains(err.Error(), "continuation") {
		t.Fatalf("want continuation error, got %v", err)
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
		{"_x", "start with '_'"},
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
