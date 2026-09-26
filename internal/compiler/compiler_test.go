package compiler_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/parser"
	"github.com/it1ro/brig-lang/internal/sema"
	"github.com/it1ro/brig-lang/internal/vm"
)

// runModule прогоняет модуль через scheduler (RunMain), чтобы recv
// и другие акторные примитивы работали. После parse вызывает sema.Check
// (как CLI `brig run`/`brig check`) и падает на HasErrors.
func runModule(t *testing.T, src string) {
	t.Helper()
	prog, err := parser.ParseProgram(parser.ModeModule, src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if semaRes := sema.Check(prog); semaRes.HasErrors() {
		t.Fatalf("sema:\n%s", formatSemaDiagnostics(semaRes))
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

// formatSemaDiagnostics зеркалит CLI reportDiagnostics (severityность + позиция + Message).
func formatSemaDiagnostics(r *sema.Result) string {
	var b strings.Builder
	for _, d := range r.Diagnostics {
		sev := "error"
		if d.Severity == sema.SeverityInfo {
			sev = "info"
		}
		fmt.Fprintf(&b, "%s: %d:%d: %s\n", sev, d.Line, d.Col, d.Message)
	}
	return b.String()
}

func TestHello(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    print("hi")
`)
}

func TestArith(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    x = 2 + 3 * 4
    print(x)
`)
}

func TestRecursion(t *testing.T) {
	runModule(t, `module Main
fn fib(n) ->
    if n < 2 then n else fib(n - 1) + fib(n - 2)
fn main() ->
    print(fib(10))
`)
}

func TestAndOr(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    a = true and false
    b = false or true
    c = true and true
    d = false or false
    assert(a == false)
    assert(b == true)
    assert(c == true)
    assert(d == false)
`)
}

func TestLocalFn(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    fn add(x, y) -> x + y
    assert(add(1, 2) == 3)
`)
}

func TestClosure(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    x = 10
    f = () -> x
    assert(f() == 10)
`)
}

func TestMutualRecursion(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    fn is_even(n) -> if n == 0 then true else is_odd(n - 1)
    fn is_odd(n) -> if n == 0 then false else is_even(n - 1)
    assert(is_even(10) == true)
`)
}

func TestLambdaShort(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    xs = [1, 2, 3]
    ys = map(x -> x * 2, xs)
    print(ys)
`)
}

func TestClosureCapture(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    base = 100
    add_base = x -> base + x
    assert(add_base(5) == 105)
`)
}

func TestLocalFnRecursion(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    fn fact(n) -> if n <= 1 then 1 else n * fact(n - 1)
    assert(fact(5) == 120)
`)
}

// ---- trap / ensure ----

func TestTrapInlineOk(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    result = trap(1 + 1)
    print(result)
`)
}

func TestTrapInlineError(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    result = trap(raise(:boom))
    print(result)
`)
}

func TestTrapBlockOk(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    result = trap
        x = 1
        x + 2
    print(result)
`)
}

func TestTrapBlockError(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    result = trap
        x = 1
        raise(:oops)
    print(result)
`)
}

func TestTrapWithEnsure(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    result = trap
        print("body")
        ensure print("cleanup-1")
        ensure print("cleanup-2")
        :ok
    print(result)
`)
}

func TestTrapNested(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    inner = trap(raise(:inner))
    outer = trap(print(inner))
    print(outer)
`)
}

func TestTrapCatchesDivisionByZero(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    result = trap(1 div 0)
    assert(result == Error((:division_by_zero, ())))
`)
}

func TestTrapBlockPropagatesThroughFn(t *testing.T) {
	runModule(t, `module Main
fn boom() -> raise(:deep)
fn main() ->
    result = trap(boom())
    assert(result == Error(:deep))
`)
}

func TestTrapEnsureRaisesInSuccess(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    result = trap
        ensure raise(:cleanup_failed)
        :ok
    assert(result == Error(:cleanup_failed))
`)
}

func TestTrapEnsureRaisesInBody(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    result = trap
        raise(:body_failed)
        ensure raise(:cleanup_failed)
    assert(result == Error(:cleanup_failed))
`)
}

func TestTrapEnsureLifo(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    result = trap
        ensure raise(:first_registered)
        ensure raise(:second_registered)
        :ok
    assert(result == Error(:first_registered))
`)
}

func TestTrapEnsureLifoOnError(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    result = trap
        ensure raise(:first_registered)
        ensure raise(:second_registered)
        raise(:boom)
    assert(result == Error(:first_registered))
`)
}

func TestTrapNoEnsureUnchanged(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    a = trap(1 + 1)
    print(a)
    b = trap(raise(:x))
    print(b)
`)
}

// ---- акторы ----

func TestSpawnAndSend(t *testing.T) {
	runModule(t, `module Main
fn worker_loop() ->
    x = recv
        (:stop) -> :ok
        msg -> msg
    print(x)

fn main() ->
    pid = spawn(worker_loop)
    send(pid, :hello)
    send(pid, :stop)
    recv
        :never -> :never
    after 50 -> :ok
`)
}

func TestSpawnLinked(t *testing.T) {
	runModule(t, `module Main
fn worker() ->
    x = recv
        msg -> msg
    x

fn main() ->
    pid = spawn_linked(worker)
    send(pid, :hi)
    recv
        (:down, _, _) -> :done
        _ -> :other
    after 50 -> :timeout
`)
}

func TestSelfIsPid(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    me = self()
    print(me)
`)
}

func TestWatchAndDown(t *testing.T) {
	runModule(t, `module Main
fn quick() -> :done

fn main() ->
    pid = spawn(quick)
    ref = watch(pid)
    x = recv
        (:down, r, reason) -> (r, reason)
        _ -> :other
    print(x)
`)
}

func TestRecvElse(t *testing.T) {
	runModule(t, `module Main
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

func TestRecvAfter(t *testing.T) {
	runModule(t, `module Main
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

// ---- TCO ----

func TestTailRecursion(t *testing.T) {
	runModule(t, `module Main
fn sum_to(n, acc) ->
    if n == 0 then acc else sum_to(n - 1, acc + n)

fn main() ->
    assert(sum_to(1000000, 0) == 500000500000)
`)
}

func TestNonTailRecursionStillWorks(t *testing.T) {
	runModule(t, `module Main
fn fact(n) ->
    if n <= 1 then 1 else n * fact(n - 1)

fn main() ->
    assert(fact(10) == 3628800)
`)
}

func TestTailRecursionThroughRecv(t *testing.T) {
	runModule(t, `module Main
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
    send(pid, (:get, self()))
    n = recv
        v -> v
    assert(n == 3)
    send(pid, :stop)
`)
}

// ---- prelude / patterns / ensure ----

func TestPreludeFilter(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    xs = [1, 2, 3, 4, 5]
    ys = filter(x -> x rem 2 == 0, xs)
    print(ys)
`)
}

func TestPreludeFind(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    xs = [1, 2, 3]
    print(find(x -> x == 2, xs))
    print(find(x -> x == 99, xs))
`)
}

func TestPreludeAllAny(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    print(all(x -> x > 0, [1, 2, 3]))
    print(all(x -> x > 0, [1, -2, 3]))
    print(any(x -> x > 2, [1, 2, 3]))
    print(any(x -> x > 9, [1, 2, 3]))
`)
}

func TestPreludeToIntToFloat(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    print(to_int("42"))
    print(to_float("3.14"))
    print(to_int(7))
`)
}

func TestPreludeLog(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    log("hi")
`)
}

func TestSmallIntFastPath(t *testing.T) {
	runModule(t, `module Main
fn loop(n, acc) ->
    if n == 0 then acc else loop(n - 1, acc + n)
fn main() ->
    print(loop(100000, 0))
`)
}

func TestRecvListPattern(t *testing.T) {
	runModule(t, `module Main
fn worker() ->
    x = recv
        [1, ..rest] -> rest
        _ -> :no_match
    print(x)

fn main() ->
    pid = spawn(worker)
    send(pid, [1, 2, 3])
    recv
        :never -> :never
    after 50 -> :ok
`)
}

func TestRecvMapPattern(t *testing.T) {
	runModule(t, `module Main
fn worker() ->
    x = recv
        %{ "a" => v } -> v
        _ -> :no_match
    print(x)

fn main() ->
    pid = spawn(worker)
    send(pid, %{ "a" => 42 })
    recv
        :never -> :never
    after 50 -> :ok
`)
}

func TestEnsureAllRunOnFailure(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    result = trap
        ensure raise(:first_registered)
        ensure raise(:cleanup_failed)
        :ok
    assert(result == Error(:first_registered))
`)
}

func TestDivByZeroSmallInt(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    a = trap(1 div 0)
    print(a)
    b = trap(1 rem 0)
    print(b)
    c = trap(0 ** 0)
    print(c)
`)
}

func TestLiteralDispatch(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    print(1)
    print(1.5)
    print("hi")
    print(true)
    print(())
`)
}

// ---- Sprint 5.1: Range ----

func TestRangeMaterialize(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    assert(list(1 to 5) == [1, 2, 3, 4, 5])
    assert(list(1 to 1) == [1])
`)
}

func TestRangeEquality(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    a = 1
    b = 5
    c = 2
    assert((a to b) == (a to b))
    assert((a to b) != (c to b))
`)
}

func TestRangeError(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    a = 5
    b = 1
    result = trap(list(a to b))
    print(result)
`)
}

// ---- Sprint 5.2: Set ----

func TestSetBasics(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    s = set(1, 2, 2, 3, 1)
    assert(len(s) == 3)
    s2 = set(3, 2, 1)
    assert(s == s2)
`)
}

// ---- Sprint 5.3: Vec/Map modules ----

func TestVecPush(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    v = %[1, 2]
    v2 = Vec.push(v, 3)
    assert(len(v2) == 3)
    assert(v2[2] == 3)
`)
}

func TestVecSet(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    v = %[1, 2, 3]
    v2 = Vec.set(v, 1, 99)
    assert(v2[1] == 99)
    assert(v2[0] == 1)
`)
}

func TestVecGet(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    v = %[10, 20, 30]
    assert(Vec.get(v, 1) == Some(20))
    assert(Vec.get(v, 99) == None)
`)
}

func TestVecLen(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    assert(Vec.len(%[1, 2, 3, 4]) == 4)
`)
}

func TestMapGetPut(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    m = %{}
    m2 = Map.put(m, "a", 1)
    assert(Map.get(m2, "a") == Some(1))
    assert(Map.get(m2, "b") == None)
`)
}

func TestMapRemove(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    m = %{ "a" => 1, "b" => 2 }
    m2 = Map.remove(m, "a")
    assert(Map.get(m2, "a") == None)
    assert(Map.get(m2, "b") == Some(2))
`)
}

func TestMapKeys(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    m = %{ "a" => 1, "b" => 2 }
    ks = Map.keys(m)
    assert(len(ks) == 2)
`)
}

func TestMapIndex(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    m = %{"a" => 1, "b" => 2}
    assert(m["a"] == Some(1))
    assert(m["z"] == None)
`)
}

func TestVecIndex(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    v = %[10, 20, 30]
    assert(v[0] == 10)
    assert(v[2] == 30)
`)
}

func TestListIndexOutOfBounds(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    xs = [1, 2, 3]
    result = trap(xs[99])
    print(result)
`)
}

// ---- Sprint 5.4: Bytes ----

func TestBytesLiteral(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    b = b"\x89PNG"
    assert(len(b) == 4)
`)
}

func TestBytesEscapes(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    b = b"\n\t\r\0\\\""
    assert(len(b) == 6)
    assert(b[0] == 10)
    assert(b[1] == 9)
    assert(b[2] == 13)
    assert(b[3] == 0)
    assert(b[4] == 92)
    assert(b[5] == 34)
`)
}

func TestBytesIndex(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    b = b"abc"
    assert(b[0] == 97)
    assert(b[2] == 99)
`)
}

func TestBytesEquality(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    assert(b"abc" == b"abc")
    assert(b"abc" != b"abd")
`)
}

func TestBytesToStr(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    s = Bytes.to_str(b"hi")
    assert(s == "hi")
`)
}

func TestBytesToStrInvalid(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    result = trap(Bytes.to_str(b"\x89"))
    print(result)
`)
}

func TestStrToBytes(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    b = Str.to_bytes("hi")
    assert(len(b) == 2)
    assert(b[0] == 104)
    assert(b[1] == 105)
`)
}

func TestBytesIndexOutOfBounds(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    b = b"ab"
    result = trap(b[99])
    print(result)
`)
}

// ---- Sprint 5.4: Decimal (§3.1) ----

func TestDecimalLiteral(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    print(dec"1.5")
    print(dec"-1.5")
    print(dec"0")
    print(dec"1_000.5")
`)
}

func TestDecimalEquality(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    assert(dec"1.5" == dec"1.5")
    assert(dec"1.50" == dec"1.5")
    assert(dec"1.5" != dec"1.6")
    assert(dec"2" == 2)
    assert(2 == dec"2")
`)
}

func TestDecimalArithmetic(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    assert(dec"1.5" + dec"2.5" == dec"4")
    assert(dec"1.5" - dec"0.5" == dec"1")
    assert(dec"1.5" * dec"2" == dec"3")
    assert(dec"1.5" / dec"0.5" == dec"3")
    assert(-dec"1.5" == dec"-1.5")
    assert(dec"2" ** 10 == dec"1024")
`)
}

func TestDecimalIntMix(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    assert(dec"1.5" + 1 == dec"2.5")
    assert(1 + dec"1.5" == dec"2.5")
    assert(dec"2.5" * 2 == dec"5")
    assert(5 < dec"5.5")
    assert(dec"5.5" > 5)
`)
}

func TestDecimalFloatTypeError(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    a = trap(dec"1.5" + 1.5)
    print(a)
    b = trap(dec"1.5" == 1.5)
    print(b)
    c = trap(dec"1.5" < 1.5)
    print(c)
`)
}

func TestDecimalDivideByZero(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    a = trap(dec"1" / dec"0")
    print(a)
`)
}

func TestDecimalIntDivRemTypeError(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    a = trap(dec"5" div 2)
    print(a)
    b = trap(dec"5" rem 2)
    print(b)
`)
}

func TestDecimalCompare(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    assert(dec"1.5" < dec"2")
    assert(dec"1.5" <= dec"1.5")
    assert(dec"2" > dec"1.999")
    assert(dec"1.5" >= dec"1.5")
`)
}

func TestStringEscapes(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    s = "a\nb\tc\rd\\e\"f"
    assert(len(s) == 11)
    assert(s[1] == "\n")
    assert(s[3] == "\t")
    assert(s[5] == "\r")
    assert(s[7] == "\\")
    assert(s[9] == "\"")
`)
}

func TestStringUnicodeEscape(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    s = "\u{41}\u{42}\u{43}"
    assert(s == "ABC")
`)
}

func TestJsonDecodeEscapedString(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    result = Json.decode("{\"x\": 1}")
    print(result)
`)
}
