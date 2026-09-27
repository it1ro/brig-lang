package term

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// runeWidth — сколько колонок терминала занимает руна при выводе через
// visible: управляющие символы показываются как `^X`, комбинирующие не
// занимают места, широкие (CJK, эмодзи) — две колонки.
func runeWidth(c rune) int {
	switch {
	case c < 0x20 || c == 0x7f:
		return 2
	case unicode.In(c, unicode.Mn, unicode.Me, unicode.Cf):
		return 0
	case isWide(c):
		return 2
	}
	return 1
}

// isWide — East Asian Wide/Fullwidth в основных диапазонах.
func isWide(c rune) bool {
	return c >= 0x1100 && (c <= 0x115f ||
		c >= 0x2e80 && c <= 0xa4cf && c != 0x303f ||
		c >= 0xac00 && c <= 0xd7a3 ||
		c >= 0xf900 && c <= 0xfaff ||
		c >= 0xfe30 && c <= 0xfe4f ||
		c >= 0xff00 && c <= 0xff60 ||
		c >= 0xffe0 && c <= 0xffe6 ||
		c >= 0x1f300 && c <= 0x1f64f ||
		c >= 0x1f900 && c <= 0x1f9ff ||
		c >= 0x20000 && c <= 0x3fffd)
}

// cells — ширина текста на экране; ANSI-последовательности `ESC [ … m`
// (раскраска, приглашения) места не занимают.
func cells(s string) int {
	n := 0
	for i := 0; i < len(s); {
		if j := skipCSI(s, i); j > i {
			i = j
			continue
		}
		c, size := utf8.DecodeRuneInString(s[i:])
		n += runeWidth(c)
		i += size
	}
	return n
}

// visible заменяет управляющие символы, кроме начала ANSI-последовательности,
// на `^X` — так текст занимает на экране ровно cells колонок.
func visible(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if j := skipCSI(s, i); j > i {
			b.WriteString(s[i:j])
			i = j
			continue
		}
		c, size := utf8.DecodeRuneInString(s[i:])
		if c < 0x20 || c == 0x7f {
			b.WriteByte('^')
			b.WriteRune(c ^ 0x40)
		} else {
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	return b.String()
}

// skipCSI — конец последовательности `ESC [ … <финальный байт>`,
// начинающейся в s[i]; i — если её там нет.
func skipCSI(s string, i int) int {
	if !strings.HasPrefix(s[i:], "\x1b[") {
		return i
	}
	for j := i + 2; j < len(s); j++ {
		if s[j] >= 0x40 && s[j] <= 0x7e {
			return j + 1
		}
	}
	return i
}
