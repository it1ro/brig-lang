package runtime

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// QuoteStr печатает строку литералом Brig `"..."` (§C): `\n`, `\t`, `\r`,
// `\0`, `\\`, `\"`; прочие управляющие и невидимые символы (Cc, Cf, Zl, Zp)
// — `\u{H}`. Байт не-UTF-8 печатается как `\u{fffd}`.
func QuoteStr(s string) string {
	var sb strings.Builder
	sb.Grow(len(s) + 2)
	sb.WriteByte('"')
	EscapeStr(&sb, s, false)
	sb.WriteByte('"')
	return sb.String()
}

// EscapeStr пишет тело строкового литерала без кавычек. triple — тело
// литерала `"""` (§3.5): перевод строки остаётся как есть, а `"""` в
// содержимом печатается как `\"""`, чтобы не закрыть литерал.
func EscapeStr(sb *strings.Builder, s string, triple bool) {
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			sb.WriteString(`\u{fffd}`)
			i++
			continue
		}
		switch {
		case r == '\n' && triple:
			sb.WriteByte('\n')
		case r == '\n':
			sb.WriteString(`\n`)
		case r == '\t':
			sb.WriteString(`\t`)
		case r == '\r':
			sb.WriteString(`\r`)
		case r == 0:
			sb.WriteString(`\0`)
		case r == '\\':
			sb.WriteString(`\\`)
		case r == '"' && triple:
			if strings.HasPrefix(s[i:], `"""`) {
				sb.WriteString(`\"""`)
				i += 3
				continue
			}
			sb.WriteByte('"')
		case r == '"':
			sb.WriteString(`\"`)
		case invisibleRune(r):
			sb.WriteString(`\u{` + strconv.FormatInt(int64(r), 16) + `}`)
		default:
			sb.WriteRune(r)
		}
		i += size
	}
}

func invisibleRune(r rune) bool {
	return unicode.In(r, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp)
}

// CodeOrigin — место определения кода функции для печати лямбды
// (`<repl>:3`, `main.brig:12`); "" — неизвестно. Реализует *vm.Chunk.
type CodeOrigin interface {
	Origin() string
}

// funcForm — печатная форма функции. Именованная функция — `#<fn sq/1>`,
// `#<fn Util.parse/2>`; лямбда — `#<clsr/0 <repl>:3>` или
// `#<clsr/1 main.brig:12>` (место — CodeOrigin кода); variadic — арность `*`.
func funcForm(mangled string, arity int, code Code) string {
	ar := "*"
	if arity >= 0 {
		ar = strconv.Itoa(arity)
	}
	fn := DemangleFunc(mangled)
	if !fn.Lambda {
		return fmt.Sprintf("#<fn %s/%s>", fn.Name, ar)
	}
	if o, ok := code.(CodeOrigin); ok && o.Origin() != "" {
		return fmt.Sprintf("#<clsr/%s %s>", ar, o.Origin())
	}
	return fmt.Sprintf("#<clsr/%s>", ar)
}

// FuncName — имя функции для пользователя, восстановленное из имени
// компилятора (`__repl__2$sq`, `main$lambda$0$`, `Util.f@2$g`).
type FuncName struct {
	// Name — имя без префиксов компилятора; у лямбды пусто.
	Name string
	// Lambda — анонимная функция.
	Lambda bool
	// Top — код верхнего уровня (ввод REPL или тело скрипта).
	Top bool
}

// DemangleFunc разбирает имя функции компилятора: префикс ввода REPL
// `__repl__N$`, поколение `@N`, вложенность через `$` и сегменты
// `lambda$K$`. Имя без `$` возвращается как есть.
func DemangleFunc(mangled string) FuncName {
	var out FuncName
	rest := mangled
	if r, ok := strings.CutPrefix(rest, "__repl__"); ok {
		num, tail, _ := strings.Cut(r, "$")
		if num == "" && tail == "" {
			out.Top = true
			return out
		}
		if isDigits(num) {
			rest = tail
		}
	}
	segs := strings.Split(strings.TrimSuffix(rest, "$"), "$")
	name := ""
	for i := 0; i < len(segs); i++ {
		seg := segs[i]
		if seg == "lambda" && i+1 < len(segs) && isDigits(segs[i+1]) {
			out.Lambda = true
			name = ""
			i++
			continue
		}
		if at := strings.LastIndexByte(seg, '@'); at > 0 && isDigits(seg[at+1:]) {
			seg = seg[:at]
		}
		if seg != "" {
			out.Lambda = false
			name = seg
		}
	}
	if !out.Lambda {
		out.Name = name
	}
	return out
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// FrameName — имя кадра в stack trace без префиксов компилятора: код
// верхнего уровня — top (`<input>` в REPL, имя файла в script), лямбда —
// `<clsr>`, остальное — имя функции.
func FrameName(mangled, top string) string {
	fn := DemangleFunc(mangled)
	switch {
	case fn.Top:
		return top
	case fn.Lambda:
		return "<clsr>"
	case fn.Name == "":
		return mangled
	}
	return fn.Name
}
