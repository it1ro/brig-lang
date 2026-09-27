package compiler_test

import "testing"

// T-131 (T-121 п.1 = C): ошибка внутри guard пробрасывается как есть,
// без обёртки (:guard_failed, …).
func TestGuardErrorPolicy(t *testing.T) {
	runModule(t, `module Main
fn f(x) when 1 / x > 0 -> :ok
fn f(_) -> :other
fn main() ->
    r = trap(f(0))
    assert(r == Error((:division_by_zero, ())))
`)
}

// T-131 (T-121 п.3 = A): Str + Str — :type_error; конкатенация — интерполяция.
func TestStrPlusPolicy(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    a = "a"
    b = "b"
    r = trap(a + b)
    assert(r == Error((:type_error, (:add, (a, b)))))
    c = "\(a)\(b)"
    assert(c == "ab")
`)
}

// T-131 (T-121 п.4): отсутствующее поле — (:no_field, (name, rec)).
func TestMissingFieldErrorName(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    a = { id: 1 }
    r = trap(a.nope)
    assert(r == Error((:no_field, (:nope, a))))
`)
}

// T-131 (T-121 п.2): mailbox_size возвращает Int; для мёртвого pid — 0.
func TestMailboxSizeDead(t *testing.T) {
	runModule(t, `module Main
fn quick() -> ()
fn main() ->
    p = spawn(quick)
    recv
        :never -> :never
    after 20 -> ()
    assert(mailbox_size(p) == 0)
`)
}
