package lexer

import "strconv"

// Escape-валидация (A4): Str (A4.1), Bytes (A4.2), Regex (A4.3), Decimal (A4.4).
// Сканеры строк/байтов/регексов живут в lexer.go; здесь — валидаторы,
// завязанные только на содержимое литерала.

// scanUnicodeEscape — \u{1–6 hex}, ≤ U+10FFFF, не surrogate (П-002).
func scanUnicodeEscape(text string, i, line int) (int, error) {
	if i+2 >= len(text) || text[i+2] != '{' {
		return 0, errf(line, i+1, "'\\u{' expected")
	}
	j := i + 3
	for j < len(text) && isHexDigit(text[j]) && j-(i+3) < 6 {
		j++
	}
	if j >= len(text) || text[j] != '}' {
		return 0, errf(line, i+1, "unterminated '\\u{...}'")
	}
	if j == i+3 {
		return 0, errf(line, i+1, "empty '\\u{}'")
	}
	v, err := strconv.ParseUint(text[i+3:j], 16, 32)
	if err != nil || v > 0x10FFFF {
		return 0, errf(line, i+1, "'\\u{%s}' out of range (max U+10FFFF)", text[i+3:j])
	}
	if v >= 0xD800 && v <= 0xDFFF {
		return 0, errf(line, i+1, "'\\u{%s}' is a surrogate", text[i+3:j])
	}
	return j + 1, nil
}

// validateDecimalBody: `[+-]? [0-9_]+ ("." [0-9_]+)?`, '_' только между цифрами.
func validateDecimalBody(body string, line, col int) error {
	bad := func(format string, args ...any) error {
		return errf(line, col, "invalid decimal: "+format, args...)
	}
	if body == "" {
		return bad("empty body")
	}
	k := 0
	if body[k] == '+' || body[k] == '-' {
		k++
	}
	digits := func(start int) (int, error) {
		if start >= len(body) || !isDecDigit(body[start]) {
			return 0, bad("digit expected")
		}
		prevDigit := true
		k := start
		for k < len(body) {
			c := body[k]
			if isDecDigit(c) {
				prevDigit = true
				k++
				continue
			}
			if c == '_' {
				if !prevDigit || k+1 >= len(body) || !isDecDigit(body[k+1]) {
					return 0, bad("'_' must be between digits")
				}
				prevDigit = false
				k++
				continue
			}
			break
		}
		return k, nil
	}
	end, err := digits(k)
	if err != nil {
		return err
	}
	if end < len(body) && body[end] == '.' {
		if end+1 >= len(body) || !isDecDigit(body[end+1]) {
			return bad("digit required after the dot")
		}
		fEnd, err := digits(end + 1)
		if err != nil {
			return err
		}
		end = fEnd
	}
	if end < len(body) {
		return bad("unexpected character %q", body[end])
	}
	return nil
}

// ---- Классы символов (общие для лексера и валидаторов) ----

func isDecDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

func isHexDigit(c byte) bool {
	return isDecDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func isDigitForBase(c byte, base int) bool {
	switch base {
	case 16:
		return isHexDigit(c)
	case 8:
		return c >= '0' && c <= '7'
	case 2:
		return c == '0' || c == '1'
	}
	return isDecDigit(c)
}
