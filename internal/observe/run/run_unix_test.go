//go:build unix

package run

import (
	"bytes"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/it1ro/brig-lang/internal/observe"
	"github.com/it1ro/brig-lang/internal/vm"
)

type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (w *lockedBuf) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.Write(p)
}

func (w *lockedBuf) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.String()
}

func shot() observe.Shot {
	return observe.Shot{Actors: []vm.ActorSnapshot{{
		Pid: 3, HasName: true, Status: "recv", Mailbox: 0, Reductions: 10, InitialFn: "main",
	}}}
}

func TestRunTTYRoundTrip(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	t.Cleanup(func() { _ = w.Close() })
	if _, err := w.WriteString("q"); err != nil {
		t.Fatal(err)
	}
	var out lockedBuf
	err = Run(t.Context(), Deps{
		TTY:      true,
		In:       r,
		Out:      &out,
		Snapshot: func() (observe.Shot, error) { return shot(), nil },
		Size:     func() (int, int) { return 80, 24 },
	})
	if err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, seq := range []string{"\x1b[?1049h", "\x1b[?25l", "\x1b[?7l", "\x1b[?1049l", "\x1b[?25h", "\x1b[?7h", "\x1b[0m"} {
		if strings.Count(got, seq) != 1 {
			t.Fatalf("%q count %d\n%s", seq, strings.Count(got, seq), got)
		}
	}
	if strings.Count(got, "\x1b[H") != 1 {
		t.Fatalf("frames %d", strings.Count(got, "\x1b[H"))
	}
	if !strings.Contains(got, "\r\n") || strings.Contains(got, "\x1b[K") {
		t.Fatal("frame is not CR LF without EL")
	}
}

func TestRunPanicRestores(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	t.Cleanup(func() { _ = w.Close() })
	var out lockedBuf
	func() {
		defer func() { _ = recover() }()
		_ = Run(t.Context(), Deps{
			TTY:      true,
			In:       r,
			Out:      &out,
			Snapshot: func() (observe.Shot, error) { return shot(), nil },
			Crashes:  func() []observe.Crash { panic("boom") },
			Size:     func() (int, int) { return 80, 24 },
		})
	}()
	got := out.String()
	if strings.Count(got, "\x1b[?1049h") != 1 || strings.Count(got, "\x1b[?1049l") != 1 {
		t.Fatalf("panic did not pair alt screen:\n%s", got)
	}
}

func TestRunStdinLeftIntact(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	t.Cleanup(func() { _ = w.Close() })
	if _, err := w.WriteString("qHELLO"); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Run(t.Context(), Deps{
		TTY:      true,
		In:       r,
		Out:      &out,
		Snapshot: func() (observe.Shot, error) { return shot(), nil },
		Size:     func() (int, int) { return 80, 24 },
	}); err != nil {
		t.Fatal(err)
	}
	_ = r.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 8)
	n, err := r.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	if string(buf[:n]) != "HELLO" {
		t.Fatalf("stdin leftover %q", buf[:n])
	}
}

func TestRunNoGoroutineLeak(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	t.Cleanup(func() { _ = w.Close() })
	if _, err := w.WriteString("q"); err != nil {
		t.Fatal(err)
	}
	before := runtime.NumGoroutine()
	var out bytes.Buffer
	if err := Run(t.Context(), Deps{
		TTY:      true,
		In:       r,
		Out:      &out,
		Snapshot: func() (observe.Shot, error) { return shot(), nil },
		Size:     func() (int, int) { return 80, 24 },
	}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if after := runtime.NumGoroutine(); after > before {
		t.Fatalf("goroutines %d -> %d", before, after)
	}
}

func TestRunSkipsIdenticalFrame(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	t.Cleanup(func() { _ = w.Close() })
	tick := make(chan time.Time)
	var out lockedBuf
	done := make(chan error, 1)
	go func() {
		done <- Run(t.Context(), Deps{
			TTY:      true,
			In:       r,
			Out:      &out,
			Tick:     tick,
			Snapshot: func() (observe.Shot, error) { return shot(), nil },
			Size:     func() (int, int) { return 80, 24 },
		})
	}()
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(out.String(), "recv") {
		if time.Now().After(deadline) {
			t.Fatal("no frame")
		}
		time.Sleep(5 * time.Millisecond)
	}
	frames := strings.Count(out.String(), "\x1b[H")
	tick <- time.Now()
	time.Sleep(200 * time.Millisecond)
	if got := strings.Count(out.String(), "\x1b[H"); got != frames {
		t.Fatalf("identical frame rewritten: %d -> %d", frames, got)
	}
	if _, err := w.WriteString("q"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return")
	}
}

func TestRunResize(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	t.Cleanup(func() { _ = w.Close() })
	if _, err := w.WriteString("q"); err != nil {
		t.Fatal(err)
	}
	resize := make(chan Size, 1)
	resize <- Size{W: 100, H: 24}
	var out bytes.Buffer
	if err := Run(t.Context(), Deps{
		TTY:      true,
		In:       r,
		Out:      &out,
		Snapshot: func() (observe.Shot, error) { return shot(), nil },
		Resize:   resize,
		Size:     func() (int, int) { return 80, 24 },
		Tick:     make(chan time.Time),
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), strings.Repeat("─", 100)) {
		t.Fatal("resize did not redraw at 100 columns")
	}
}

func TestRunThreeSnapshotErrors(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	t.Cleanup(func() { _ = w.Close() })
	tick := make(chan time.Time, 4)
	var out lockedBuf
	done := make(chan error, 1)
	go func() {
		done <- Run(t.Context(), Deps{
			TTY:      true,
			In:       r,
			Out:      &out,
			Tick:     tick,
			Snapshot: func() (observe.Shot, error) { return observe.Shot{}, io.ErrClosedPipe },
			Size:     func() (int, int) { return 80, 24 },
		})
	}()
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(out.String(), "снимок недоступен") {
		if time.Now().After(deadline) {
			t.Fatal("error was not drawn")
		}
		time.Sleep(5 * time.Millisecond)
	}
	tick <- time.Now()
	time.Sleep(200 * time.Millisecond)
	tick <- time.Now()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "снимок недоступен") {
			t.Fatalf("err %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not stop after 3 errors")
	}
	if strings.Count(out.String(), "\x1b[?1049l") != 1 {
		t.Fatal("terminal not restored")
	}
}

func TestRunSessionClosed(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	t.Cleanup(func() { _ = w.Close() })
	var out bytes.Buffer
	err = Run(t.Context(), Deps{
		TTY: true,
		In:  r,
		Out: &out,
		Snapshot: func() (observe.Shot, error) {
			return observe.Shot{}, observe.ErrSessionClosed
		},
		Size: func() (int, int) { return 80, 24 },
		Tick: make(chan time.Time),
	})
	if err != observe.ErrSessionClosed {
		t.Fatalf("err %v", err)
	}
	if strings.Count(out.String(), "\x1b[?1049h") != 1 || strings.Count(out.String(), "\x1b[?1049l") != 1 {
		t.Fatalf("restore:\n%s", out.String())
	}
}
