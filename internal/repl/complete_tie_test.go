package repl

import (
	"io"
	"testing"

	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/vm"
)

// TestCompleteRecordShadowsModule — имя модуля, связанное с записью,
// дополняется полями; связанное не с записью модуль не прячет.
// Верхний регистр слева от `=` в REPL — не привязка, поэтому значение
// кладётся в окружение сессии напрямую.
func TestCompleteRecordShadowsModule(t *testing.T) {
	s := New(vm.New(), io.Discard)
	t.Cleanup(s.Close)

	s.env["List"] = runtime.Record("", []runtime.RecordField{
		{Name: "id", Val: runtime.Int(1)},
	})
	c := s.Complete("List.", len("List."))
	if !hasInsert(c, "id") || hasInsert(c, "take") {
		t.Fatalf("record should hide the module: %+v", c.Candidates)
	}

	s.env["List"] = runtime.Int(1)
	c = s.Complete("List.ta", len("List.ta"))
	if !hasInsert(c, "take") {
		t.Fatalf("non-record binding hid List.take: %+v", c.Candidates)
	}
	if c.From != len("List.") || c.To != len("List.ta") {
		t.Fatalf("span %d:%d", c.From, c.To)
	}
}

func hasInsert(c Completion, insert string) bool {
	for _, cand := range c.Candidates {
		if cand.Insert == insert {
			return true
		}
	}
	return false
}
