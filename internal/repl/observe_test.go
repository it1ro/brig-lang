package repl

import (
	"bytes"
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/observe"
	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/vm"
)

func evalOK(t *testing.T, s *Session, src string) runtime.Value {
	t.Helper()
	res, err := s.Eval(src)
	if err != nil {
		t.Fatalf("eval %q: %v", src, err)
	}
	if len(res) == 0 {
		t.Fatalf("eval %q: no result", src)
	}
	return res[len(res)-1].Value
}

func TestObserveOnceFrame(t *testing.T) {
	var out bytes.Buffer
	s := New(vm.New(), &out)
	defer s.Close()
	v, err := s.observeOnce()
	if err != nil {
		t.Fatal(err)
	}
	if v.Kind != runtime.KindUnit {
		t.Fatalf("result %s", v.Inspect())
	}
	got := out.String()
	if !strings.Contains(got, "brig observe") || strings.Contains(got, "\x1b[?1049") {
		t.Fatalf("frame:\n%s", got)
	}
}

func TestObserveHelp(t *testing.T) {
	var out bytes.Buffer
	s := New(vm.New(), &out)
	defer s.Close()
	evalOK(t, s, "h(observe)\n")
	got := out.String()
	for _, frag := range []string{"observe()", "observe is already open", ":crash", ":down"} {
		if !strings.Contains(got, frag) {
			t.Fatalf("h(observe) missing %q:\n%s", frag, got)
		}
	}
	out.Reset()
	evalOK(t, s, "h(Repl)\n")
	if !strings.Contains(out.String(), "observe/0") {
		t.Fatalf("h(Repl):\n%s", out.String())
	}
}

func TestObserveAlreadyOpen(t *testing.T) {
	var out bytes.Buffer
	s := New(vm.New(), &out)
	defer s.Close()
	var ok bool
	if err := s.vm.Scheduler().Sync(func() error {
		var err error
		ok, err = s.beginObserve(&observe.Ring{})
		return err
	}); err != nil || !ok {
		t.Fatalf("attach %v %v", ok, err)
	}
	defer func() {
		_ = s.vm.Scheduler().Sync(func() error {
			s.detachObserve()
			return nil
		})
	}()

	out.Reset()
	if err := s.vm.Scheduler().Sync(func() error {
		var err error
		ok, err = s.beginObserve(&observe.Ring{})
		return err
	}); err != nil || ok {
		t.Fatalf("second attach ok=%v err=%v", ok, err)
	}
	if !strings.Contains(out.String(), "observe is already open") {
		t.Fatalf("message:\n%s", out.String())
	}

	again := evalOK(t, s, "Telemetry.attach(\"<observe>\", [:vm], (e, m, meta) -> ())\n")
	if again.Inspect() != "Error(:already_exists)" {
		t.Fatalf("same id %s", again.Inspect())
	}
	other := evalOK(t, s, "Telemetry.attach(:other, [:app], (e, m, meta) -> ())\n")
	if other.Inspect() != "Ok(())" {
		t.Fatalf("other %s", other.Inspect())
	}
	if err := s.vm.Scheduler().Sync(func() error {
		s.detachObserve()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	kept := evalOK(t, s, "Telemetry.attach(:other, [:app], (e, m, meta) -> ())\n")
	if kept.Inspect() != "Error(:already_exists)" {
		t.Fatalf("other was detached: %s", kept.Inspect())
	}
	mine := evalOK(t, s, "Telemetry.attach(\"<observe>\", [:vm], (e, m, meta) -> ())\n")
	if mine.Inspect() != "Ok(())" {
		t.Fatalf("reattach %s", mine.Inspect())
	}
}

func TestObserveCrashRing(t *testing.T) {
	var out bytes.Buffer
	s := New(vm.New(), &out)
	defer s.Close()
	ring := &observe.Ring{}
	if err := s.vm.Scheduler().Sync(func() error {
		ok, err := s.beginObserve(ring)
		if err != nil {
			return err
		}
		if !ok {
			return errAlready
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = s.vm.Scheduler().Sync(func() error {
			s.detachObserve()
			return nil
		})
	}()
	evalOK(t, s, "fn boom() -> raise(:boom)\n")
	evalOK(t, s, "spawn(boom)\n")
	evalOK(t, s, "recv\n    _ -> ()\nafter 300 -> ()\n")
	list := ring.List()
	found := false
	for _, c := range list {
		if strings.Contains(c.Reason, ":boom") {
			found = true
		}
	}
	if !found {
		t.Fatalf("ring %+v", list)
	}
}

var errAlready = errString("observe already open")

type errString string

func (e errString) Error() string { return string(e) }
