package run

import (
	"io"
	"os"
	"sync"

	xterm "golang.org/x/term"
)

const (
	enterSeq = "\x1b[?1049h\x1b[?25l"
	exitSeq  = "\x1b[0m\x1b[?25h\x1b[?1049l"
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
