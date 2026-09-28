package vm

import (
	"testing"
	"time"

	"github.com/it1ro/brig-lang/internal/runtime"
)

// TestTimerHeapUnifiedOrder — T-166: при равном deadline recv … after и
// Timer.send_after срабатывают в порядке взвода (одна куча, G4).
func TestTimerHeapUnifiedOrder(t *testing.T) {
	m := New()
	s := m.scheduler
	mk := func(pid int) *Actor {
		a := &Actor{pid: pid, status: actorBlocked, hwm: defaultHWM}
		s.actors[pid] = a
		return a
	}
	a := mk(1)
	c := mk(2)
	b := mk(3)
	dl := time.Now().Add(-time.Millisecond)
	s.armTimer(a, dl)
	s.armSend(c.pid, runtime.Atom("m"), dl)
	s.armTimer(b, dl)
	s.wakeExpired()
	if len(s.ready) != 3 || s.ready[0] != a || s.ready[1] != c || s.ready[2] != b {
		pids := make([]int, len(s.ready))
		for i, x := range s.ready {
			pids[i] = x.pid
		}
		t.Fatalf("wake order %v, want [1 2 3]", pids)
	}
	if len(c.mailbox) != 1 || c.mailbox[0].Inspect() != ":m" {
		t.Fatalf("mailbox %v, want [:m]", c.mailbox)
	}
	if s.ArmedTimers() != 0 {
		t.Fatalf("%d timers left", s.ArmedTimers())
	}
}
