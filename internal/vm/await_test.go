package vm_test

import (
	"testing"
	"time"
)

// awaitPrelude — хелперы тестов await/reply (§12.9) поверх exitPrelude.
// server отвечает на (:get, from, ref) значением v; перед ответом шлёт
// вызывающему :tick в ящик.
const awaitPrelude = exitPrelude + `
fn server(v) ->
    recv
        (:get, from, ref) ->
            send(from, :tick)
            reply(from, ref, v)
            server(v)
`

// TestAwaitReplyBypassesMailbox — ответ reply приходит в слот ref, а не в
// ящик: :tick, пришедший до ответа, остаётся в ящике и берётся следующим
// recv; ответ до await хранится; побеждает первый ответ (§12.9).
func TestAwaitReplyBypassesMailbox(t *testing.T) {
	runModuleSync(t, awaitPrelude+`
fn main() ->
    srv = spawn(() -> server(42))
    ref = make_ref()
    send(srv, (:get, self(), ref))
    assert(await(ref, 1000) == Ok(42))
    assert(mailbox_size() == 1)
    assert(take() == :tick)
    assert(empty() == :empty)

    early = make_ref()
    assert(reply(self(), early, 1) == ())
    reply(self(), early, 2)
    assert(mailbox_size() == 0)
    assert(await(early, 0) == Ok(1))
    assert(await(early, 0) == Error(:timeout))
    :ok
`)
}

// TestAwaitTimeout — нет ответа: Error(:timeout) через timeout мс; слот
// закрывается, повторный await сразу Error(:timeout); неверные аргументы —
// :type_error (§12.9).
func TestAwaitTimeout(t *testing.T) {
	runModuleSync(t, exitPrelude+`
fn main() ->
    ref = make_ref()
    assert(await(ref, 30) == Error(:timeout))
    assert(await(ref, 5000) == Error(:timeout))
    assert(await(make_ref(), 0) == Error(:timeout))

    r = trap
        await(ref, -1)
    assert(r == Error((:type_error, (:await, -1))))
    r = trap
        await(ref, :soon)
    assert(r == Error((:type_error, (:await, :soon))))
    r = trap
        await(:ref, 10)
    assert(r == Error((:type_error, (:await, :ref))))
    pid = spawn(() -> idle())
    wref = watch(pid)
    r = trap
        await(wref, 10)
    assert(r == Error((:type_error, (:await, wref))))
    r = trap
        reply(1, ref, 0)
    assert(r == Error((:type_error, (:reply, 1))))
    r = trap
        reply(self(), :ref, 0)
    assert(r == Error((:type_error, (:reply, :ref))))
    :ok
`)
}

// TestAwaitTimeoutWaits — await действительно ждёт timeout, а не
// возвращается сразу.
func TestAwaitTimeoutWaits(t *testing.T) {
	start := time.Now()
	runModuleSync(t, exitPrelude+`
fn main() ->
    assert(await(make_ref(), 50) == Error(:timeout))
`)
	if d := time.Since(start); d < 50*time.Millisecond {
		t.Fatalf("await(ref, 50) вернулся через %v", d)
	}
}

// TestAwaitLateReplyDropped — ответ после таймаута не попадает ни в слот,
// ни в ящик; ответ чужому или мёртвому pid отбрасывается (§12.9).
func TestAwaitLateReplyDropped(t *testing.T) {
	runModuleSync(t, exitPrelude+`
fn late(from, ref) ->
    empty()
    reply(from, ref, :late)
    send(from, :done)

fn main() ->
    me = self()
    ref = make_ref()
    spawn(() -> late(me, ref))
    assert(await(ref, 5) == Error(:timeout))
    assert(take() == :done)
    assert(mailbox_size() == 0)
    assert(await(ref, 0) == Error(:timeout))

    other = spawn(() -> idle())
    ref2 = make_ref()
    reply(other, ref2, :wrong_pid)
    assert(await(ref2, 0) == Error(:timeout))

    (dead, dref) = spawn_watched(() -> :done)
    assert(take() == (:down, dref, :normal))
    reply(dead, make_ref(), :dead)

    wref = watch(other)
    reply(self(), wref, :watch_ref)
    assert(mailbox_size() == 0)
    :ok
`)
}

// TestAwaitDoesNotConsumeOtherMessages — пока актор ждёт в await,
// сообщения и :down копятся по обычным правилам и берутся следующим recv:
// :down впереди, остальные в порядке отправки (§12.9, G1, G2).
func TestAwaitDoesNotConsumeOtherMessages(t *testing.T) {
	runModuleSync(t, exitPrelude+`
fn chatty(from, ref) ->
    send(from, :a)
    send(from, :b)
    empty()
    send(from, :c)
    reply(from, ref, :answer)

fn main() ->
    me = self()
    ref = make_ref()
    (pid, wref) = spawn_watched(() -> chatty(me, ref))
    assert(await(ref, 1000) == Ok(:answer))
    assert(take() == (:down, wref, :normal))
    assert(take() == :a)
    assert(take() == :b)
    assert(take() == :c)
    assert(empty() == :empty)
    :ok
`)
}

// TestAwaitHwmUnaffected — слот не учитывается в HWM и не виден
// mailbox_size: ответ доходит до актора с полным ящиком (§12.9, G2).
func TestAwaitHwmUnaffected(t *testing.T) {
	runModuleSync(t, awaitPrelude+`
fn fill(0) -> ()
fn fill(n) ->
    assert(send(self(), n) == Ok(()))
    fill(n - 1)

fn main() ->
    fill(64)
    assert(send(self(), 0) == Error(:busy))
    ref = make_ref()
    reply(self(), ref, :full)
    assert(mailbox_size() == 64)
    assert(await(ref, 0) == Ok(:full))

    srv = spawn(() -> server(7))
    ref2 = make_ref()
    send(srv, (:get, self(), ref2))
    assert(await(ref2, 1000) == Ok(7))
    assert(mailbox_size(self()) == 64)
    :ok
`)
}

// TestAwaitInterruptedByExit — await — точка ожидания: exit будит
// ждущего и завершает его с причиной сигнала (§12.7, §12.9).
func TestAwaitInterruptedByExit(t *testing.T) {
	start := time.Now()
	runModuleSync(t, exitPrelude+`
fn waiter() ->
    await(make_ref(), 5000)
    :finished

fn main() ->
    (pid, ref) = spawn_watched(waiter)
    empty()
    exit(pid, :stop)
    assert(take() == (:down, ref, :stop))
    :ok
`)
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("exit не прервал await: %v", d)
	}
}
