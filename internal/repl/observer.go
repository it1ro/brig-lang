package repl

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/it1ro/brig-lang/internal/actorview"
	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/vm"
)

// tree/info/top (§11.4). Дерево собирает actorview из снимка и
// Supervisor.which_children (CallNested умеет await). Печать записей
// Actor.info — pretty-выводом T-204.

func (s *Session) tree() (runtime.Value, error) {
	snap := s.vm.Scheduler().SnapshotHere()
	links, err := s.supervisorLinks(snap)
	if err != nil {
		return runtime.Unit, err
	}
	var b strings.Builder
	actorview.Walk(snap, links, func(depth int, a vm.ActorSnapshot) {
		fmt.Fprintf(&b, "%s%s name=%s status=%s mailbox=%s\n",
			strings.Repeat("  ", depth), pidText(a.Pid), nameText(a), statusText(a.Status), mailboxText(a.Mailbox))
	})
	if err := s.writeOut("%s", b.String()); err != nil {
		return runtime.Unit, err
	}
	return runtime.Unit, nil
}

func pidText(pid int) string {
	return runtime.Value{Kind: runtime.KindPid, Pid: pid}.Inspect()
}

func nameText(a vm.ActorSnapshot) string {
	if !a.HasName {
		return runtime.Variant("None").Inspect()
	}
	return runtime.Variant("Some", a.Name).Inspect()
}

func statusText(status string) string { return runtime.Atom(status).Inspect() }

func mailboxText(n int) string { return runtime.Int(int64(n)).Inspect() }

// supervisorLinks — pid детей по which_children для каждого супервизора
// в снимке. Порядок — порядок старта. Звать с горутины цикла.
func (s *Session) supervisorLinks(snaps []vm.ActorSnapshot) (map[int][]int, error) {
	fn := s.vm.Global("Supervisor.which_children")
	if fn.Kind != runtime.KindFunction && fn.Kind != runtime.KindClosure {
		return nil, fmt.Errorf("internal: Supervisor.which_children is missing")
	}
	links := make(map[int][]int)
	for _, a := range snaps {
		if a.InitialFn != actorview.SupervisorInitialFn {
			continue
		}
		v, err := s.vm.Scheduler().CallNested(fn, []runtime.Value{{Kind: runtime.KindPid, Pid: a.Pid}})
		if err != nil {
			return nil, err
		}
		pids, err := childPids(v)
		if err != nil {
			return nil, err
		}
		links[a.Pid] = pids
	}
	return links, nil
}

func childPids(v runtime.Value) ([]int, error) {
	if v.Kind != runtime.KindList {
		return nil, fmt.Errorf("internal: which_children is %s", v.Inspect())
	}
	out := make([]int, 0, v.Len())
	for _, item := range v.Elems() {
		if item.Kind != runtime.KindTuple || len(item.Tuple) < 2 || item.Tuple[1].Kind != runtime.KindPid {
			return nil, fmt.Errorf("internal: which_children row is %s", item.Inspect())
		}
		out = append(out, item.Tuple[1].Pid)
	}
	return out, nil
}

// infoActor — info(pid). Хелпер i — другой: он печатает вид значения.
func (s *Session) infoActor(v runtime.Value) (runtime.Value, error) {
	if v.Kind != runtime.KindPid {
		return runtime.Unit, raiseType("info", v)
	}
	return s.writeInfo(s.vm.Scheduler().ActorInfoValue(v.Pid))
}

func (s *Session) writeInfo(got runtime.Value) (runtime.Value, error) {
	if isNone(got) {
		return runtime.Unit, s.writeOut("not alive\n")
	}
	rec, ok := someArg(got)
	if !ok {
		return runtime.Unit, fmt.Errorf("internal: Actor.info is %s", got.Inspect())
	}
	text := Format(rec, Print{Pal: s.pal, Env: s.HighlightEnv()})
	return runtime.Unit, s.writeOut("%s\n", text)
}

func (s *Session) top(v runtime.Value) (runtime.Value, error) {
	n, err := atLeastOne("top", v)
	if err != nil {
		return runtime.Unit, err
	}
	snap := s.vm.Scheduler().SnapshotHere()
	sort.Slice(snap, func(i, j int) bool {
		if snap[i].Reductions != snap[j].Reductions {
			return snap[i].Reductions > snap[j].Reductions
		}
		return snap[i].Pid < snap[j].Pid
	})
	if n > len(snap) {
		n = len(snap)
	}
	opt := Print{Pal: s.pal, Env: s.HighlightEnv()}
	var b strings.Builder
	for _, a := range snap[:n] {
		pid := runtime.Value{Kind: runtime.KindPid, Pid: a.Pid}
		got := s.vm.Scheduler().ActorInfoValue(a.Pid)
		if isNone(got) {
			fmt.Fprintf(&b, "%s\nnot alive\n", pid.Inspect())
			continue
		}
		rec, ok := someArg(got)
		if !ok {
			return runtime.Unit, fmt.Errorf("internal: Actor.info is %s", got.Inspect())
		}
		fmt.Fprintf(&b, "%s\n%s\n", pid.Inspect(), Format(rec, opt))
	}
	return runtime.Unit, s.writeOut("%s", b.String())
}

// atLeastOne — Int ≥ 1. Большое целое, которое не влезает в int,
// означает «все акторы»: живых меньше.
func atLeastOne(op string, v runtime.Value) (int, error) {
	if v.Kind != runtime.KindInt {
		return 0, raiseType(op, v)
	}
	b := v.AsBig()
	if b == nil || b.Sign() < 1 {
		return 0, raiseType(op, v)
	}
	if !b.IsInt64() {
		return math.MaxInt, nil
	}
	n := b.Int64()
	if n < 1 {
		return 0, raiseType(op, v)
	}
	if n > int64(math.MaxInt) {
		return math.MaxInt, nil
	}
	return int(n), nil
}

func isNone(v runtime.Value) bool {
	return v.Kind == runtime.KindVariant && v.Variant != nil && v.Variant.Tag == "None" && len(v.Variant.Args) == 0
}

func someArg(v runtime.Value) (runtime.Value, bool) {
	if v.Kind != runtime.KindVariant || v.Variant == nil || v.Variant.Tag != "Some" || len(v.Variant.Args) != 1 {
		return runtime.Unit, false
	}
	return v.Variant.Args[0], true
}

// MaskObserverCounters заменяет счётчики Actor.info на «_».
// reductions и alloc_bytes меняются от правки компилятора или VM;
// golden-сессии observer сравнивают уже замаскированный текст.
func MaskObserverCounters(s string) string {
	return observerCounters.ReplaceAllString(s, "${1}: _")
}

var observerCounters = regexp.MustCompile(`(reductions|alloc_bytes|turn_reductions|turn_alloc_bytes): -?\d+`)
