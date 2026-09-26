package lexer

import (
	"reflect"
	"testing"
)

// TestLexInterpolationParts — якорь T-53 / S-F1: лексер отдаёт части
// строки и исходники выражений внутри \(...), а не один непрозрачный токен.
func TestLexInterpolationParts(t *testing.T) {
	cases := []struct {
		name  string
		body  string
		parts []string
		exprs []string
	}{
		{
			name:  "plain",
			body:  `hello`,
			parts: []string{`hello`},
			exprs: nil,
		},
		{
			name:  "single",
			body:  `Привет, \(name)!`,
			parts: []string{`Привет, `, `!`},
			exprs: []string{`name`},
		},
		{
			name:  "two",
			body:  `a \(x) b \(y + 1) c`,
			parts: []string{`a `, ` b `, ` c`},
			exprs: []string{`x`, `y + 1`},
		},
		{
			name:  "only_expr",
			body:  `\(x)`,
			parts: []string{``, ``},
			exprs: []string{`x`},
		},
		{
			name:  "escaped_not_interp",
			body:  `\\(not interp)`,
			parts: []string{`\\(not interp)`},
			exprs: nil,
		},
		{
			name:  "nested_parens",
			body:  `sum \(f(1, 2))`,
			parts: []string{`sum `, ``},
			exprs: []string{`f(1, 2)`},
		},
		{
			name:  "nested_string",
			body:  `a \("b") c`,
			parts: []string{`a `, ` c`},
			exprs: []string{`"b"`},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			parts, exprs, err := SplitInterp(c.body)
			if err != nil {
				t.Fatalf("SplitInterp: %v", err)
			}
			if !reflect.DeepEqual(parts, c.parts) {
				t.Fatalf("parts: got %#v, want %#v", parts, c.parts)
			}
			if c.exprs == nil {
				if len(exprs) != 0 {
					t.Fatalf("exprs: got %#v, want empty", exprs)
				}
			} else if !reflect.DeepEqual(exprs, c.exprs) {
				t.Fatalf("exprs: got %#v, want %#v", exprs, c.exprs)
			}
		})
	}

	// Lex still accepts the string and validates interpolation bounds.
	toks, err := Lex(`"Привет, \(name)!"`)
	if err != nil {
		t.Fatalf("Lex: %v", err)
	}
	found := false
	for _, tk := range toks {
		if tk.Type == STRING && tk.Lit == `Привет, \(name)!` {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Lex: want STRING with full body, got %v", toks)
	}
}
