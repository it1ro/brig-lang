package vm_test

import "testing"

// Микро-бенчмарки горячих путей VM (T-152): `make bench` гоняет их с
// -benchmem по 10 повторов, CI сравнивает PR с main через benchstat
// (.github/workflows/bench.yml). Вызов лямбды нативом (map по 1000
// элементам, аллокации на вызов) — BenchmarkMapLambda (T-103).
//
// Программы получают размер через n() (см. newCallAllocRun), поэтому
// компиляция и константы не входят в замер.

const benchN = 1000

var benchProgs = map[string]string{
	// Не хвостовой CALL байткод-функции в цикле.
	"call": callAllocProgs["call_loop"],
	// Хвостовая рекурсия: TAILCALL без роста стека.
	"tailcall": `module Main
fn loop(i, acc) -> if i == 0 then acc else loop(i - 1, acc + i)

fn main() -> loop(n(), 0)
`,
	// Мультиклозная fn с паттернами параметров: MATCHLOCAL на каждом клозе.
	"matchlocal": `module Main
fn classify((:a, x)) -> x
fn classify((:b, x)) -> x * 2
fn classify(_) -> 0

fn loop(i, acc) -> if i == 0 then acc else loop(i - 1, acc + classify((:b, i)))

fn main() -> loop(n(), 0)
`,
	// send/recv: пинг-понг двух акторов, n обменов.
	"send_recv": `module Main
fn echo() ->
    recv
        (:ping, from, i) ->
            send(from, i)
            echo()
        :stop -> :ok

fn pingpong(pid, i, acc) ->
    if i == 0
        send(pid, :stop)
        acc
    else
        send(pid, (:ping, self(), i))
        v = recv
            r -> r
        pingpong(pid, i - 1, acc + v)

fn main() ->
    pid = spawn(() -> echo())
    pingpong(pid, n(), 0)
`,
}

func benchProg(b *testing.B, name string) {
	run := newCallAllocRun(b, benchProgs[name], benchN)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		run()
	}
}

func BenchmarkCall(b *testing.B)       { benchProg(b, "call") }
func BenchmarkTailCall(b *testing.B)   { benchProg(b, "tailcall") }
func BenchmarkMatchLocal(b *testing.B) { benchProg(b, "matchlocal") }
func BenchmarkSendRecv(b *testing.B)   { benchProg(b, "send_recv") }
