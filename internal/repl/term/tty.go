package term

import (
	"bytes"
	"os"
	"os/exec"
	"os/signal"
	"sync/atomic"
	"syscall"
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
	resized atomic.Bool
	cooked  *xterm.State // режим терминала до ReadInput: для $EDITOR
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
	t := &Terminal{Editor: e, in: in, out: out}
	fd := int(in.Fd())
	e.Wait = func(d time.Duration) bool {
		ok, err := termio.PollReady(fd, d)
		return ok || err != nil
	}
	e.Resized = func() bool { return t.resized.Swap(false) }
	e.External = t.external
	return t
}

// ReadInput — Editor.ReadInput в raw mode.
func (t *Terminal) ReadInput(prompt, cont string) (string, error) {
	fd := int(t.in.Fd())
	st, err := xterm.MakeRaw(fd)
	if err != nil {
		return "", err
	}
	t.cooked = st
	defer func() { _ = xterm.Restore(fd, st) }()
	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-winch:
				t.resized.Store(true)
			case <-done:
				return
			}
		}
	}()
	defer func() {
		signal.Stop(winch)
		close(done)
	}()
	t.write(pasteOn)
	defer t.write(pasteOff)
	return t.Editor.ReadInput(prompt, cont)
}

// external открывает ввод в $VISUAL или $EDITOR (по умолчанию vi) на
// время в обычном режиме терминала и возвращает исправленный текст.
func (t *Terminal) external(src string) (string, error) {
	fd := int(t.in.Fd())
	if t.cooked != nil {
		_ = xterm.Restore(fd, t.cooked)
		defer func() { _, _ = xterm.MakeRaw(fd) }()
	}
	t.write(pasteOff + "\r\n")
	defer t.write(pasteOn)
	f, err := os.CreateTemp("", "brig-*.brig")
	if err != nil {
		return "", err
	}
	path := f.Name()
	defer func() { _ = os.Remove(path) }()
	if _, err := f.WriteString(src + "\n"); err != nil {
		_ = f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	// sh -c: $EDITOR может быть командой с аргументами (`code --wait`).
	cmd := exec.Command("sh", "-c", editor+` "$1"`, "sh", path)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = t.in, t.out, os.Stderr
	if err := cmd.Run(); err != nil {
		return "", err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
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
