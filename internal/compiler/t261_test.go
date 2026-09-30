package compiler_test

import "testing"

// T-261: локальная привязка с именем акторного примитива (§12.6)
// затеняет его, как любое имя прелюдии (§11.5): вызов идёт в значение
// привязки, опкод примитива не эмитится, ящик вызывающего не трогается.
func TestLocalBindingShadowsActorPrimitive(t *testing.T) {
	runModule(t, `module Main

fn main() ->
    send = (a, b) -> (:mine, a, b)
    match send(self(), 2)
        (:mine, p, 2) -> assert(p == self())
    spawn = x -> x + 1
    assert(spawn(1) == 2)
    reply = (x) -> x * 2
    assert(reply(21) == 42)
    assert(mailbox_size() == 0)
`)
}

// Параметр функции и лямбды, имя из паттерна, локальная fn и захват
// в замыкании затеняют примитив так же; `Prelude.send(…)` — по-прежнему
// примитив.
func TestLocalBindingShadowsActorPrimitiveForms(t *testing.T) {
	runModule(t, `module Main

fn call_param(send) -> send(1, 2)

fn main() ->
    assert(call_param((a, b) -> a + b) == 3)
    f = spawn -> spawn(10)
    assert(f(x -> x * 3) == 30)
    (exit, _) = (x -> (:no_exit, x), 0)
    assert(exit(:kill) == (:no_exit, :kill))
    match (y -> y - 1)
        watch -> assert(watch(5) == 4)
    send = (a, b) -> a + b
    g = () -> send(40, 2)
    assert(g() == 42)
    assert(Prelude.send(self(), :hi) == Ok(()))
    assert(mailbox_size() == 1)
    assert(local_fn() == 7)

fn local_fn() ->
    fn self() -> 7
    self()
`)
}
