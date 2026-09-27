package highlight

import (
	"os"
	"strings"
)

// Palette — коды SGR 16 цветов терминала. on == false — без escape
// (NO_COLOR или TERM=dumb); направляющие отступа остаются символами.
type Palette struct {
	on   bool
	code map[Class]string
}

// PaletteFromEnv читает BRIG_COLORS, NO_COLOR и TERM. getenv == nil —
// os.LookupEnv. BRIG_COLORS имеет вид `atom=35:string=32` (как LS_COLORS):
// ключ — имя класса, значение — параметр SGR.
func PaletteFromEnv(getenv func(string) (string, bool)) Palette {
	if getenv == nil {
		getenv = os.LookupEnv
	}
	code := make(map[Class]string, len(defaultCodes))
	for k, v := range defaultCodes {
		code[k] = v
	}
	if v, ok := getenv("BRIG_COLORS"); ok {
		applyColors(code, v)
	}
	on := true
	if _, ok := getenv("NO_COLOR"); ok {
		on = false
	}
	if v, ok := getenv("TERM"); ok && v == "dumb" {
		on = false
	}
	return Palette{on: on, code: code}
}

var defaultCodes = map[Class]string{
	Keyword: "33",
	Atom:    "35",
	String:  "32",
	Interp:  "36",
	Bytes:   "32",
	Regex:   "35",
	Number:  "36",
	Comment: "90",
	Doc:     "1;90",
	Module:  "33",
	Type:    "33",
	Op:      "31",
	Punct:   "37",
	Binding: "1",
	Prelude: "36",
	Helper:  "1;36",
	Unknown: "31",
	Error:   "1;31",
}

func applyColors(code map[Class]string, spec string) {
	for _, part := range strings.Split(spec, ":") {
		k, v, ok := strings.Cut(part, "=")
		if !ok || k == "" || !validSGR(v) {
			continue
		}
		if _, known := code[Class(k)]; !known {
			continue
		}
		code[Class(k)] = v
	}
}

func validSGR(v string) bool {
	if v == "" {
		return false
	}
	for i := 0; i < len(v); i++ {
		if v[i] != ';' && (v[i] < '0' || v[i] > '9') {
			return false
		}
	}
	return true
}

const guideMark = "│"

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
		b.WriteString("\x1b[90m")
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
				code = "7"
			} else {
				code += ";7"
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
