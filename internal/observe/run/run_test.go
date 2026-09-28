package run

import (
	"bytes"
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/observe"
	"github.com/it1ro/brig-lang/internal/observe/view"
	"github.com/it1ro/brig-lang/internal/termio"
	"github.com/it1ro/brig-lang/internal/vm"
)

func TestObserveNoTTY(t *testing.T) {
	var calls int
	var out bytes.Buffer
	err := Run(t.Context(), Deps{
		TTY: false,
		Snapshot: func() (observe.Shot, error) {
			calls++
			return observe.Shot{Actors: []vm.ActorSnapshot{{
				Pid: 1, Status: "running", InitialFn: "main",
			}}}, nil
		},
		Out: &out,
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("snapshot calls %d", calls)
	}
	got := out.String()
	if strings.Contains(got, "\x1b[?1049") || strings.Contains(got, "\x1b[31m") || strings.Contains(got, "\x1b[0m") {
		t.Fatalf("no-TTY touched the terminal or used color:\n%s", got)
	}
	if !strings.Contains(got, "brig observe") || !strings.Contains(got, "1 актор") {
		t.Fatalf("frame:\n%s", got)
	}
	if strings.Count(got, "\n") < 1 {
		t.Fatal("frame has no newline")
	}
}

func TestTermFrameCRLF(t *testing.T) {
	frame := view.Render(observe.Model{Actors: []vm.ActorSnapshot{{
		Pid: 1, Status: "recv",
	}}}, 80, 24, false)
	got := termFrame(frame)
	if strings.Contains(got, "\x1b[K") {
		t.Fatal("EL after a full-width line erases the last cell when wrap is off")
	}
	if strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Fatal("bare LF: raw mode does not return the cursor")
	}
	lines := strings.Split(got, "\n")
	if len(lines) != 24 {
		t.Fatalf("lines %d", len(lines))
	}
	for i, line := range lines[:len(lines)-1] {
		if !strings.HasSuffix(line, "\r") {
			t.Fatalf("line %d has no CR", i)
		}
		body := strings.TrimSuffix(line, "\r")
		if termio.Cells(body) != 80 {
			t.Fatalf("line %d width %d", i, termio.Cells(body))
		}
	}
	if termio.Cells(lines[len(lines)-1]) != 80 {
		t.Fatalf("last line width %d", termio.Cells(lines[len(lines)-1]))
	}
	if !strings.Contains(got, "recv") {
		t.Fatal("actor row missing")
	}
}

func TestRestoreIdempotent(t *testing.T) {
	var out bytes.Buffer
	ts, err := openTerm(nil, &out)
	if err != nil {
		t.Fatal(err)
	}
	ts.restore()
	ts.restore()
	got := out.String()
	if strings.Count(got, "\x1b[?1049h") != 1 || strings.Count(got, "\x1b[?1049l") != 1 {
		t.Fatalf("sequences %q", got)
	}
	if strings.Count(got, "\x1b[?7l") != 1 || strings.Count(got, "\x1b[?7h") != 1 {
		t.Fatalf("wrap mode %q", got)
	}
	if strings.Count(got, "\x1b[?25l") != 1 || strings.Count(got, "\x1b[?25h") != 1 {
		t.Fatalf("cursor %q", got)
	}
	if strings.Count(got, "\x1b[0m") != 1 {
		t.Fatalf("sgr reset %q", got)
	}
}
