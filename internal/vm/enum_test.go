package vm_test

import (
	"strings"
	"testing"
	"time"
)

// enumKinds — пять перечислимых видов (T-257) и их обход как List:
// Set и Map — в term order, элемент Map — пара (k, v).
var enumKinds = []struct{ name, coll, elems, pick string }{
	{"List", "[3, 1, 2]", "[3, 1, 2]", "1"},
	{"Vector", "%[3, 1, 2]", "[3, 1, 2]", "1"},
	{"Range", "1 to 3", "[1, 2, 3]", "2"},
	{"Set", "set(3, 1, 2)", "[1, 2, 3]", "2"},
	{"Map", "%{2 => 20, 1 => 10}", "[(1, 10), (2, 20)]", "(2, 20)"},
}

// enumSameAsList — каждая функция Enum на виде c даёт то же, что на
// списке его элементов xs в порядке обхода; p — элемент xs.
const enumSameAsList = `
    assert(Enum.map(c, x -> (x, x)) == Enum.map(xs, x -> (x, x)))
    assert(Enum.filter(c, x -> x == p) == [p])
    assert(Enum.reject(c, x -> x == p) == Enum.filter(xs, x -> x != p))
    assert(Enum.fold(c, [], (acc, x) -> [x, ..acc]) == Enum.reverse(xs))
    assert(Enum.find(c, x -> x == p) == Some(p))
    assert(Enum.find(c, x -> false) == None)
    assert(Enum.all?(c, x -> Enum.member?(xs, x)))
    assert(not Enum.all?(c, x -> x == p))
    assert(Enum.any?(c, x -> x == p))
    assert(not Enum.any?(c, x -> false))
    assert(Enum.each(c, x -> x) == ())
    assert(Enum.count(c) == len(xs))
    assert(Enum.count(c, x -> x == p) == 1)
    assert(Enum.member?(c, p))
    assert(not Enum.member?(c, :nope))
    assert(Enum.reverse(c) == Enum.reverse(xs))
    assert(Enum.take(c, 2) == Enum.take(xs, 2))
    assert(Enum.take(c, 9) == xs)
    assert(Enum.take(c, -1) == [])
    assert(Enum.drop(c, 1) == Enum.drop(xs, 1))
    assert(Enum.drop(c, 9) == [])
    assert(Enum.drop(c, 0) == xs)
    assert(Enum.sort(c) == Enum.sort(xs))
    assert(Enum.sort_by(c, x -> x != p) == [p, ..Enum.filter(xs, x -> x != p)])
    assert(Enum.zip(c, c) == Enum.map(xs, x -> (x, x)))
    assert(Enum.zip(c, [:a]) == [(Enum.take(xs, 1)[0], :a)])
    assert(Enum.with_index(c) == Enum.zip(xs, list(0 to len(xs) - 1)))
    assert(Enum.group_by(c, x -> x == p) == %{true => [p], false => Enum.filter(xs, x -> x != p)})
    assert(Enum.min(c) == Some(Enum.sort(xs)[0]))
    assert(Enum.max(c) == Some(Enum.sort(xs)[len(xs) - 1]))
    assert(Enum.flat_map(c, x -> [x, x]) == Enum.fold(Enum.reverse(xs), [], (acc, x) -> [x, x, ..acc]))
    assert(Enum.uniq(c) == xs)
`

// TestEnumKinds — функции Enum на List, Vector, Range, Set и Map.
func TestEnumKinds(t *testing.T) {
	for _, k := range enumKinds {
		t.Run(k.name, func(t *testing.T) {
			runModuleSync(t, "module Main\n\nfn main() ->\n    c = "+k.coll+
				"\n    xs = "+k.elems+"\n    p = "+k.pick+"\n"+enumSameAsList)
		})
	}
	runModuleSync(t, `module Main

fn main() ->
    # sum и join — по видам элементов.
    assert(Enum.sum([3, 1, 2]) == 6)
    assert(Enum.sum(%[3, 1, 2]) == 6)
    assert(Enum.sum(1 to 100) == 5050)
    assert(Enum.sum(set(1, 2.5)) == 3.5)
    assert(Enum.sum([]) == 0)
    assert(Enum.sum([dec"0.1", dec"0.2"]) == dec"0.3")
    assert(trap(Enum.sum(%{1 => 10})) == Error((:type_error, ((:enum, :sum), (1, 10)))))
    assert(Enum.join(["a", "b"], ", ") == "a, b")
    assert(Enum.join(%["a", "b"], "") == "ab")
    assert(Enum.join(set("b", "a"), "-") == "a-b")
    assert(Enum.join([], ",") == "")
    assert(trap(Enum.join(1 to 2, ",")) == Error((:type_error, ((:enum, :join), 1))))
    assert(trap(Enum.join(%{"a" => "b"}, ",")) == Error((:type_error, ((:enum, :join), ("a", "b")))))
`)
}

// TestEnumListValues — результаты на List явными значениями, примеры DoD.
func TestEnumListValues(t *testing.T) {
	runModuleSync(t, `module Main

fn main() ->
    assert(Enum.map(%{"a" => 1}, fn ((k, v)) -> v) == [1])
    assert(Enum.map(%{"b" => 2, "a" => 1}, fn ((k, _)) -> k) == ["a", "b"])
    assert(Enum.filter([1, 2, 3, 4], x -> x rem 2 == 0) == [2, 4])
    assert(Enum.reject([1, 2, 3, 4], x -> x rem 2 == 0) == [1, 3])
    assert(Enum.fold([1, 2, 3], 0, (acc, x) -> acc * 10 + x) == 123)
    assert(Enum.find([1, 2, 3], x -> x > 1) == Some(2))
    assert(Enum.all?([], x -> false))
    assert(not Enum.any?([], x -> true))
    assert(Enum.count([1, 2, 3], x -> x > 1) == 2)
    assert(Enum.reverse([1, 2, 3]) == [3, 2, 1])
    assert(Enum.take([1, 2, 3], 2) == [1, 2])
    assert(Enum.drop([1, 2, 3], 1) == [2, 3])
    assert(Enum.sort([3, 1, 2]) == [1, 2, 3])
    assert(Enum.sort([[1, 1], [2], [0, 0, 0]]) == [[0, 0, 0], [1, 1], [2]])
    assert(Enum.zip([1, 2, 3], [:a, :b]) == [(1, :a), (2, :b)])
    assert(Enum.with_index([:x, :y]) == [(:x, 0), (:y, 1)])
    assert(Enum.min([2, 1, 3]) == Some(1))
    assert(Enum.max([]) == None)
    assert(Enum.flat_map([1, 2], x -> 1 to x) == [1, 1, 2])
    assert(Enum.uniq([1, 2, 1, 3, 2]) == [1, 2, 3])
    assert(Enum.take(1 to 1_000_000_000_000, 2) == [1, 2])
    assert(Enum.count(1 to 1_000_000_000_000) == 1_000_000_000_000)
    assert([3, 1, 2] |> Enum.map(x -> x * 10) |> Enum.sort() == [10, 20, 30])

    # group_by — Map ключ -> List в исходном порядке.
    g = Enum.group_by(["bb", "a", "cc", "d"], s -> len(s))
    assert(g == %{1 => ["a", "d"], 2 => ["bb", "cc"]})
    assert(Enum.group_by([], x -> x) == %{})

    # sort_by устойчивая: равные ключи сохраняют исходный порядок.
    pairs = [(2, :a), (1, :b), (2, :c), (1, :d)]
    assert(Enum.sort_by(pairs, fn ((k, _)) -> k) == [(1, :b), (1, :d), (2, :a), (2, :c)])
    assert(Enum.sort([(2, :a), (1, :b), (2, :a)]) == [(1, :b), (2, :a), (2, :a)])
`)
}

// TestEnumTypeError — не-коллекция на месте субъекта — (:type_error,
// ((:enum, :f), v)) (форма T-236); предикат не Bool — (:expected_bool, r).
func TestEnumTypeError(t *testing.T) {
	calls := map[string]string{
		"map": "Enum.map(v, x -> x)", "filter": "Enum.filter(v, x -> true)",
		"reject": "Enum.reject(v, x -> true)", "fold": "Enum.fold(v, 0, (a, x) -> a)",
		"find": "Enum.find(v, x -> true)", "all?": "Enum.all?(v, x -> true)",
		"any?": "Enum.any?(v, x -> true)", "each": "Enum.each(v, x -> x)",
		"count": "Enum.count(v)", "member?": "Enum.member?(v, 1)",
		"reverse": "Enum.reverse(v)", "take": "Enum.take(v, 1)", "drop": "Enum.drop(v, 1)",
		"sort": "Enum.sort(v)", "sort_by": "Enum.sort_by(v, x -> x)", "zip": "Enum.zip(v, [])",
		"with_index": "Enum.with_index(v)", "group_by": "Enum.group_by(v, x -> x)",
		"sum": "Enum.sum(v)", "min": "Enum.min(v)", "max": "Enum.max(v)",
		"flat_map": "Enum.flat_map(v, x -> [x])", "uniq": "Enum.uniq(v)",
		"join": "Enum.join(v, \",\")",
	}
	runModuleSync(t, enumTypeErrSrc(calls))

	runModuleSync(t, `module Main

fn main() ->
    assert(trap(Enum.count([1], x -> 1)) == Error((:type_error, (:expected_bool, 1))))
    assert(trap(Enum.filter(1 to 2, x -> :yes)) == Error((:type_error, (:expected_bool, :yes))))
    assert(trap(Enum.all?(set(1), x -> 0)) == Error((:type_error, (:expected_bool, 0))))
    assert(trap(Enum.take([1], 1.5)) == Error((:type_error, ((:enum, :take), 1.5))))
    assert(trap(Enum.drop(%[1], :x)) == Error((:type_error, ((:enum, :drop), :x))))
    assert(trap(Enum.zip([1], 5)) == Error((:type_error, ((:enum, :zip), 5))))
    assert(trap(Enum.join(["a"], 1)) == Error((:type_error, ((:enum, :join), 1))))
    assert(trap(Enum.flat_map([1], x -> x)) == Error((:type_error, ((:enum, :flat_map), 1))))
    r = trap(Enum.sort([1, fn () -> 1]))
    assert(is_compare_error(r))
    assert(trap(Enum.min([dec"1", 1.0])) == Error((:type_error, (:compare, (1.0, dec"1")))))
    assert(trap(Enum.count([1], 2, 3)) == Error((:function_clause, [[1], 2, 3])))
    n = 0
    assert(trap(Enum.map(n to n - 1, x -> x)) == Error((:range_error, (0, -1))))
    assert(trap(Enum.map([1, 2], x -> raise(:boom))) == Error(:boom))

fn is_compare_error(Error((:type_error, (:compare, _)))) -> true
fn is_compare_error(_) -> false
`)
}

// enumTypeErrSrc — по функции на вызов: v — параметр, не повторная
// привязка в одной области.
func enumTypeErrSrc(calls map[string]string) string {
	var b strings.Builder
	b.WriteString("module Main\n\n")
	var names []string
	i := 0
	for f, call := range calls {
		name := "check" + string(rune('a'+i/26)) + string(rune('a'+i%26))
		i++
		names = append(names, name)
		b.WriteString("fn " + name + "(v) -> assert(trap(" + call +
			") == Error((:type_error, ((:enum, :" + f + "), v))))\n")
	}
	b.WriteString("\nfn main() ->\n")
	for _, v := range []string{"42", "\"abc\"", "(1, 2)", "Some(1)", ":atom"} {
		for _, n := range names {
			b.WriteString("    " + n + "(" + v + ")\n")
		}
	}
	return b.String()
}

// TestEnumMapLinear — Enum.map строит результат за один проход: 100 000
// элементов не дольше ~20× от 10 000 (квадратичный рост — ~100×).
func TestEnumMapLinear(t *testing.T) {
	run := func(n string) time.Duration {
		start := time.Now()
		runModuleSync(t, `module Main

fn main() ->
    xs = list(1 to `+n+`)
    ys = Enum.map(xs, x -> x + 1)
    assert(Enum.count(ys) == `+n+`)
    assert(Enum.map(%[..xs], x -> x) == xs)
`)
		return time.Since(start)
	}
	run("10000") // прогрев
	small, big := run("10000"), run("100000")
	if big > 20*small+50*time.Millisecond {
		t.Fatalf("Enum.map: 10k %v, 100k %v — рост сверхлинейный", small, big)
	}
}
