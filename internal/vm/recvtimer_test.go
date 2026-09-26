package vm

import (
	"math"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/it1ro/brig-lang/internal/runtime"
)

// Bounds for ms→Duration without int64-nanosecond overflow.
const (
	recvTimerMaxMs = math.MaxInt64 / int64(time.Millisecond) // 9223372036854
	recvTimerMinMs = math.MinInt64 / int64(time.Millisecond) // -9223372036854
)

func TestRecvTimerDurationBounds(t *testing.T) {
	cases := []struct {
		name string
		ms   int64
		ok   bool
	}{
		{"zero", 0, true},
		{"one", 1, true},
		{"ten", 10, true},
		{"max_ok", recvTimerMaxMs, true},
		{"max_plus_one", recvTimerMaxMs + 1, false},
		{"min_ok", recvTimerMinMs, true},
		{"min_minus_one", recvTimerMinMs - 1, false},
		{"min_int64", math.MinInt64, false},
		{"max_int64", math.MaxInt64, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, ok := recvTimerDuration(tc.ms)
			if ok != tc.ok {
				t.Fatalf("ms=%d: ok=%v, want %v", tc.ms, ok, tc.ok)
			}
			if !ok {
				return
			}
			want := time.Duration(tc.ms) * time.Millisecond
			if d != want {
				t.Fatalf("ms=%d: got %v, want %v", tc.ms, d, want)
			}
			// Sanity: converted duration must not have flipped sign via overflow.
			if tc.ms > 0 && d <= 0 {
				t.Fatalf("positive ms=%d produced non-positive duration %v", tc.ms, d)
			}
			if tc.ms < 0 && d >= 0 {
				t.Fatalf("negative ms=%d produced non-negative duration %v", tc.ms, d)
			}
		})
	}
}

// TestRecvTimerRejectsBigIntBeyondInt64 drives RECVTIMER with a KindInt
// that does not fit in int64 (would previously truncate via Int64()).
func TestRecvTimerRejectsBigIntBeyondInt64(t *testing.T) {
	huge := new(big.Int).Lsh(big.NewInt(1), 80) // 2^80
	msVal := runtime.IntBig(huge)
	if msVal.IsSmall {
		t.Fatal("expected non-small Int for 2^80")
	}

	c := NewChunk()
	c.NumRegs = 2
	c.Emit(ABC(RECVTIMER, 0, 0, 0), SrcPos{})
	c.Emit(ABC(RETURN, 1, 0, 0), SrcPos{})

	s := New().Scheduler()
	a := &Actor{
		pid:    1,
		status: actorReady,
		frames: []*Frame{{
			chunk: c,
			name:  "main",
			regs:  []runtime.Value{msVal, runtime.Atom("ok")},
		}},
	}
	out := s.stepFrame(a, a.frames[0])
	if out != stepFailed {
		t.Fatalf("outcome=%v, want stepFailed", out)
	}
	if a.err == nil {
		t.Fatal("want type_error, got nil")
	}
	msg := a.err.Error()
	if !strings.Contains(msg, "type_error") || !strings.Contains(msg, "after") {
		t.Fatalf("got %q, want (:type_error, (:after, ...))", msg)
	}
}
