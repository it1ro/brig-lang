package vm_test

import "testing"

// linkPrelude — хелперы тестов link (§12.2): take берёт следующее
// сообщение (:down впереди ящика), empty проверяет, что ящик пуст,
// idle — актор, который ждёт в recv.
const linkPrelude = `module Main
fn take() ->
    recv
        m -> m
    after 1000 -> :timeout

fn empty() ->
    recv
        m -> m
    after 20 -> :empty

fn idle() ->
    recv
        _ -> idle()
`

// TestLinkOwnerDeathExitsChild — связь однонаправленная «владелец →
// ребёнок» (§12.2): смерть владельца делает ребёнку
// exit(child, (:linked_exit, reason)). Это не raise и не :kill: trap
// ребёнка причину не ловит, ensure выполняются, наблюдатели ребёнка
// получают :down с этой причиной.
func TestLinkOwnerDeathExitsChild(t *testing.T) {
	runModuleSync(t, linkPrelude+`
fn child(parent) ->
    r = trap
        ensure send(parent, :child_cleaned)
        send(parent, (:child, self()))
        idle()
    send(parent, (:child_trapped, r))

fn owner(parent) ->
    _ = spawn_linked(() -> child(parent))
    idle()

fn main() ->
    parent = self()
    (op, oref) = spawn_watched(() -> owner(parent))
    (:child, c) = take()
    cref = watch(c)
    assert(exit(op, :kill) == Ok(()))
    assert(take() == (:down, oref, :kill))
    assert(take() == (:down, cref, (:linked_exit, :kill)))
    assert(take() == :child_cleaned)
    assert(empty() == :empty)
    assert(Actor.info(c) == None)
    :ok
`)
}

// TestLinkAdoptsExistingActor — link(pid) делает владельцем вызывающего
// (§12.2): ребёнок, заспавненный кем-то другим, уходит вместе с новым
// владельцем. Причина владельца вкладывается как есть.
func TestLinkAdoptsExistingActor(t *testing.T) {
	runModuleSync(t, linkPrelude+`
fn child(parent) ->
    trap
        ensure send(parent, :child_cleaned)
        send(parent, (:child, self()))
        idle()

fn owner(parent, c) ->
    link(c)
    send(parent, :linked)
    idle()

fn main() ->
    parent = self()
    _ = spawn(() -> child(parent))
    (:child, c) = take()
    cref = watch(c)
    (op, oref) = spawn_watched(() -> owner(parent, c))
    assert(take() == :linked)
    exit(op, (:shutdown, :deploy))
    assert(take() == (:down, oref, (:shutdown, :deploy)))
    assert(take() == (:down, cref, (:linked_exit, (:shutdown, :deploy))))
    assert(take() == :child_cleaned)
    :ok
`)
}

// TestLinkIsNotWatch — link больше не синоним watch (§12.2): владелец
// не наблюдает за ребёнком, связь не видна в watchers/watching, смерть
// ребёнка владельцу ничего не шлёт и его не завершает.
func TestLinkIsNotWatch(t *testing.T) {
	runModuleSync(t, linkPrelude+`
fn dying(parent) ->
    send(parent, :child_done)

fn main() ->
    parent = self()
    c = spawn_linked(idle)
    Some(me) = Actor.info(self())
    Some(ci) = Actor.info(c)
    assert(me.watching == [])
    assert(ci.watchers == [])
    d = spawn_linked(() -> dying(parent))
    assert(take() == :child_done)
    assert(empty() == :empty)
    assert(Actor.info(d) == None)
    link(d)
    assert(empty() == :empty)
    :ok
`)
}

// TestLinkTypeError — тег ошибки link — свой, не watch (G-26).
func TestLinkTypeError(t *testing.T) {
	runModuleSync(t, linkPrelude+`
fn main() ->
    assert(trap(link(42)) == Error((:type_error, (:link, 42))))
    assert(trap(link(:nope)) == Error((:type_error, (:link, :nope))))
    assert(trap(spawn_linked(:nope)) == Error((:type_error, (:call, :nope))))
    :ok
`)
}

// TestLinkChainExitsGrandchild — связь передаётся по дереву: каждый
// владелец завершает своих детей, причина вкладывается на каждом шаге.
func TestLinkChainExitsGrandchild(t *testing.T) {
	runModuleSync(t, linkPrelude+`
fn grandchild(parent) ->
    send(parent, (:grandchild, self()))
    idle()

fn child(parent) ->
    _ = spawn_linked(() -> grandchild(parent))
    send(parent, (:child, self()))
    idle()

fn owner(parent) ->
    _ = spawn_linked(() -> child(parent))
    idle()

fn main() ->
    parent = self()
    op = spawn(() -> owner(parent))
    (:child, c) = take()
    (:grandchild, g) = take()
    cref = watch(c)
    gref = watch(g)
    exit(op, :stop)
    assert(take() == (:down, cref, (:linked_exit, :stop)))
    assert(take() == (:down, gref, (:linked_exit, (:linked_exit, :stop))))
    assert(Actor.list() == [self()])
    :ok
`)
}
