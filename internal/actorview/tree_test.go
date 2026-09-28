package actorview

import (
	"testing"

	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/vm"
)

func actor(pid int, reds int64, box int) vm.ActorSnapshot {
	return vm.ActorSnapshot{
		Pid: pid, Status: "recv", Mailbox: box, Reductions: reds,
		InitialFn: "idle", Watchers: []int{}, Watching: []int{},
	}
}

func pids(rows []Row) []int {
	out := make([]int, len(rows))
	for i, r := range rows {
		out[i] = r.Actor.Pid
	}
	return out
}

func TestForestKeepsChildOrder(t *testing.T) {
	snaps := []vm.ActorSnapshot{actor(1, 0, 0), actor(2, 0, 0), actor(3, 0, 0), actor(4, 0, 0)}
	links := map[int][]int{1: {3, 2}, 3: {4}}
	var got []int
	Walk(snaps, links, func(_ int, a vm.ActorSnapshot) { got = append(got, a.Pid) })
	want := []int{1, 3, 4, 2}
	if len(got) != len(want) {
		t.Fatalf("walk %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("walk %v, want %v", got, want)
		}
	}
	// Мёртвый ребёнок и чужой pid в связи не становятся узлами.
	links[1] = []int{3, 9}
	got = nil
	Walk([]vm.ActorSnapshot{actor(1, 0, 0), actor(3, 0, 0)}, links, func(_ int, a vm.ActorSnapshot) {
		got = append(got, a.Pid)
	})
	if len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Fatalf("dead child walk %v", got)
	}
}

func TestRowsSorts(t *testing.T) {
	snaps := []vm.ActorSnapshot{actor(1, 10, 0), actor(2, 50, 1), actor(3, 5, 9)}
	links := map[int][]int{1: {3, 2}}
	if got := pids(Rows(snaps, links, SortTree)); !eq(got, []int{1, 2, 3}) {
		t.Fatalf("tree %v", got)
	}
	rows := Rows(snaps, links, SortTree)
	if rows[1].Depth != 1 || rows[2].Depth != 1 || !rows[2].Last || rows[1].Last {
		t.Fatalf("depth %+v", rows)
	}
	if got := pids(Rows(snaps, links, SortReductions)); !eq(got, []int{2, 1, 3}) {
		t.Fatalf("reductions %v", got)
	}
	if got := pids(Rows(snaps, links, SortMailbox)); !eq(got, []int{3, 2, 1}) {
		t.Fatalf("mailbox %v", got)
	}
	if got := pids(Rows(snaps, links, SortPid)); !eq(got, []int{1, 2, 3}) {
		t.Fatalf("pid %v", got)
	}
	if Rows(snaps, links, SortReductions)[0].Depth != 0 {
		t.Fatal("flat row has depth")
	}
}

func TestForestCutsCycle(t *testing.T) {
	snaps := []vm.ActorSnapshot{actor(1, 0, 0), actor(2, 0, 0)}
	// 2 ссылается на себя: обход 1, затем 2, ребро на 2 срезается.
	links := map[int][]int{1: {2}, 2: {2}}
	var got []int
	Walk(snaps, links, func(_ int, a vm.ActorSnapshot) { got = append(got, a.Pid) })
	if !eq(got, []int{1, 2}) {
		t.Fatalf("cycle walk %v", got)
	}
}

func TestSortLabelCycle(t *testing.T) {
	s := SortTree
	var labels []string
	for i := 0; i < 5; i++ {
		labels = append(labels, Label(s))
		s = Next(s)
	}
	want := []string{"tree", "reductions ↓", "mailbox ↓", "pid ↑", "tree"}
	for i := range want {
		if labels[i] != want[i] {
			t.Fatalf("labels %v", labels)
		}
	}
}

func eq(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Имя в снимке — значение реестра, не Option. Тест только фиксирует,
// что снимок доезжает до узла как есть.
func TestForestKeepsName(t *testing.T) {
	a := actor(1, 0, 0)
	a.HasName = true
	a.Name = runtime.Atom("w")
	f := Forest([]vm.ActorSnapshot{a}, nil)
	if len(f) != 1 || !f[0].Actor.HasName || f[0].Actor.Name.Atom != "w" {
		t.Fatalf("name %+v", f)
	}
}
