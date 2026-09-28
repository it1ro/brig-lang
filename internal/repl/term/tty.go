package term

import (
	"os"

	xterm "golang.org/x/term"
)

// Bracketed paste (xterm, режим 2004). Конец вставки разбирает termio.
const (
	pasteOn  = "\x1b[?2004h"
	pasteOff = "\x1b[?2004l"
)

// IsTerminal — f подключён к терминалу.
func IsTerminal(f *os.File) bool { return xterm.IsTerminal(int(f.Fd())) }

// Terminal — редактор на настоящем терминале: raw mode и bracketed paste
// включены только на время ReadInput, между вводами терминал в обычном
// режиме и вывод программы идёт как обычно.
type Terminal struct {
	*Editor
	in, out *os.File
}

// NewTerminal создаёт редактор, читающий in и рисующий в out; ширина —
// текущая ширина out.
func NewTerminal(in, out *os.File) *Terminal {
	e := NewEditor(in, out)
	e.Width = func() int {
		w, _, err := xterm.GetSize(int(out.Fd()))
		if err != nil {
			return 0
		}
		return w
	}
	return &Terminal{Editor: e, in: in, out: out}
}

// ReadInput — Editor.ReadInput в raw mode.
func (t *Terminal) ReadInput(prompt, cont string) (string, error) {
	fd := int(t.in.Fd())
	st, err := xterm.MakeRaw(fd)
	if err != nil {
		return "", err
	}
	defer func() { _ = xterm.Restore(fd, st) }()
	t.write(pasteOn)
	defer t.write(pasteOff)
	return t.Editor.ReadInput(prompt, cont)
}
