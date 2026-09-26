package compiler_test

import "testing"

// T-75, §12: link(pid) — как watch, но без возвращаемого ref.
func TestLinkDeliversDown(t *testing.T) {
	runModule(t, `module Main
fn boom() -> raise(:boom)
fn main() ->
    p = spawn(boom)
    r = link(p)
    assert(r == ())
    reason = recv
        (:down, _, why) -> why
    assert(reason == (:raise, :boom))
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
