package compiler_test

import "testing"

// T-75, §12.2 (T-252): link(pid) делает вызывающего владельцем pid и
// возвращает (). Это не watch: падение ребёнка владельцу ничего не
// шлёт, :down приходит только от своего watch.
func TestLinkMakesCallerOwner(t *testing.T) {
	runModule(t, `module Main
fn boom() -> raise(:boom)
fn main() ->
    p = spawn(boom)
    ref = watch(p)
    r = link(p)
    assert(r == ())
    reason = recv
        (:down, got, why) when got == ref -> why
    assert(reason == (:raise, :boom))
    assert(mailbox_size() == 0)
`)
}

// T-75: mailbox_size() без аргументов — своя очередь; == mailbox_size(self()).
func TestMailboxSizeNoArgs(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    assert(mailbox_size() == 0)
    send(self(), :a)
    send(self(), :b)
    assert(mailbox_size() == 2)
    assert(mailbox_size() == mailbox_size(self()))
`)
}
