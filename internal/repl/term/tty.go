package term

import (
	"bytes"
	"os"
	"time"

	"github.com/it1ro/brig-lang/internal/termio"
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

// QueryBackground спрашивает цвет фона (OSC 11) и ждёт ответ не дольше
// timeout. Следом идёт запрос DA1: на него отвечает любой терминал, и
// ответ на OSC 11, если он есть, приходит раньше, поэтому поздний ответ
// не попадает в ввод как клавиши. Результат — сырой ответ терминала;
// "" — терминал не ответил.
func (t *Terminal) QueryBackground(timeout time.Duration) string {
	fd := int(t.in.Fd())
	st, err := xterm.MakeRaw(fd)
	if err != nil {
		return ""
	}
	defer func() { _ = xterm.Restore(fd, st) }()
	t.write("\x1b]11;?\x1b\\\x1b[c")
	var reply []byte
	buf := make([]byte, 128)
	deadline := time.Now().Add(timeout)
	for !da1Done(reply) {
		wait := time.Until(deadline)
		if wait <= 0 {
			break
		}
		ready, err := termio.PollReady(fd, wait)
		if err != nil || !ready {
			continue
		}
		n, err := t.in.Read(buf)
		reply = append(reply, buf[:n]...)
		if err != nil {
			break
		}
	}
	return string(reply)
}

// da1Done — в ответе есть конец ответа DA1 (`ESC [ ? … c`).
func da1Done(reply []byte) bool {
	i := bytes.LastIndex(reply, []byte("\x1b[?"))
	return i >= 0 && bytes.IndexByte(reply[i:], 'c') > 0
}
