package vm_test

import (
	"testing"
)

// TestTelemetryMessageEvents — T-174: [:vm, :message, :send] излучает
// отправитель с { from, pid, msg } (pid — получатель), [:vm, :message, :recv] — получатель,
// взявший сообщение, с { pid, msg }; измерения { monotonic_ms }. Send
// обработчика события не порождает (§12.14).
func TestTelemetryMessageEvents(t *testing.T) {
	runModuleSync(t, `module Main
fn echo(main) ->
    recv
        m -> send(main, (:pong, m))

# Только события с участием w (send — от w или к w, recv — в w): recv самого main и доставка событий в
# main сюда не попадают.
fn note(me, w, e, meta) ->
    if meta.from == w or meta.pid == w then send(me, (:evt, e, meta)) else ()
fn note_recv(me, w, e, meta) ->
    if meta.pid == w then send(me, (:evt, e, meta)) else ()

fn main() ->
    me = self()
    w = spawn(fn () -> echo(me))
    Ok(()) = Telemetry.attach(:send, [:vm, :message, :send], (e, m, meta) -> note(me, w, e, meta))
    Ok(()) = Telemetry.attach(:recv, [:vm, :message, :recv], (e, m, meta) -> note_recv(me, w, e, meta))
    Ok(()) = Telemetry.attach(:meas, [:vm, :message], fn (e, m, meta) ->
        assert(m.monotonic_ms >= 0))
    assert(send(w, :ping) == Ok(()))
    (:evt, e1, s1) = recv
        x -> x
    assert(e1 == [:vm, :message, :send])
    assert(s1 == { from: me, pid: w, msg: :ping })
    (:evt, e2, r2) = recv
        x -> x
    assert(e2 == [:vm, :message, :recv])
    assert(r2 == { pid: w, msg: :ping })
    pong = recv
        x -> x
    assert(pong == (:pong, :ping))
    (:evt, e3, s3) = recv
        x -> x
    assert(e3 == [:vm, :message, :send])
    assert(s3 == { from: w, pid: me, msg: (:pong, :ping) })
    Telemetry.detach(:send)
    Telemetry.detach(:recv)
    Telemetry.detach(:meas)
    w2 = spawn(fn () -> echo(me))
    _ = send(w2, :quiet)
    q = recv
        x -> x
    assert(q == (:pong, :quiet))
    late = recv
        x -> x
    after 20 -> :none
    assert(late == :none)
    :ok
`)
}

// msgRunner — цикл send себе + recv; без подписок события сообщений не
// аллоцируют (§12.14). msgAllocsBase — аллокации итерации на main до
// событий (d52f1fd): ящик и кадры, не телеметрия.
const msgRunner = `module Main
fn loop(n) ->
    if n == 0 then :ok else step(n)

fn step(n) ->
    _ = send(self(), :m)
    recv
        _ -> ()
    loop(n - 1)

fn main() ->
    loop(NLOOP)
`

const msgAllocsBase = 3

func TestTelemetryMessageNoSubscribersCost(t *testing.T) {
	teleRunners["msg"] = msgRunner
	const n = 1000
	small := testing.AllocsPerRun(20, newTeleRun(t, "msg", n))
	big := testing.AllocsPerRun(20, newTeleRun(t, "msg", 2*n))
	if per := (big - small) / n; per > msgAllocsBase+0.01 {
		t.Errorf("allocs per send+recv without subscribers = %.3f (n: %.0f, 2n: %.0f), want %d as before the events",
			per, small, big, msgAllocsBase)
	}
}
