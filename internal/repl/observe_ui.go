package repl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/it1ro/brig-lang/internal/highlight"
	"github.com/it1ro/brig-lang/internal/observe"
	"github.com/it1ro/brig-lang/internal/observe/run"
	"github.com/it1ro/brig-lang/internal/observe/view"
	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/vm"

	xterm "golang.org/x/term"
)

// observeTeleID — подписка ленты падений. Чужие id не совпадают со строкой.
var observeTeleID = runtime.Str("<observe>")

func (s *Session) observe() (runtime.Value, error) {
	in, out := s.termIn, s.termOut
	if in == nil {
		in = os.Stdin
	}
	if out == nil {
		out = os.Stdout
	}
	if xterm.IsTerminal(int(in.Fd())) && xterm.IsTerminal(int(out.Fd())) {
		return s.observeScreen(in, out)
	}
	return s.observeOnce()
}

func (s *Session) observeOnce() (runtime.Value, error) {
	snap := s.vm.Scheduler().SnapshotHere()
	links, err := s.supervisorLinks(snap)
	if err != nil {
		return runtime.Unit, err
	}
	m := observe.Update(observe.Model{}, observe.ShotMsg{Actors: snap, Links: links})
	frame := view.Render(m, 80, 24, false)
	return runtime.Unit, s.writeOut("%s\n", frame)
}

type shotResult struct {
	shot observe.Shot
	err  error
}

func (s *Session) observeScreen(in, out *os.File) (runtime.Value, error) {
	ring := &observe.Ring{}
	ok, err := s.beginObserve(ring)
	if err != nil || !ok {
		return runtime.Unit, err
	}
	defer s.detachObserve()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := make(chan chan shotResult, 1)

	var runErr error
	finished := make(chan struct{})
	go func() {
		runErr = run.Run(ctx, run.Deps{
			Snapshot: func() (observe.Shot, error) {
				ch := make(chan shotResult, 1)
				select {
				case <-ctx.Done():
					return observe.Shot{}, ctx.Err()
				case req <- ch:
					s.vm.Scheduler().Wake()
				}
				select {
				case <-ctx.Done():
					return observe.Shot{}, ctx.Err()
				case r := <-ch:
					if errors.Is(r.err, vm.ErrSessionClosed) {
						return observe.Shot{}, observe.ErrSessionClosed
					}
					return r.shot, r.err
				}
			},
			Crashes: ring.List,
			In:      in,
			Out:     out,
			TTY:     true,
			Color:   highlight.PaletteFromEnv(nil).Enabled(),
			Size: func() (int, int) {
				w, h, err := xterm.GetSize(int(out.Fd()))
				if err != nil || w <= 0 || h <= 0 {
					return 80, 24
				}
				return w, h
			},
		})
		close(finished)
	}()

	yerr := s.vm.Scheduler().YieldUntil(finished, cancel, func() { s.serveObserve(req) })
	switch {
	case errors.Is(yerr, vm.ErrInterrupted):
		return runtime.Unit, yerr
	case errors.Is(yerr, vm.ErrSessionClosed), errors.Is(runErr, observe.ErrSessionClosed):
		return runtime.Unit, s.writeOut("сессия закрыта\n")
	}
	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		return runtime.Unit, s.writeOut("observe: %s\n", runErr)
	}
	return runtime.Unit, nil
}

func (s *Session) serveObserve(req chan chan shotResult) {
	select {
	case ch := <-req:
		shot, err := s.takeShot()
		select {
		case ch <- shotResult{shot: shot, err: err}:
		default:
		}
	default:
	}
}

func (s *Session) takeShot() (observe.Shot, error) {
	snap := s.vm.Scheduler().SnapshotHere()
	links, err := s.supervisorLinks(snap)
	if err != nil {
		return observe.Shot{}, err
	}
	return observe.Shot{Actors: snap, Links: links}, nil
}

// beginObserve подписывается на падения. false — id занят, сообщение уже напечатано.
func (s *Session) beginObserve(ring *observe.Ring) (bool, error) {
	ok, err := s.attachObserve(ring)
	if err != nil || ok {
		return ok, err
	}
	return false, s.writeOut("observe уже открыт\n")
}

func (s *Session) attachObserve(ring *observe.Ring) (bool, error) {
	handler := runtime.Func(&runtime.FuncValue{
		Name: "observe", Arity: 3, IsNative: true,
		Native: func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
			if c, ok := crashFrom(args); ok {
				ring.Add(c)
			}
			return runtime.Unit, nil
		},
	})
	prefix := runtime.List(runtime.Atom("vm"), runtime.Atom("actor"))
	res, err := s.callNative("Telemetry.attach", []runtime.Value{observeTeleID, prefix, handler})
	if err != nil {
		return false, err
	}
	if isAlreadyExists(res) {
		return false, nil
	}
	if !isOkUnit(res) {
		return false, fmt.Errorf("internal: Telemetry.attach returned %s", res.Inspect())
	}
	return true, nil
}

func (s *Session) detachObserve() {
	_, _ = s.callNative("Telemetry.detach", []runtime.Value{observeTeleID})
}

func (s *Session) callNative(name string, args []runtime.Value) (runtime.Value, error) {
	fn := s.vm.Global(name)
	if fn.Kind != runtime.KindFunction || fn.Func == nil || fn.Func.Native == nil {
		return runtime.Unit, fmt.Errorf("internal: %s is missing", name)
	}
	return fn.Func.Native(s.vm, args)
}

func crashFrom(args []runtime.Value) (observe.Crash, bool) {
	if len(args) < 3 || args[0].Kind != runtime.KindList || args[0].Len() < 3 {
		return observe.Crash{}, false
	}
	ev := args[0].Elems()
	if ev[0].Kind != runtime.KindAtom || ev[1].Kind != runtime.KindAtom || ev[2].Kind != runtime.KindAtom {
		return observe.Crash{}, false
	}
	if ev[0].Atom != "vm" || ev[1].Atom != "actor" || (ev[2].Atom != "crash" && ev[2].Atom != "down") {
		return observe.Crash{}, false
	}
	meta := args[2]
	pid, ok := recField(meta, "pid")
	if !ok || pid.Kind != runtime.KindPid {
		return observe.Crash{}, false
	}
	reason, _ := recField(meta, "reason")
	name, _ := recField(meta, "name")
	return observe.Crash{
		At:     time.Now(),
		Pid:    pid.Pid,
		Name:   optName(name),
		Reason: reason.Inspect(),
	}, true
}

func optName(v runtime.Value) string {
	if v.Kind != runtime.KindVariant || v.Variant == nil || v.Variant.Tag != "Some" || len(v.Variant.Args) != 1 {
		return ""
	}
	a := v.Variant.Args[0]
	switch a.Kind {
	case runtime.KindAtom:
		return a.Atom
	case runtime.KindStr:
		return a.Str
	default:
		return a.Inspect()
	}
}

func recField(v runtime.Value, name string) (runtime.Value, bool) {
	if v.Kind != runtime.KindRecord || v.Record == nil {
		return runtime.Unit, false
	}
	for _, f := range v.Record.Fields {
		if f.Name == name {
			return f.Val, true
		}
	}
	return runtime.Unit, false
}

func isOkUnit(v runtime.Value) bool {
	return v.Kind == runtime.KindVariant && v.Variant != nil && v.Variant.Tag == "Ok"
}

func isAlreadyExists(v runtime.Value) bool {
	if v.Kind != runtime.KindVariant || v.Variant == nil || v.Variant.Tag != "Error" || len(v.Variant.Args) != 1 {
		return false
	}
	a := v.Variant.Args[0]
	return a.Kind == runtime.KindAtom && a.Atom == "already_exists"
}
