package repl

import (
	"fmt"
	"io"

	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/vm"
)

// trace(pid) / untrace(pid) (§11.4, T-174): подписка на [:vm, :message]
// с фильтром по pid, печать в stderr. Второго механизма рядом с
// Telemetry нет: в программе то же делает Telemetry.attach.

// traceTeleID — id подписки трассировки pid: свой на каждый pid.
func traceTeleID(pid runtime.Value) runtime.Value {
	return runtime.Tuple(runtime.Str("<trace>"), pid)
}

func (s *Session) trace(pid runtime.Value) (runtime.Value, error) {
	if pid.Kind != runtime.KindPid {
		return runtime.Unit, traceTypeErr("trace", pid)
	}
	w := s.errOut
	handler := runtime.Func(&runtime.FuncValue{
		Name: "trace", Arity: 3, IsNative: true,
		Native: func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
			if line, ok := traceLine(pid.Pid, args[0], args[2]); ok {
				_, _ = io.WriteString(w, line)
			}
			return runtime.Unit, nil
		},
	})
	prefix := runtime.List(runtime.Atom("vm"), runtime.Atom("message"))
	res, err := s.callNative("Telemetry.attach", []runtime.Value{traceTeleID(pid), prefix, handler})
	if err != nil {
		return runtime.Unit, err
	}
	// Повторный trace того же pid — подписка уже стоит.
	if !isOkUnit(res) && !isAlreadyExists(res) {
		return runtime.Unit, fmt.Errorf("internal: Telemetry.attach returned %s", res.Inspect())
	}
	return runtime.Unit, nil
}

func (s *Session) untrace(pid runtime.Value) (runtime.Value, error) {
	if pid.Kind != runtime.KindPid {
		return runtime.Unit, traceTypeErr("untrace", pid)
	}
	_, err := s.callNative("Telemetry.detach", []runtime.Value{traceTeleID(pid)})
	return runtime.Unit, err
}

// traceLine — строка события сообщения, если в нём участвует pid как
// отправитель (send) или получатель, взявший сообщение (recv):
// `trace #<pid 3>: recv (:ping, 1)`, `trace #<pid 3>: send (:ping, 1) to #<pid 4>`.
func traceLine(pid int, event, meta runtime.Value) (string, bool) {
	if event.Len() != 3 {
		return "", false
	}
	kind := event.At(2)
	msg, _ := recField(meta, "msg")
	who := runtime.Value{Kind: runtime.KindPid, Pid: pid}
	switch kind.Atom {
	case "send":
		from, _ := recField(meta, "from")
		if from.Kind != runtime.KindPid || from.Pid != pid {
			return "", false
		}
		to, _ := recField(meta, "pid")
		return fmt.Sprintf("trace %s: send %s to %s\n", who.Inspect(), msg.Inspect(), to.Inspect()), true
	case "recv":
		p, _ := recField(meta, "pid")
		if p.Kind != runtime.KindPid || p.Pid != pid {
			return "", false
		}
		return fmt.Sprintf("trace %s: recv %s\n", who.Inspect(), msg.Inspect()), true
	}
	return "", false
}

func traceTypeErr(op string, v runtime.Value) error {
	return &vm.ErrRaise{Val: runtime.Tuple(runtime.Atom("type_error"),
		runtime.Tuple(runtime.Atom(op), v))}
}
