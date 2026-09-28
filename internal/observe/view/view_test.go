package view

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/it1ro/brig-lang/internal/actorview"
	"github.com/it1ro/brig-lang/internal/observe"
	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/termio"
	"github.com/it1ro/brig-lang/internal/vm"
)

var update = flag.Bool("update", false, "rewrite observe frame goldens")

func named(pid int, name, status string, box int, reds int64) vm.ActorSnapshot {
	a := vm.ActorSnapshot{
		Pid: pid, HasName: name != "", Status: status, Mailbox: box, Reductions: reds,
		AllocBytes: reds * 8, TurnReductions: 3, TurnAllocBytes: 40,
		InitialFn: "worker", Watchers: []int{1, 2}, Watching: []int{9},
	}
	if name != "" {
		a.Name = runtime.Atom(name)
	}
	return a
}

func treeModel() observe.Model {
	return observe.Model{
		Actors: []vm.ActorSnapshot{
			named(3, "main_sup", "recv", 0, 1204),
			named(7, "http", "running", 4, 98311),
			named(8, "worker", "recv", 40, 10),
			named(9, "db_pool", "recv", 0, 12907),
			named(10, "", "waiting", 1, 2),
		},
		Links:  map[int][]int{3: {9, 7}, 7: {8, 10}},
		Sort:   0,
		Pid:    3,
		HasPid: true,
		Crashes: []observe.Crash{
			{At: time.Date(2026, 9, 28, 12, 4, 31, 0, time.UTC), Pid: 14, Name: "worker", Reason: "(:badarg, …)"},
			{At: time.Date(2026, 9, 28, 12, 4, 29, 0, time.UTC), Pid: 11, Name: "db_conn", Reason: ":down"},
		},
	}
}

func TestObserveRenderFrame(t *testing.T) {
	m := treeModel()
	cases := []struct {
		name  string
		m     observe.Model
		w, h  int
		color bool
	}{
		{name: "empty", m: observe.Model{}, w: 80, h: 24},
		{name: "tree", m: m, w: 80, h: 24},
		{name: "reductions", m: sorted(m, actorview.SortReductions), w: 80, h: 24},
		{name: "mailbox", m: sorted(m, actorview.SortMailbox), w: 80, h: 24},
		{name: "pid", m: sorted(m, actorview.SortPid), w: 80, h: 24},
		{name: "crashes", m: m, w: 80, h: 16},
		{name: "detail_right", m: detailed(m), w: 100, h: 24},
		{name: "detail_bottom", m: detailed(m), w: 80, h: 36},
		{name: "narrow", m: m, w: 50, h: 20},
		{name: "tiny", m: m, w: 32, h: 12},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			frame := Render(tc.m, tc.w, tc.h, tc.color)
			assertFrame(t, frame, tc.w, tc.h)
			compare(t, tc.name, frame)
		})
	}

	t.Run("color", func(t *testing.T) {
		frame := Render(m, 80, 24, true)
		assertFrame(t, frame, 80, 24)
		for _, frag := range []string{"\x1b[2m", "\x1b[1m", "\x1b[31m", "\x1b[0m"} {
			if !strings.Contains(frame, frag) {
				t.Errorf("frame missing %q", frag)
			}
		}
		plain := stripSGR(frame)
		if strings.Contains(plain, "\x1b[2m") || strings.Contains(plain, "\x1b[31m") || strings.Contains(plain, "\x1b[1m") {
			t.Fatal("SGR survived strip")
		}
	})
}

func sorted(m observe.Model, s actorview.Sort) observe.Model {
	m.Sort = s
	return m
}

func detailed(m observe.Model) observe.Model {
	m.Detail = true
	m.Pid = 7
	// Длинное имя функции и больше восьми pid.
	for i := range m.Actors {
		if m.Actors[i].Pid == 7 {
			m.Actors[i].InitialFn = "App.Handler.handle_request$lambda$0$"
			m.Actors[i].Watchers = []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
			m.Actors[i].Watching = []int{}
		}
	}
	return m
}

func assertFrame(t *testing.T, frame string, w, h int) {
	t.Helper()
	lines := strings.Split(frame, "\n")
	if len(lines) != h {
		t.Fatalf("lines %d, want %d", len(lines), h)
	}
	for i, line := range lines {
		if !strings.HasSuffix(line, "\x1b[K") {
			t.Fatalf("line %d: no erase", i)
		}
		body := strings.TrimSuffix(line, "\x1b[K")
		if got := termio.Cells(body); got != w {
			t.Fatalf("line %d width %d, want %d\n%q", i, got, w, body)
		}
	}
}

func compare(t *testing.T, name, frame string) {
	t.Helper()
	path := filepath.Join("testdata", name+".frame")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(frame), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (go test -update)", err)
	}
	if string(want) != frame {
		t.Fatalf("golden %s mismatch\n--- got ---\n%s", name, visible(frame))
	}
}

func stripSGR(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if strings.HasPrefix(s[i:], "\x1b[") {
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			if j < len(s) && s[j] == 'm' {
				i = j + 1
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func visible(s string) string {
	s = strings.ReplaceAll(s, "\x1b[K", "¶")
	s = strings.ReplaceAll(s, " ", "·")
	return s
}
