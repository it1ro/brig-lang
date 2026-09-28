package repl

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/it1ro/brig-lang/internal/runtime"
)

// tree/info/top (§11.4). Сбор дерева, которому нужен блокирующий
// which_children, — Observer.nodes на Brig (CallNested умеет await).
// Печать — здесь, pretty-выводом T-204 для записей Actor.info.

func (s *Session) tree() (runtime.Value, error) {
	fn := s.vm.Global("Observer.nodes")
	if fn.Kind != runtime.KindFunction && fn.Kind != runtime.KindClosure {
		return runtime.Unit, errors.New("internal: Observer.nodes is missing")
	}
	v, err := s.vm.Scheduler().CallNested(fn, nil)
	if err != nil {
		return runtime.Unit, err
	}
	var b strings.Builder
	if err := writeActorTree(&b, v, 0); err != nil {
		return runtime.Unit, err
	}
	if err := s.writeOut("%s", b.String()); err != nil {
		return runtime.Unit, err
	}
	return runtime.Unit, nil
}

func writeActorTree(b *strings.Builder, v runtime.Value, depth int) error {
	if v.Kind != runtime.KindList {
		return fmt.Errorf("internal: observer tree is %s", v.Inspect())
	}
	for _, node := range v.List {
		if err := writeActorNode(b, node, depth); err != nil {
			return err
		}
	}
	return nil
}

func writeActorNode(b *strings.Builder, node runtime.Value, depth int) error {
	pid, err := recField(node, "pid")
	if err != nil {
		return err
	}
	name, err := recField(node, "name")
	if err != nil {
		return err
	}
	status, err := recField(node, "status")
	if err != nil {
		return err
	}
	mailbox, err := recField(node, "mailbox")
	if err != nil {
		return err
	}
	children, err := recField(node, "children")
	if err != nil {
		return err
	}
	fmt.Fprintf(b, "%s%s name=%s status=%s mailbox=%s\n",
		strings.Repeat("  ", depth), pid.Inspect(), name.Inspect(), status.Inspect(), mailbox.Inspect())
	return writeActorTree(b, children, depth+1)
}

func recField(node runtime.Value, name string) (runtime.Value, error) {
	if node.Kind != runtime.KindRecord || node.Record == nil {
		return runtime.Unit, fmt.Errorf("internal: observer node is %s", node.Inspect())
	}
	for _, f := range node.Record.Fields {
		if f.Name == name {
			return f.Val, nil
		}
	}
	return runtime.Unit, fmt.Errorf("internal: observer node has no %s", name)
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
		return runtime.Unit, s.writeOut("не жив\n")
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
			fmt.Fprintf(&b, "%s\nне жив\n", pid.Inspect())
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
