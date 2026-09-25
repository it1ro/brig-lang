package vm_test

import (
	"testing"

	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/parser"
	"github.com/it1ro/brig-lang/internal/vm"
)

// runModuleSync — прогон модуля через scheduler: parse → compile → RunMain.
func runModuleSync(t *testing.T, src string) {
	t.Helper()
	prog, err := parser.ParseProgram(parser.ModeModule, src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	img, err := compiler.New().Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if img.Main == nil {
		t.Fatal("no main")
	}
	m := vm.New()
	for name, fn := range img.Functions {
		m.DefineGlobal(name, vm.FuncValue(fn))
	}
	if _, err := m.RunMain(m.Global("main")); err != nil {
		t.Fatalf("run: %v", err)
	}
}

// TestSchedulerSpawnSendRecv — spawn + send + recv в двух акторах.
// main ждёт через after, чтобы worker успел распечатать.
func TestSchedulerSpawnSendRecv(t *testing.T) {
	runModuleSync(t, `module Main
fn worker() ->
    x = recv
        msg -> msg
    print(x)

fn main() ->
    pid = spawn(worker)
    send(pid, :hello)
    recv
        :never -> :never
    after 50 -> :ok
`)
}

// TestSchedulerRecvOnAlreadyFilledMailbox — сообщение приходит до recv.
func TestSchedulerRecvOnAlreadyFilledMailbox(t *testing.T) {
	runModuleSync(t, `module Main
fn worker() ->
    x = recv
        msg -> msg
    print(x)

fn main() ->
    pid = spawn(worker)
    send(pid, :a)
    send(pid, :b)
    recv
        :never -> :never
    after 50 -> :ok
`)
}

// TestSchedulerSelf — self() возвращает pid текущего актора.
func TestSchedulerSelf(t *testing.T) {
	runModuleSync(t, `module Main
fn worker() ->
    me = self()
    send(me, :hi)
    x = recv
        :hi -> :got
    print(x)

fn main() ->
    pid = spawn(worker)
    recv
        :never -> :never
    after 50 -> :ok
`)
}

// TestSchedulerWatchAndDown — watch получает (:down, ref, reason).
func TestSchedulerWatchAndDown(t *testing.T) {
	runModuleSync(t, `module Main
fn quick() -> :done

fn main() ->
    pid = spawn(quick)
    ref = watch(pid)
    x = recv
        (:down, r, reason) -> (:got_down, reason)
        _ -> :other
    print(x)
`)
}

// TestSchedulerRecvElse — else-клауза при несовпадении паттерна.
func TestSchedulerRecvElse(t *testing.T) {
	runModuleSync(t, `module Main
fn worker() ->
    x = recv
        :a -> :got_a
    else msg
        :unknown
    print(x)

fn main() ->
    pid = spawn(worker)
    send(pid, :b)
    recv
        :never -> :never
    after 50 -> :ok
`)
}

// TestSchedulerRecvAfter — after-клауза при таймауте (без входящих сообщений).
func TestSchedulerRecvAfter(t *testing.T) {
	runModuleSync(t, `module Main
fn worker() ->
    x = recv
        :a -> :got_a
    after 10 -> :timeout
    print(x)

fn main() ->
    pid = spawn(worker)
    recv
        :never -> :never
    after 100 -> :ok
`)
}

// TestSchedulerDeadlock — все акторы заблокированы, scheduler должен
// вернуть ошибку, а не зависнуть.
func TestSchedulerDeadlock(t *testing.T) {
	prog, err := parser.ParseProgram(parser.ModeModule, `module Main
fn main() ->
    recv
        :never -> :ok
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	img, err := compiler.New().Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	m := vm.New()
	for name, fn := range img.Functions {
		m.DefineGlobal(name, vm.FuncValue(fn))
	}
	if _, err := m.RunMain(m.Global("main")); err == nil {
		t.Fatal("want deadlock error, got nil")
	}
}

// TestSchedulerMailboxHWM — send кладёт сообщения, mailbox_size их видит.
func TestSchedulerMailboxHWM(t *testing.T) {
	runModuleSync(t, `module Main
fn sleeper() ->
    recv
        :wake -> :ok

fn main() ->
    pid = spawn(sleeper)
    send(pid, :a)
    send(pid, :b)
    n = mailbox_size(pid)
    print("mailbox:", n)
    send(pid, :wake)
    recv
        :never -> :never
    after 20 -> :ok
`)
}

// TestSchedulerTailRecursionActor — много-итерационный цикл в акторе
// через recv-ветки. С TCO кадр актора заменяется, стек не растёт.
// HWM=64 позволяет отправить 64 :inc без блокировки; для теста хватит.
func TestSchedulerTailRecursionActor(t *testing.T) {
	runModuleSync(t, `module Main
fn counter_loop(n) ->
    recv
        (:inc) -> counter_loop(n + 1)
        (:get, from) ->
            send(from, n)
            counter_loop(n)
        (:stop) -> :ok

fn main() ->
    pid = spawn(() -> counter_loop(0))
    send(pid, :inc)
    send(pid, :inc)
    send(pid, :inc)
    send(pid, :inc)
    send(pid, :inc)
    send(pid, (:get, self()))
    n = recv
        v -> v
    assert(n == 5)
    send(pid, :stop)
`)
}

// internal/vm/scheduler_test.go
func TestRecvAfterSmallInt(t *testing.T) {
	runModuleSync(t, `module Main
fn main() ->
    recv
        :never -> :ok
    after 1 -> :ok
`)
}
