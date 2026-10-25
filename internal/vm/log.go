package vm

import (
	"fmt"
	"os"
	"strings"

	"github.com/it1ro/brig-lang/internal/runtime"
)

// ---- обработчик Log по умолчанию (§11.5, T-174) ----
//
// Модуль Log — на Brig (stdlib/log.brig): уровни и события [:log, level].
// Печать — этот натив: из Brig поля записи не перечислить.

var logDefaultID = runtime.Atom("log_default")

// AttachLogDefault подписывает печать Log на [:log] под id :log_default.
// Её ставит compiler.InstallStdlib; Telemetry.detach(:log_default) снимает.
// Занятый id — подписка уже стоит.
func (vm *VM) AttachLogDefault() error {
	h := runtime.Func(&runtime.FuncValue{
		Name: "Log.default_handler", Arity: 3, IsNative: true,
		Native: func(_ runtime.Caller, args []runtime.Value) (runtime.Value, error) {
			fmt.Fprintln(os.Stderr, FormatLogLine(args[0], args[2]))
			return runtime.Unit, nil
		},
	})
	_, err := vm.scheduler.teleAttach(logDefaultID, runtime.List(runtime.Atom("log")), h)
	return err
}

// FormatLogLine — строка `level: msg key=value …` события [:log, level]:
// поля meta по порядку записи, без msg и pid.
func FormatLogLine(event, meta runtime.Value) string {
	var b strings.Builder
	level := "log"
	if event.Len() >= 2 {
		if a := event.At(1); a.Kind == runtime.KindAtom {
			level = a.Atom
		}
	}
	b.WriteString(level)
	b.WriteByte(':')
	if meta.Kind != runtime.KindRecord || meta.Record == nil {
		return b.String()
	}
	if msg, ok := meta.Record.Get("msg"); ok {
		b.WriteByte(' ')
		b.WriteString(msg.Display())
	}
	for _, f := range meta.Record.Fields {
		if f.Name == "msg" || f.Name == "pid" {
			continue
		}
		b.WriteByte(' ')
		b.WriteString(f.Name)
		b.WriteByte('=')
		b.WriteString(f.Val.Display())
	}
	return b.String()
}
