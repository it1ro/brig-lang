package observe

import (
	"errors"
	"testing"

	"github.com/it1ro/brig-lang/internal/actorview"
	"github.com/it1ro/brig-lang/internal/termio"
	"github.com/it1ro/brig-lang/internal/vm"
)

func act(pid int, reds int64, box int) vm.ActorSnapshot {
	return vm.ActorSnapshot{Pid: pid, Status: "recv", Mailbox: box, Reductions: reds, InitialFn: "f"}
}

func pidsOf(m Model) []int {
	rows := m.rows()
	out := make([]int, len(rows))
	for i, r := range rows {
		out[i] = r.Actor.Pid
	}
	return out
}

func key(code termio.KeyCode, r rune) KeyMsg {
	return KeyMsg{Key: termio.Key{Code: code, Rune: r}}
}

// TestObserveKeys — выбор по pid, цикл сортировки, детали, выход.
func TestObserveKeys(t *testing.T) {
	shot := ShotMsg{
		Actors: []vm.ActorSnapshot{act(1, 10, 0), act(2, 50, 1), act(3, 5, 9)},
		Links:  map[int][]int{1: {3, 2}},
	}
	m := Update(Model{}, shot)
	if !m.HasPid || m.Pid != 1 {
		t.Fatalf("initial pid %d has %v", m.Pid, m.HasPid)
	}
	m = Update(m, key(termio.KeyDown, 0))
	m = Update(m, key(termio.KeyDown, 0))
	if m.Pid != 3 {
		t.Fatalf("down pid %d", m.Pid)
	}
	m = Update(m, key(termio.KeyDown, 0))
	if m.Pid != 3 {
		t.Fatalf("down sticks %d", m.Pid)
	}
	m = Update(m, key(termio.KeyUp, 0))
	if m.Pid != 2 {
		t.Fatalf("up pid %d", m.Pid)
	}

	m = Update(m, key(termio.KeyRune, 's'))
	if m.Sort != actorview.SortReductions || pidsOf(m)[0] != 2 || m.Pid != 2 {
		t.Fatalf("reductions sort %v pid %d", pidsOf(m), m.Pid)
	}
	m = Update(m, key(termio.KeyRune, 'S'))
	if m.Sort != actorview.SortMailbox || pidsOf(m)[0] != 3 {
		t.Fatalf("mailbox sort %v", pidsOf(m))
	}
	m = Update(m, key(termio.KeyRune, 's'))
	if m.Sort != actorview.SortPid || pidsOf(m)[0] != 1 {
		t.Fatalf("pid sort %v", pidsOf(m))
	}
	m = Update(m, key(termio.KeyRune, 's'))
	if m.Sort != actorview.SortTree {
		t.Fatalf("back to tree %v", m.Sort)
	}

	m = Update(m, key(termio.KeyEnter, 0))
	if !m.Detail {
		t.Fatal("enter did not open details")
	}
	m = Update(m, key(termio.KeyEsc, 0))
	if m.Detail || m.Quit {
		t.Fatalf("esc detail %v quit %v", m.Detail, m.Quit)
	}
	m = Update(m, key(termio.KeyEsc, 0))
	if !m.Quit {
		t.Fatal("esc on the list should quit")
	}

	m.Quit = false
	m.Detail = true
	m = Update(m, key(termio.KeyRune, 'q'))
	if !m.Quit {
		t.Fatal("q should quit from details")
	}
	m.Quit = false
	m = Update(m, key(termio.KeyInterrupt, 0))
	if !m.Quit {
		t.Fatal("ctrl-c should quit")
	}
}

func TestObserveDeadActor(t *testing.T) {
	m := Update(Model{}, ShotMsg{Actors: []vm.ActorSnapshot{act(1, 1, 0), act(2, 1, 0)}})
	m = Update(m, key(termio.KeyDown, 0))
	if m.Pid != 2 {
		t.Fatalf("pid %d", m.Pid)
	}
	m = Update(m, ShotMsg{Actors: []vm.ActorSnapshot{act(1, 1, 0)}})
	if m.Note != deadNote || m.Pid != 1 || !m.HasPid {
		t.Fatalf("dead note %q pid %d has %v", m.Note, m.Pid, m.HasPid)
	}
	m = Update(m, key(termio.KeyUp, 0))
	if m.Note != "" {
		t.Fatalf("note stuck %q", m.Note)
	}
}

func TestObserveShotErrorKeepsActors(t *testing.T) {
	m := Update(Model{}, ShotMsg{Actors: []vm.ActorSnapshot{act(4, 1, 0)}})
	m = Update(m, ShotMsg{Err: errors.New("timeout")})
	if m.Err != "timeout" || len(m.Actors) != 1 || m.Actors[0].Pid != 4 {
		t.Fatalf("err %q actors %+v", m.Err, m.Actors)
	}
	m = Update(m, ShotMsg{Actors: []vm.ActorSnapshot{act(4, 2, 0)}})
	if m.Err != "" || m.Actors[0].Reductions != 2 {
		t.Fatalf("recovered %+v err %q", m.Actors, m.Err)
	}
}

func TestCrashRing(t *testing.T) {
	var r Ring
	for i := 0; i < ringCap+5; i++ {
		r.Add(Crash{Pid: i})
	}
	list := r.List()
	if len(list) != ringCap {
		t.Fatalf("len %d", len(list))
	}
	if list[0].Pid != ringCap+4 || list[len(list)-1].Pid != 5 {
		t.Fatalf("order %d .. %d", list[0].Pid, list[len(list)-1].Pid)
	}
}
