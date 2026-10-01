package vm_test

import "testing"

// TestFanInDeliversAll — проба a2.brig (X-2): 1 000 воркеров отвечают
// родителю, который разбирает ящик позже. HWM по умолчанию (10 000)
// держит весь fan-in, ни один ответ не теряется (§12.2).
func TestFanInDeliversAll(t *testing.T) {
	runModuleSync(t, `module Main
fn worker(parent) ->
    recv
        (:ping, n) ->
            Ok(()) = send(parent, (:pong, n))
            worker(parent)

fn spawn_n(0, me) -> :ok
fn spawn_n(n, me) ->
    pid = spawn(() -> worker(me))
    Ok(()) = send(pid, (:ping, 1))
    spawn_n(n - 1, me)

fn collect(0, sum) -> sum
fn collect(n, sum) ->
    recv
        (:pong, k) -> collect(n - 1, sum + k)
    after 10000 -> (:timeout, n, sum)

fn main() ->
    spawn_n(1000, self())
    assert(collect(1000, 0) == 1000)
    :ok
`)
}

// TestSpawnMailboxHwmLimit — поле лимитов mailbox_hwm задаёт порог на
// актора (§12.10): сверх порога send → Error(:busy), счётчик отказов
// виден в Actor.info как dropped, VM излучает [:vm, :mailbox, :hwm]
// (§12.14).
func TestSpawnMailboxHwmLimit(t *testing.T) {
	runModuleSync(t, `module Main
fn idle() ->
    await(make_ref(), 60000)

fn main() ->
    me = self()
    Ok(()) = Telemetry.attach(:hwm, [:vm, :mailbox, :hwm], (e, m, meta) -> send(me, (:evt, e, m, meta)))
    pid = spawn(() -> idle(), { mailbox_hwm: 2 })
    assert(send(pid, :a) == Ok(()))
    assert(send(pid, :b) == Ok(()))
    assert(send(pid, :c) == Error(:busy))
    Some(info) = Actor.info(pid)
    assert(info.mailbox == 2)
    assert(info.dropped == 1)
    (:evt, ev, m, meta) = recv
        x -> x
    assert(ev == [:vm, :mailbox, :hwm])
    assert(m.mailbox == 2)
    assert(m.hwm == 2)
    assert(meta.pid == pid)
    assert(meta.from == Some(me))
    :ok
`)
}
