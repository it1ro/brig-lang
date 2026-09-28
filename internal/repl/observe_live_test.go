//go:build unix

package repl

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/vm"
	"golang.org/x/sys/unix"
)

type screenBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *screenBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *screenBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func openPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = master.Close() })
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	n, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("/dev/pts/%d", n)
	slave, err = os.OpenFile(name, os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = slave.Close() })
	if err := unix.IoctlSetWinsize(int(slave.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: 24, Col: 80}); err != nil {
		t.Fatal(err)
	}
	return master, slave
}

// TestObserveLive — приложение живёт, пока TUI на настоящем pty тикает:
// стрелки, Enter, s, q; падение попадает в ленту; после выхода REPL жив.
func TestObserveLive(t *testing.T) {
	master, slave := openPTY(t)
	var log bytes.Buffer
	s := New(vm.New(), &log)
	defer s.Close()
	s.termIn = slave
	s.termOut = slave

	var hits atomic.Int64
	if err := s.vm.Scheduler().Sync(func() error {
		s.vm.DefineGlobal("tick", runtime.Func(&runtime.FuncValue{
			Name: "tick", Arity: 0, IsNative: true,
			Native: func(runtime.Caller, []runtime.Value) (runtime.Value, error) {
				hits.Add(1)
				return runtime.Unit, nil
			},
		}))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	evalOK(t, s, "fn spin() ->\n    tick()\n    recv\n        _ -> spin()\n    after 1 -> spin()\n")
	evalOK(t, s, "spawn(spin)\n")
	evalOK(t, s, "fn later() ->\n    recv\n        _ -> ()\n    after 200 -> raise(:boom)\n")
	evalOK(t, s, "spawn(later)\n")

	var screen screenBuf
	go func() { _, _ = io.Copy(&screen, master) }()

	gone := make(chan struct{})
	go func() {
		defer close(gone)
		deadline := time.Now().Add(4 * time.Second)
		for !strings.Contains(screen.String(), "brig observe") {
			if time.Now().After(deadline) {
				_, _ = master.Write([]byte{'q'})
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		n1 := hits.Load()
		time.Sleep(80 * time.Millisecond)
		if hits.Load() <= n1 {
			t.Errorf("scheduler stalled during observe (%d)", n1)
		}
		until := time.Now().Add(2 * time.Second)
		for !strings.Contains(screen.String(), "boom") && time.Now().Before(until) {
			time.Sleep(20 * time.Millisecond)
		}
		_, _ = master.Write([]byte{0x1b, '[', 'B'})
		time.Sleep(40 * time.Millisecond)
		_, _ = master.Write([]byte{'\r'})
		time.Sleep(40 * time.Millisecond)
		_, _ = master.Write([]byte{'s'})
		time.Sleep(40 * time.Millisecond)
		_, _ = master.Write([]byte{'q'})
	}()

	done := make(chan error, 1)
	go func() {
		_, err := s.Eval("observe()\n")
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("observe: %v\nscreen:\n%s", err, screen.String())
		}
	case <-time.After(8 * time.Second):
		_, _ = master.Write([]byte{'q'})
		t.Fatal("observe did not return")
	}
	<-gone
	deadline := time.Now().Add(time.Second)
	for !strings.Contains(screen.String(), "reductions ↓") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(screen.String(), "boom") {
		t.Fatalf("crash tape missed boom:\n%s", screen.String())
	}
	if !strings.Contains(screen.String(), "sort: reductions") && !strings.Contains(screen.String(), "reductions ↓") {
		t.Fatalf("sort key not applied:\n%s", screen.String())
	}
	got := evalOK(t, s, "1 + 1\n")
	if got.Inspect() != "2" {
		t.Fatalf("repl after observe: %s", got.Inspect())
	}
}
