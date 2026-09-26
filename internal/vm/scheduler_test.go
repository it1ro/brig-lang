package vm_test

import (
	"errors"
	"io"
	"os"
	"strings"
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

// TestWatchDeadActorGetsDown — watch на уже завершившийся актор даёт :noproc (I-F9 / T-40).
func TestWatchDeadActorGetsDown(t *testing.T) {
	runModuleSync(t, `module Main
fn quick() -> :done

fn main() ->
    p = spawn(quick)
    r1 = watch(p)
    gone = recv
        (:down, _, _) -> ()
    r2 = watch(p)
    reason = recv
        (:down, _, why) -> why
    assert(reason == :noproc)
`)
}

// TestSendToDeadActor — send на мёртвый pid → Ok(()), без :busy; mailbox_size = 0 (I-F9 / T-40).
func TestSendToDeadActor(t *testing.T) {
	runModuleSync(t, `module Main
fn quick() -> :done

fn flood(p, n) ->
    if n == 0 then mailbox_size(p) else send_one(p, n)

fn send_one(p, n) ->
    assert(send(p, :x) == Ok(()))
    flood(p, n - 1)

fn main() ->
    p = spawn(quick)
    r1 = watch(p)
    gone = recv
        (:down, _, _) -> ()
    n = flood(p, 100)
    assert(n == 0)
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

// captureStdout подменяет os.Stdout на время fn и возвращает вывод.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	done := make(chan []byte)
	go func() {
		b, _ := io.ReadAll(r)
		done <- b
	}()
	old := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = old }()

	fn()

	_ = w.Close()
	b := <-done
	_ = r.Close()
	return string(b)
}

// fairnessSrc — два актора с одинаковой работой на элемент: hog делает её
// в колбэке прелюдии (hogExpr), tick — рекурсией.
func fairnessSrc(hogExpr string) string {
	return `module Main

fn busy(n) -> if n == 0 then 0 else busy(n - 1)

fn step(tag, i) ->
    busy(5000)
    print(tag, i)
    i

fn ticks(tag, i, n) -> if i == n then :ok else ticks(tag, i + 1 + step(tag, i) * 0, n)

fn wait(k) -> if k == 0 then :ok else recv
    (:down, _, _) -> wait(k - 1)

fn main() ->
    watch(spawn(() -> ` + hogExpr + `))
    watch(spawn(() -> ticks("tick", 0, 4)))
    wait(2)
`
}

// TestFairnessPreludeCallback — колбэк прелюдии тратит редукции (K-4) и
// уступает квант (G3, §15.2 п.2): вывод чередуется так же, как у варианта
// с рекурсией, а не «весь hog, затем tick» (T-58).
func TestFairnessPreludeCallback(t *testing.T) {
	const want = "hog 0\ntick 0\nhog 1\ntick 1\nhog 2\ntick 2\nhog 3\ntick 3\n"
	cases := []struct{ name, hog string }{
		{"recursion", `ticks("hog", 0, 4)`},
		{"map", `map(fn (i) -> step("hog", i), [0, 1, 2, 3])`},
		{"filter", `filter(fn (i) -> step("hog", i) >= 0, [0, 1, 2, 3])`},
		{"find", `find(fn (i) -> step("hog", i) < 0, [0, 1, 2, 3])`},
		{"all", `all(fn (i) -> step("hog", i) >= 0, [0, 1, 2, 3])`},
		{"any", `any(fn (i) -> step("hog", i) < 0, [0, 1, 2, 3])`},
		{"fold", `fold(fn (acc, i) -> acc + step("hog", i), 0, [0, 1, 2, 3])`},
		// не хвостовой CALL (выше — TAILCALL из тела лямбды) через алиас
		{"map_call", `assert(Prelude.map(fn (i) -> step("hog", i), [0, 1, 2, 3]) == [0, 1, 2, 3])`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := captureStdout(t, func() { runModuleSync(t, fairnessSrc(tc.hog)) })
			if got != want {
				t.Errorf("interleaving:\ngot:\n%swant:\n%s", got, want)
			}
		})
	}
}

// TestPreludeCallbackRecv — колбэк прелюдии исполняется на кадрах актора
// (T-58), поэтому recv в нём блокирует актор, как в обычной функции, а не
// падает «recv in synchronous call context».
func TestPreludeCallbackRecv(t *testing.T) {
	runModuleSync(t, `module Main
fn take(x) -> recv
    v -> v + x

fn feeder(dst) ->
    send(dst, 10)
    send(dst, 20)

fn main() ->
    me = self()
    spawn(() -> feeder(me))
    ys = map(take, [1, 2])
    assert(ys == [11, 22])
`)
}

// TestPreludeCallbackFrames — raise колбэка всплывает сквозь кадр натива к
// trap вызывающего; вложенные HOF, нативный колбэк и досрочный выход (T-58).
func TestPreludeCallbackFrames(t *testing.T) {
	runModuleSync(t, `module Main
fn g(x) -> if x == 2 then raise(:bad) else x

fn main() ->
    r = trap(map(g, [1, 2, 3]))
    assert(r == Error(:bad))
    zs = map(fn (xs) -> map(fn (x) -> x * 2, xs), [[1], [2, 3]])
    assert(zs == [[2], [4, 6]])
    assert(fold(fn (a, xs) -> fold(fn (b, x) -> b + x, a, xs), 0, zs) == 12)
    assert(map(Some, [1, 2]) == [Some(1), Some(2)])
    assert(find(fn (x) -> g(x) == 1, [1, 2]) == Some(1))
    assert(any(fn (x) -> g(x) == 1, [1, 2]))
    assert(not all(fn (x) -> g(x) == 3, [1, 2]))
    assert(filter(fn (x) -> x > 1, [1, 2, 3]) == [2, 3])
`)
}

// TestPreludeCallbackRaiseTrace — trace непойманного raise из колбэка
// содержит кадр колбэка и не содержит кадра натива (у него нет позиции).
func TestPreludeCallbackRaiseTrace(t *testing.T) {
	prog, err := parser.ParseProgram(parser.ModeModule, `module Main
fn g(x) -> if x == 2 then raise(:bad) else x

fn main() ->
    ys = map(g, [1, 2, 3])
    print(ys)
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
	_, err = m.RunMain(m.Global("main"))
	var rerr *vm.ErrRaise
	if !errors.As(err, &rerr) {
		t.Fatalf("want *ErrRaise, got %v", err)
	}
	var funcs []string
	for _, fr := range rerr.Trace {
		funcs = append(funcs, fr.Func)
	}
	if got := strings.Join(funcs, ","); got != "g,main" {
		t.Errorf("trace funcs = %q, want %q", got, "g,main")
	}
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
