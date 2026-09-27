package term

import (
	"bufio"
	"bytes"
	"strings"
)

// keyCode — действие, которое редактор получает из потока байтов
// терминала. kRune — печатный символ key.r.
type keyCode int

const (
	kRune keyCode = iota
	kEnter
	kBackspace
	kDelete
	kLeft
	kRight
	kUp
	kDown
	kHome
	kEnd
	kWordLeft  // Alt-B, Ctrl-←
	kWordRight // Alt-F, Ctrl-→
	kKillEnd   // Ctrl-K
	kKillStart // Ctrl-U
	kKillWord  // Ctrl-W, Alt-Backspace
	kEOF       // Ctrl-D
	kInterrupt // Ctrl-C
	kClear     // Ctrl-L
	kSearch    // Ctrl-R
	kCancel    // Ctrl-G
	kTab
	kPaste // bracketed paste: key.text — вставленный текст
	kUnknown
)

type key struct {
	code keyCode
	r    rune
	text string
}

// Последовательности bracketed paste (xterm, режим 2004).
const (
	pasteOn  = "\x1b[?2004h"
	pasteOff = "\x1b[?2004l"
	pasteEnd = "\x1b[201~"
)

// ctrlKeys — управляющие байты (Ctrl-<буква>, Enter, Backspace).
var ctrlKeys = map[rune]keyCode{
	0x01: kHome,      // Ctrl-A
	0x02: kLeft,      // Ctrl-B
	0x03: kInterrupt, // Ctrl-C
	0x04: kEOF,       // Ctrl-D
	0x05: kEnd,       // Ctrl-E
	0x06: kRight,     // Ctrl-F
	0x07: kCancel,    // Ctrl-G
	0x08: kBackspace, // Ctrl-H
	0x09: kTab,
	0x0a: kEnter, // Ctrl-J, '\n' вне raw mode
	0x0b: kKillEnd,
	0x0c: kClear,
	0x0d: kEnter, // '\r' в raw mode
	0x0e: kDown,  // Ctrl-N
	0x10: kUp,    // Ctrl-P
	0x12: kSearch,
	0x15: kKillStart,
	0x17: kKillWord,
	0x7f: kBackspace,
}

// readKey читает одно действие из r. Ошибка — только ошибка чтения.
func readKey(r *bufio.Reader) (key, error) {
	c, _, err := r.ReadRune()
	if err != nil {
		return key{}, err
	}
	if c == 0x1b {
		return readEscape(r)
	}
	if code, ok := ctrlKeys[c]; ok {
		return key{code: code}, nil
	}
	if c < 0x20 {
		return key{code: kUnknown}, nil
	}
	return key{code: kRune, r: c}, nil
}

// readEscape разбирает последовательность после ESC: CSI (`ESC [`),
// SS3 (`ESC O`) и Alt-<клавиша>. Нераспознанная — kUnknown.
func readEscape(r *bufio.Reader) (key, error) {
	c, err := r.ReadByte()
	if err != nil {
		return key{}, err
	}
	switch c {
	case '[':
		return readCSI(r)
	case 'O':
		c, err := r.ReadByte()
		if err != nil {
			return key{}, err
		}
		return key{code: finalKey(c)}, nil
	case 'b', 'B':
		return key{code: kWordLeft}, nil
	case 'f', 'F':
		return key{code: kWordRight}, nil
	case 0x7f, 0x08:
		return key{code: kKillWord}, nil
	}
	return key{code: kUnknown}, nil
}

// readCSI разбирает `ESC [ <параметры> <финальный байт>`.
func readCSI(r *bufio.Reader) (key, error) {
	var params strings.Builder
	for {
		c, err := r.ReadByte()
		if err != nil {
			return key{}, err
		}
		if c >= 0x40 && c <= 0x7e {
			return csiKey(r, params.String(), c)
		}
		params.WriteByte(c)
	}
}

func csiKey(r *bufio.Reader, params string, final byte) (key, error) {
	if final == '~' {
		switch params {
		case "1", "7":
			return key{code: kHome}, nil
		case "4", "8":
			return key{code: kEnd}, nil
		case "3":
			return key{code: kDelete}, nil
		case "200":
			text, err := readPaste(r)
			return key{code: kPaste, text: text}, err
		}
		return key{code: kUnknown}, nil
	}
	code := finalKey(final)
	// `ESC [ 1 ; 5 C` — Ctrl-→, `1;3` — Alt-→.
	if strings.HasSuffix(params, ";5") || strings.HasSuffix(params, ";3") {
		switch code {
		case kLeft:
			code = kWordLeft
		case kRight:
			code = kWordRight
		}
	}
	return key{code: code}, nil
}

func finalKey(c byte) keyCode {
	switch c {
	case 'A':
		return kUp
	case 'B':
		return kDown
	case 'C':
		return kRight
	case 'D':
		return kLeft
	case 'H':
		return kHome
	case 'F':
		return kEnd
	}
	return kUnknown
}

// readPaste читает вставленный текст до `ESC [ 201 ~`. Переводы строк
// `\r\n` и `\r` становятся `\n`; прочие управляющие символы, кроме `\t`,
// отбрасываются.
func readPaste(r *bufio.Reader) (string, error) {
	var raw []byte
	for !bytes.HasSuffix(raw, []byte(pasteEnd)) {
		c, err := r.ReadByte()
		if err != nil {
			return "", err
		}
		raw = append(raw, c)
	}
	text := strings.ToValidUTF8(string(raw[:len(raw)-len(pasteEnd)]), "\uFFFD")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return strings.Map(func(c rune) rune {
		if c < 0x20 && c != '\n' && c != '\t' || c == 0x7f {
			return -1
		}
		return c
	}, text), nil
}
