package vm_test

import (
	"testing"

	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/parser"
	"github.com/it1ro/brig-lang/internal/runtime"
	"github.com/it1ro/brig-lang/internal/vm"
)

// callAllocProgs — вызовы байткод-функций на горячем пути (T-103): лямбда
// колбэком возобновляемого натива и не хвостовой CALL в цикле. input()
// отдаёт готовый список, n() — его длину: сама программа ничего не строит.
var callAllocProgs = map[string]string{
	"map_lambda": `module Main
fn main() -> map(input(), fn (x) -> x + 1)
`,
	"call_loop": `module Main
fn inc(x) -> x + 1

fn loop(i, acc) -> if i == 0 then acc else loop(i - 1, acc + inc(i))

fn main() -> loop(n(), 0)
`,
}

// newCallAllocRun компилирует src и возвращает прогон main на новой VM со
// списком из n элементов в input().
func newCallAllocRun(tb testing.TB, src string, n int) func() {
	tb.Helper()
	prog, err := parser.ParseProgram(parser.ModeModule, src)
	if err != nil {
		tb.Fatalf("parse: %v", err)
	}
	img, err := compiler.New().Compile(prog)
	if err != nil {
		tb.Fatalf("compile: %v", err)
	}
	xs := make([]runtime.Value, n)
	for i := range xs {
		xs[i] = runtime.Int(int64(i))
	}
	input := runtime.List(xs...)
	native := func(name string, v runtime.Value) runtime.Value {
		return runtime.Func(&runtime.FuncValue{Name: name, Arity: 0, IsNative: true,
			Native: func(runtime.Caller, []runtime.Value) (runtime.Value, error) { return v, nil }})
	}
	inputFn, nFn := native("input", input), native("n", runtime.Int(int64(n)))
	return func() {
		m := vm.New()
		for name, fn := range img.Functions {
			m.DefineGlobal(name, vm.FuncValue(fn))
		}
		m.DefineGlobal("input", inputFn)
		m.DefineGlobal("n", nFn)
		if _, err := m.RunMain(m.Global("main")); err != nil {
			tb.Fatalf("run: %v", err)
		}
	}
}

// TestCallFrameAllocs — вызов байткод-функции не аллоцирует: регистры кадра
// берутся со стека регистров актора, *Frame переиспользуется (T-103).
// Считаем прирост аллокаций от 1000 дополнительных вызовов; постоянная часть
// (VM, прелюдия, кадр натива, итоговый список) вычитается. Допуск — на
// перепостановку актора в очередь раз в квант редукций.
func TestCallFrameAllocs(t *testing.T) {
	const n = 1000
	for name, src := range callAllocProgs {
		t.Run(name, func(t *testing.T) {
			small := testing.AllocsPerRun(20, newCallAllocRun(t, src, n))
			large := testing.AllocsPerRun(20, newCallAllocRun(t, src, 2*n))
			perCall := (large - small) / n
			if perCall > 0.01 {
				t.Errorf("allocs per call = %.3f (n=%d: %.0f, n=%d: %.0f), want 0",
					perCall, n, small, 2*n, large)
			}
		})
	}
}

// BenchmarkMapLambda — map с лямбдой по списку из 1000 элементов (профиль
// Z3, T-101). allocs/call — аллокации на вызов лямбды сверх постоянной части
// прогона.
func BenchmarkMapLambda(b *testing.B) {
	const n = 1000
	// baseN — размер прогона постоянной части. Не 0: часть состояния чанка VM
	// строит лениво, при первом его исполнении (кэш ячеек глобалов, T-276), а
	// при n = 0 чанк лямбды не исполняется ни разу — разовая работа попала бы
	// в allocs/call.
	const baseN = 1
	src := callAllocProgs["map_lambda"]
	base := testing.AllocsPerRun(5, newCallAllocRun(b, src, baseN))
	run := newCallAllocRun(b, src, n)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		run()
	}
	b.StopTimer()
	b.ReportMetric((testing.AllocsPerRun(5, run)-base)/(n-baseN), "allocs/call")
}

// TestFrameWindowReuse — окна регистров снятых кадров переиспользуются
// следующими вызовами, поэтому захваты замыканий, значения trap/ensure и
// результаты не должны на них ссылаться (T-103). clobber пишет в те же
// окна, что только что освободили mk/deep; глубокая не хвостовая рекурсия
// проходит через несколько сегментов стека регистров.
func TestFrameWindowReuse(t *testing.T) {
	runModuleSync(t, `module Main
fn mk(a, b) -> fn () -> (a, b)

fn clobber(x, y, z) ->
    w = x + y + z
    (w, [x, y, z])

fn deep(n) -> if n == 0 then raise((:boom, [n, n + 1])) else deep(n - 1) + 1

fn sum(n) -> if n == 0 then 0 else n + sum(n - 1)

fn cleanup(log) ->
    clobber(4, 5, 6)
    send(self(), log)

fn guarded(n) ->
    trap
        ensure cleanup((:ensured, n))
        deep(n)

fn main() ->
    c = mk(1, [2, 3])
    clobber(10, 20, 30)
    assert(c() == (1, [2, 3]))
    r = trap(deep(50))
    clobber(7, 8, 9)
    assert(r == Error((:boom, [0, 1])))
    assert(guarded(30) == Error((:boom, [0, 1])))
    clobber(1, 1, 1)
    recv
        m -> assert(m == (:ensured, 30))
    assert(sum(20000) == 200010000)
    cs = map([1, 2, 3], fn (i) -> mk(i, i * 2))
    clobber(0, 0, 0)
    assert(map(cs, fn (g) -> g()) == [(1, 2), (2, 4), (3, 6)])
`)
}
