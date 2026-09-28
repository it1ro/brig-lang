package run

import (
	"io"
	"os"
	"strings"
	"sync"

	xterm "golang.org/x/term"
)

const (
	// ?7l — без автопереноса. Строка кадра ровно в ширину окна, и с
	// включённым переносом терминал уносит её на следующую строку:
	// список акторов уезжает вверх, внизу остаются пустые строки.
	// Сам по себе ?7l курсор после такой строки оставляет на последней
	// колонке; перевод строк — в termFrame.
	enterSeq = "\x1b[?1049h\x1b[?25l\x1b[?7l"
	exitSeq  = "\x1b[0m\x1b[?7h\x1b[?25h\x1b[?1049l"
)

// termState — raw mode и альтернативный экран. restore идемпотентен.
type termState struct {
	mu       sync.Mutex
	out      io.Writer
	fd       int
	st       *xterm.State
	restored bool
}

func openTerm(in *os.File, out io.Writer) (*termState, error) {
	ts := &termState{out: out, fd: -1}
	if in != nil && xterm.IsTerminal(int(in.Fd())) {
		st, err := xterm.MakeRaw(int(in.Fd()))
		if err != nil {
			return nil, err
		}
		ts.fd = int(in.Fd())
		ts.st = st
	}
	ts.writeUnlocked(enterSeq)
	return ts, nil
}

func (t *termState) write(s string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.restored {
		return
	}
	t.writeUnlocked(s)
}

// termFrame готовит кадр view к raw mode. MakeRaw снимает OPOST, и LF
// не возвращает каретку: без CR каждая следующая строка начинается там,
// где кончилась предыдущая. С ?7l это последняя колонка, ESC[K стирает
// её, и на экране остаётся только шапка. Строка и так ровно в ширину
// окна, поэтому EL не нужен: CR LF переводит курсор в колонку 0.
func termFrame(frame string) string {
	frame = strings.ReplaceAll(frame, "\x1b[K\n", "\r\n")
	return strings.TrimSuffix(frame, "\x1b[K")
}

func (t *termState) writeUnlocked(s string) {
	if t.out == nil || s == "" {
		return
	}
	_, _ = io.WriteString(t.out, s)
}

// restore снимает оформление. Повторный вызов ничего не пишет.
func (t *termState) restore() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.restored {
		return
	}
	t.restored = true
	t.writeUnlocked(exitSeq)
	if t.st != nil {
		_ = xterm.Restore(t.fd, t.st)
	}
}
