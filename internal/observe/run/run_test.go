package run

import (
	"bytes"
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/observe"
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
	if strings.Count(got, "\x1b[?25l") != 1 || strings.Count(got, "\x1b[?25h") != 1 {
		t.Fatalf("cursor %q", got)
	}
	if strings.Count(got, "\x1b[0m") != 1 {
		t.Fatalf("sgr reset %q", got)
	}
}
