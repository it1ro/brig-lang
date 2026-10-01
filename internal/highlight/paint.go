package highlight

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// Palette — коды SGR 16 цветов терминала. on == false — без escape
// (NO_COLOR или TERM=dumb); направляющие отступа остаются символами.
type Palette struct {
	on   bool
	code map[Class]string
}

// PaletteFromEnv — палитра терминала по окружению (NewPalette с TTY):
// BRIG_THEME, BRIG_COLORS, NO_COLOR, FORCE_COLOR и TERM. getenv == nil —
// os.LookupEnv. Фон при BRIG_THEME=auto — только по COLORFGBG.
func PaletteFromEnv(getenv func(string) (string, bool)) Palette {
	return NewPalette(PaletteOptions{Getenv: getenv, TTY: true})
}

// PaletteOptions — откуда NewPalette берёт настройки.
type PaletteOptions struct {
	// Getenv — окружение; nil — os.LookupEnv.
	Getenv func(string) (string, bool)
	// TTY — поток, в который пишет палитра, — терминал. Вне терминала
	// цвет только с FORCE_COLOR или CLICOLOR_FORCE.
	TTY bool
	// Background — фон терминала по запросу OSC 11 для BRIG_THEME=auto:
	// light и ok == true, если ответ получен. nil — не спрашивать.
	Background func() (light, ok bool)
	// Warn — куда писать предупреждения о BRIG_THEME и BRIG_COLORS;
	// nil — молча.
	Warn io.Writer
}

// NewPalette собирает палитру. Цвет выключают NO_COLOR с непустым
// значением (no-color.org) и TERM=dumb; вне терминала цвет включают
// FORCE_COLOR и CLICOLOR_FORCE. Коды — тема BRIG_THEME (auto, dark,
// light), поверх неё BRIG_COLORS вида `atom=35:string=32` (как
// LS_COLORS): ключ — имя класса, значение — параметр SGR.
func NewPalette(o PaletteOptions) Palette {
	getenv := o.Getenv
	if getenv == nil {
		getenv = os.LookupEnv
	}
	code := make(map[Class]string, len(defaultCodes))
	for k, v := range defaultCodes {
		code[k] = v
	}
	if themeLight(getenv, o.Background, o.Warn) {
		for k, v := range lightCodes {
			code[k] = v
		}
	}
	if v, ok := getenv("BRIG_COLORS"); ok {
		applyColors(code, v, o.Warn)
	}
	return Palette{on: colorOn(getenv, o.TTY), code: code}
}

func colorOn(getenv func(string) (string, bool), tty bool) bool {
	if v, _ := getenv("NO_COLOR"); v != "" {
		return false
	}
	for _, k := range []string{"FORCE_COLOR", "CLICOLOR_FORCE"} {
		if v, _ := getenv(k); v != "" && v != "0" {
			return true
		}
	}
	if v, _ := getenv("TERM"); v == "dumb" {
		return false
	}
	return tty
}

// themeLight — тема светлого фона: BRIG_THEME=light или auto, где фон
// светлый по OSC 11, а без ответа — по COLORFGBG. Не удалось — тёмный.
func themeLight(getenv func(string) (string, bool), bg func() (bool, bool), warn io.Writer) bool {
	theme, _ := getenv("BRIG_THEME")
	switch theme {
	case "light":
		return true
	case "dark":
		return false
	case "", "auto":
	default:
		warnf(warn, "warning: BRIG_THEME: unknown theme %q, using auto\n", theme)
	}
	if bg != nil {
		if light, ok := bg(); ok {
			return light
		}
	}
	if v, ok := getenv("COLORFGBG"); ok {
		if light, ok := colorFGBGLight(v); ok {
			return light
		}
	}
	return false
}

// colorFGBGLight разбирает COLORFGBG (`15;0`, `0;default;15`): последнее
// поле — цвет фона из 16 ANSI. 7 и 9–15 — светлый фон.
func colorFGBGLight(v string) (light, ok bool) {
	fields := strings.Split(v, ";")
	n, err := strconv.Atoi(fields[len(fields)-1])
	if err != nil || n < 0 || n > 15 {
		return false, false
	}
	return n == 7 || n >= 9, true
}

// LightBackground — цвет фона из ответа на OSC 11 (`rgb:ffff/ffff/ffff`):
// светлый, если яркость больше половины. ok == false — ответ не разобран.
func LightBackground(reply string) (light, ok bool) {
	_, rgb, found := strings.Cut(reply, "rgb:")
	if !found {
		return false, false
	}
	rgb = strings.TrimRight(rgb, "\x07\x1b\\")
	parts := strings.Split(rgb, "/")
	if len(parts) != 3 {
		return false, false
	}
	var c [3]float64
	for i, p := range parts {
		if p == "" || len(p) > 4 {
			return false, false
		}
		n, err := strconv.ParseUint(p, 16, 16)
		if err != nil {
			return false, false
		}
		c[i] = float64(n) / float64(uint64(1)<<(4*len(p))-1)
	}
	return 0.299*c[0]+0.587*c[1]+0.114*c[2] > 0.5, true
}

func warnf(w io.Writer, format string, args ...any) {
	if w != nil {
		fmt.Fprintf(w, format, args...)
	}
}

// defaultCodes — сдержанная палитра 16 цветов терминала: цвет у ключевых
// слов, литералов и имён модулей; операторы, пунктуация, привязки и
// прелюдия — цвета терминала. Красный — только error; неизвестное имя
// подчёркнуто; комментарии приглушены (dim), что читается и на тёмном, и
// на светлом фоне. Пустой код — без escape.
var defaultCodes = map[Class]string{
	Keyword: "35",
	Atom:    "36",
	String:  "32",
	Interp:  "33",
	Bytes:   "32",
	Regex:   "3;32",
	Number:  "33",
	Comment: "2",
	Doc:     "2;3",
	Module:  "1;34",
	Type:    "34",
	Op:      "",
	Punct:   "",
	Binding: "",
	Prelude: "",
	Helper:  "1",
	Unknown: "4",
	Error:   "1;31",
}

// lightCodes — тема light поверх defaultCodes: жёлтый на светлом фоне
// не читается.
var lightCodes = map[Class]string{
	Number: "34",
	Interp: "35",
}

// DimCode — SGR приглушённого текста: направляющая отступа, хвост истории.
const DimCode = "2"

// matchCode — пара скобок под курсором: жирная и подчёркнутая поверх
// цвета своего класса. Инверсия прячет саму скобку за фоном курсора.
const matchCode = "1;4"

func applyColors(code map[Class]string, spec string, warn io.Writer) {
	for _, part := range strings.Split(spec, ":") {
		if part == "" {
			continue
		}
		k, v, ok := strings.Cut(part, "=")
		if _, known := code[Class(k)]; !ok || !known {
			warnf(warn, "warning: BRIG_COLORS: unknown class %q\n", k)
			continue
		}
		if !validSGR(v) {
			warnf(warn, "warning: BRIG_COLORS: %s: invalid SGR %q\n", k, v)
			continue
		}
		code[Class(k)] = v
	}
}

// validSGR — параметры SGR через `;`, каждый 0–255 (38;5;n и 48;5;n
// допускают индекс 256 цветов). Пустое значение — без escape.
func validSGR(v string) bool {
	if v == "" {
		return true
	}
	for _, p := range strings.Split(v, ";") {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || n > 255 || len(p) > 3 {
			return false
		}
	}
	return true
}

const guideMark = "│"

// Enabled — палитра рисует SGR. false при NO_COLOR и TERM=dumb.
func (p Palette) Enabled() bool { return p.on }

// Paint рисует res поверх src. Число строк не меняется.
func (p Palette) Paint(src string, res Result) string {
	guide := make(map[int]bool, len(res.Guides))
	for _, g := range res.Guides {
		if g >= 0 && g < len(src) {
			guide[g] = true
		}
	}
	cls := make([]Class, len(src))
	match := make([]bool, len(src))
	for _, sp := range res.Spans {
		a, b := sp.Start, sp.End
		if a < 0 {
			a = 0
		}
		if b > len(src) {
			b = len(src)
		}
		for i := a; i < b; i++ {
			cls[i] = sp.Class
			match[i] = sp.Match
		}
	}
	var b strings.Builder
	for i := 0; i < len(src); {
		if guide[i] && src[i] == ' ' && cls[i] != Error {
			p.guide(&b)
			i++
			continue
		}
		j := i + 1
		for j < len(src) && cls[j] == cls[i] && match[j] == match[i] && !guideHere(guide, cls, src, j) {
			j++
		}
		p.write(&b, src[i:j], cls[i], match[i])
		i = j
	}
	return b.String()
}

func guideHere(guide map[int]bool, cls []Class, src string, j int) bool {
	return guide[j] && src[j] == ' ' && cls[j] != Error
}

func (p Palette) guide(b *strings.Builder) {
	if p.on {
		b.WriteString("\x1b[" + DimCode + "m")
	}
	b.WriteString(guideMark)
	if p.on {
		b.WriteString("\x1b[0m")
	}
}

func (p Palette) write(b *strings.Builder, s string, c Class, match bool) {
	code := ""
	if p.on && c != "" {
		code = p.code[c]
		if match {
			if code == "" {
				code = matchCode
			} else {
				code += ";" + matchCode
			}
		}
	}
	if code == "" {
		b.WriteString(s)
		return
	}
	b.WriteString("\x1b[")
	b.WriteString(code)
	b.WriteByte('m')
	b.WriteString(s)
	b.WriteString("\x1b[0m")
}
