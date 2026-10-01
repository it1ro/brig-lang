// Package termio разбирает клавиши терминала и считает ширину текста.
// Редактор строки и observe пользуются одним разбором.
package termio

import (
	"bufio"
	"bytes"
	"strings"
	"time"
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
	kEsc   // одиночный ESC, без хвоста CSI
	kShiftTab
	kSubmit        // Alt-Enter: отправить ввод целиком
	kBufStart      // Alt-<
	kBufEnd        // Alt->
	kHistPrev      // Alt-P: история по префиксу назад
	kHistNext      // Alt-N
	kYank          // Ctrl-Y
	kYankPop       // Alt-Y
	kUndo          // Ctrl-_, Ctrl-/
	kTranspose     // Ctrl-T
	kKillWordRight // Alt-D, Ctrl-Delete
	kKillWordAlnum // Alt-Backspace: слово из букв и цифр
	kLastArg       // Alt-.
	kCtrlX         // Ctrl-X: префикс Ctrl-X Ctrl-E
	kUnknown
)

type key struct {
	code keyCode
	r    rune
	text string
}

// pasteEnd — конец bracketed paste (xterm, режим 2004).
const pasteEnd = "\x1b[201~"

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
	0x14: kTranspose,
	0x15: kKillStart,
	0x17: kKillWord,
	0x18: kCtrlX,
	0x19: kYank,
	0x1f: kUndo, // Ctrl-_ и Ctrl-/
	0x7f: kBackspace,
}

// KeyCode — действие клавиши. Числа совпадают с внутренними кодами
// редактора: ReadKey и readKey разбирают один и тот же поток.
type KeyCode int

// Key — одна клавиша. Text заполнен у вставки.
type Key struct {
	Code KeyCode
	Rune rune
	Text string
}

// Коды клавиш. У KeyRune печатный символ лежит в Key.Rune.
const (
	KeyRune      KeyCode = KeyCode(kRune)
	KeyEnter     KeyCode = KeyCode(kEnter)
	KeyBackspace KeyCode = KeyCode(kBackspace)
	KeyDelete    KeyCode = KeyCode(kDelete)
	KeyLeft      KeyCode = KeyCode(kLeft)
	KeyRight     KeyCode = KeyCode(kRight)
	KeyUp        KeyCode = KeyCode(kUp)
	KeyDown      KeyCode = KeyCode(kDown)
	KeyHome      KeyCode = KeyCode(kHome)
	KeyEnd       KeyCode = KeyCode(kEnd)
	KeyWordLeft  KeyCode = KeyCode(kWordLeft)
	KeyWordRight KeyCode = KeyCode(kWordRight)
	KeyKillEnd   KeyCode = KeyCode(kKillEnd)
	KeyKillStart KeyCode = KeyCode(kKillStart)
	KeyKillWord  KeyCode = KeyCode(kKillWord)
	KeyEOF       KeyCode = KeyCode(kEOF)
	KeyInterrupt KeyCode = KeyCode(kInterrupt)
	KeyClear     KeyCode = KeyCode(kClear)
	KeySearch    KeyCode = KeyCode(kSearch)
	KeyCancel    KeyCode = KeyCode(kCancel)
	KeyTab       KeyCode = KeyCode(kTab)
	KeyPaste     KeyCode = KeyCode(kPaste)
	KeyEsc       KeyCode = KeyCode(kEsc)
	KeyShiftTab  KeyCode = KeyCode(kShiftTab)
	KeySubmit    KeyCode = KeyCode(kSubmit)
	KeyBufStart  KeyCode = KeyCode(kBufStart)
	KeyBufEnd    KeyCode = KeyCode(kBufEnd)
	KeyHistPrev  KeyCode = KeyCode(kHistPrev)
	KeyHistNext  KeyCode = KeyCode(kHistNext)
	KeyYank      KeyCode = KeyCode(kYank)
	KeyYankPop   KeyCode = KeyCode(kYankPop)
	KeyUndo      KeyCode = KeyCode(kUndo)
	KeyTranspose KeyCode = KeyCode(kTranspose)
	// KeyKillWordRight — Alt-D и Ctrl-Delete, KeyKillWordAlnum —
	// Alt-Backspace (слово из букв и цифр, а не до пробела, как Ctrl-W).
	KeyKillWordRight KeyCode = KeyCode(kKillWordRight)
	KeyKillWordAlnum KeyCode = KeyCode(kKillWordAlnum)
	KeyLastArg       KeyCode = KeyCode(kLastArg)
	KeyCtrlX         KeyCode = KeyCode(kCtrlX)
	KeyUnknown       KeyCode = KeyCode(kUnknown)
)

// EscDelay — сколько ждать хвоста после ESC: без него это одиночный Esc.
const EscDelay = 30 * time.Millisecond

// ReadKey читает одно действие из r. Конец потока — io.EOF.
// Неполный ESC (следующего байта нет) — тоже EOF: вызывающий решает,
// это одиночный Esc или оборванная последовательность.
func ReadKey(r *bufio.Reader) (Key, error) { return ReadKeyWait(r, nil) }

// ReadKeyWait — ReadKey, где после ESC без байтов в буфере r редактор
// ждёт хвост не дольше EscDelay: wait(d) — придут ли данные за d. Не
// пришли — одиночный KeyEsc, и следующая клавиша не теряется. wait == nil
// — ждать хвост без таймаута.
func ReadKeyWait(r *bufio.Reader, wait func(time.Duration) bool) (Key, error) {
	k, err := readKey(r, wait)
	if err != nil {
		return Key{}, err
	}
	return Key{Code: KeyCode(k.code), Rune: k.r, Text: k.text}, nil
}

// readKey читает одно действие из r. Ошибка — только ошибка чтения.
func readKey(r *bufio.Reader, wait func(time.Duration) bool) (key, error) {
	c, _, err := r.ReadRune()
	if err != nil {
		return key{}, err
	}
	if c == 0x1b {
		if wait != nil && r.Buffered() == 0 && !wait(EscDelay) {
			return key{code: kEsc}, nil
		}
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
	case 'd', 'D':
		return key{code: kKillWordRight}, nil
	case 'p', 'P':
		return key{code: kHistPrev}, nil
	case 'n', 'N':
		return key{code: kHistNext}, nil
	case 'y', 'Y':
		return key{code: kYankPop}, nil
	case '<':
		return key{code: kBufStart}, nil
	case '>':
		return key{code: kBufEnd}, nil
	case '.':
		return key{code: kLastArg}, nil
	case '\r', '\n':
		return key{code: kSubmit}, nil
	case 0x1b:
		return key{code: kEsc}, nil
	case 0x7f, 0x08:
		return key{code: kKillWordAlnum}, nil
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
		case "3;5":
			return key{code: kKillWordRight}, nil
		case "200":
			text, err := readPaste(r)
			return key{code: kPaste, text: text}, err
		}
		return key{code: kUnknown}, nil
	}
	if final == 'Z' {
		return key{code: kShiftTab}, nil
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
