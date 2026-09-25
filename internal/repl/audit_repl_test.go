package repl_test

// Регресс-тест для O-F1 (T-05, issue #5): CompileReplLine не
// инициализировал Compiler.image, из-за чего repl.go:81
// (`c.Image().Functions`) паниковал nil-разыменованием на любой строке.
//
// Тест-якорь: `x=5; f=()->x; x=10; f()==5` — три строки REPL-сессии,
// проверяющие персистентность окружения и лексический снимок захвата
// (N12): f, созданная до переопределения x, должна видеть старое
// значение.

import (
	"bytes"
	"testing"

	"github.com/it1ro/brig-lang/internal/repl"
	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/vm"
)

func TestAuditReplSnapshot(t *testing.T) {
	var out bytes.Buffer
	r := repl.New(vm.New(), &out)

	if _, err := r.Eval("x = 5\n"); err != nil {
		t.Fatalf("eval `x = 5`: %v (diag: %s)", err, out.String())
	}
	if _, err := r.Eval("f = () -> x\n"); err != nil {
		t.Fatalf("eval `f = () -> x`: %v (diag: %s)", err, out.String())
	}
	if _, err := r.Eval("x = 10\n"); err != nil {
		t.Fatalf("eval `x = 10`: %v (diag: %s)", err, out.String())
	}
	res, err := r.Eval("f() == 5\n")
	if err != nil {
		t.Fatalf("eval `f() == 5`: %v (diag: %s)", err, out.String())
	}
	if res.Kind != runtime.KindBool || !res.Bool {
		t.Fatalf("f() == 5: got %s, want true (lexical snapshot of x=5)", res.Inspect())
	}
}
