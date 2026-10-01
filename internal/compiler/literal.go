package compiler

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/it1ro/brig-lang/internal/runtime"
)

func parseLiteralValue(s string) (runtime.Value, error) {
	switch s {
	case "()":
		return runtime.Unit, nil
	case "true":
		return runtime.Bool(true), nil
	case "false":
		return runtime.Bool(false), nil
	}
	if s == "" {
		return runtime.Unit, fmt.Errorf("пустой литерал")
	}
	if s[0] == ':' {
		return runtime.Atom(s[1:]), nil
	}
	if s[0] == '"' && s[len(s)-1] == '"' {
		return runtime.Str(decodeStrBody(s[1 : len(s)-1])), nil
	}
	if strings.HasPrefix(s, `dec"`) && strings.HasSuffix(s, `"`) {
		r, err := runtime.ParseDecimal(s[4 : len(s)-1])
		if err != nil {
			return runtime.Unit, err
		}
		return runtime.Decimal(r), nil
	}
	// Int: base by prefix (0x/0b/0o → 16/2/8, else 10). Arbitrary precision (§3.1, S-F7).
	if v, ok := parseIntLiteral(s); ok {
		return v, nil
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return runtime.Float(f), nil
	}
	return runtime.Unit, fmt.Errorf("неизвестный литерал %q", s)
}

// parseIntLiteral разбирает целочисленный литерал по префиксу основания.
// Underscores допускаются (лексер уже проверил позиции). Не-int строки → ok=false.
func parseIntLiteral(s string) (runtime.Value, bool) {
	clean := strings.ReplaceAll(s, "_", "")
	if clean == "" {
		return runtime.Unit, false
	}
	base := 10
	body := clean
	neg := false
	if body[0] == '+' || body[0] == '-' {
		neg = body[0] == '-'
		body = body[1:]
		if body == "" {
			return runtime.Unit, false
		}
	}
	if len(body) >= 2 && body[0] == '0' {
		switch body[1] {
		case 'x', 'X':
			base = 16
			body = body[2:]
		case 'b', 'B':
			base = 2
			body = body[2:]
		case 'o', 'O':
			base = 8
			body = body[2:]
		}
	}
	if body == "" {
		return runtime.Unit, false
	}
	n := new(big.Int)
	if _, ok := n.SetString(body, base); !ok {
		return runtime.Unit, false
	}
	if neg {
		n.Neg(n)
	}
	return runtime.IntBig(n), true
}

func decodeStrBody(s string) string {
	var sb strings.Builder
	for i := 0; i < len(s); {
		c := s[i]
		if c != '\\' || i+1 >= len(s) {
			sb.WriteByte(c)
			i++
			continue
		}
		esc := s[i+1]
		switch esc {
		case 'n':
			sb.WriteByte('\n')
			i += 2
		case 't':
			sb.WriteByte('\t')
			i += 2
		case 'r':
			sb.WriteByte('\r')
			i += 2
		case '0':
			sb.WriteByte(0)
			i += 2
		case '\\':
			sb.WriteByte('\\')
			i += 2
		case '"':
			sb.WriteByte('"')
			i += 2
		case 'u':
			if i+2 >= len(s) || s[i+2] != '{' {
				sb.WriteByte(c)
				i++
				continue
			}
			j := i + 3
			for j < len(s) && s[j] != '}' {
				j++
			}
			if j >= len(s) {
				sb.WriteByte(c)
				i++
				continue
			}
			var cp uint64
			if _, err := fmt.Sscanf(s[i+3:j], "%x", &cp); err == nil && cp <= 0x10FFFF {
				sb.WriteRune(rune(cp))
			}
			i = j + 1
		case '(':
			// Интерполяция — оставляем как есть.
			sb.WriteByte(c)
			sb.WriteByte(esc)
			i += 2
		default:
			sb.WriteByte(c)
			i++
		}
	}
	return sb.String()
}

func decodeBytesBody(s string) ([]byte, error) {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); {
		c := s[i]
		if c != '\\' {
			out = append(out, c)
			i++
			continue
		}
		if i+1 >= len(s) {
			return nil, fmt.Errorf("trailing backslash")
		}
		esc := s[i+1]
		switch esc {
		case 'n':
			out = append(out, 0x0A)
			i += 2
		case 't':
			out = append(out, 0x09)
			i += 2
		case 'r':
			out = append(out, 0x0D)
			i += 2
		case '0':
			out = append(out, 0x00)
			i += 2
		case '\\':
			out = append(out, 0x5C)
			i += 2
		case '"':
			out = append(out, 0x22)
			i += 2
		case 'x':
			if i+3 >= len(s) {
				return nil, fmt.Errorf("\\xHH requires two hex digits")
			}
			hi := hexVal(s[i+2])
			lo := hexVal(s[i+3])
			if hi < 0 || lo < 0 {
				return nil, fmt.Errorf("\\xHH requires two hex digits")
			}
			out = append(out, byte(hi<<4|lo))
			i += 4
		default:
			return nil, fmt.Errorf("invalid escape \\%c", esc)
		}
	}
	return out, nil
}

func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}
