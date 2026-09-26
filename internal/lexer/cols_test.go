package lexer

import "testing"

// §E.2: col считается в Unicode code points, а не в байтах.
func TestColumnsAreCodePoints(t *testing.T) {
	toks, err := Lex("x = \"жж\" + y\n")
	if err != nil {
		t.Fatal(err)
	}
	var plus, y Token
	for _, tk := range toks {
		switch tk.Lit {
		case "+":
			plus = tk
		case "y":
			y = tk
		}
	}
	if plus.Col != 10 || y.Col != 12 {
		t.Fatalf("cols: + at %d, y at %d; want 10, 12", plus.Col, y.Col)
	}

	_, err = Lex("x = \"ж\" $\n")
	le, ok := err.(*Error)
	if !ok || le.Line != 1 || le.Col != 9 {
		t.Fatalf("error = %v, want lex error at 1:9", err)
	}
}
