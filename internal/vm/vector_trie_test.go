package vm_test

import "testing"

// T-273: Vector на 32-арном trie — push, set, чтение и спред на длинах,
// которые пересекают границы уровней дерева (32, 1024).
func TestVectorTrieSemantics(t *testing.T) {
	runModuleSync(t, `module Main
fn build(i, acc) -> if i == 0 then acc else build(i - 1, Vec.push(acc, i))

fn main() ->
    v = build(2000, %[])
    assert(len(v) == 2000)
    assert(Vec.len(v) == 2000)
    # build кладёт 2000, затем 1999 … 1.
    assert(v[0] == 2000)
    assert(v[31] == 1969)
    assert(v[32] == 1968)
    assert(v[1023] == 977)
    assert(v[1024] == 976)
    assert(v[1999] == 1)
    assert(Vec.get(v, 1999) == Some(1))
    assert(Vec.get(v, 2000) == None)
    assert(Vec.get(v, -1) == None)
    w = Vec.set(v, 1024, :x)
    assert(w[1024] == :x)
    assert(w[1023] == 977)
    assert(w[1025] == 975)
    assert(len(w) == 2000)
    # Прежнее значение не изменилось (§0.13).
    assert(v[1024] == 976)
    assert(Vec.push(v, :tail)[2000] == :tail)
    assert(len(v) == 2000)
`)
}

func TestVectorIndexOutOfBounds(t *testing.T) {
	runModuleSync(t, `module Main
fn main() ->
    v = %[1, 2, 3]
    r = trap(v[3])
    assert(r == Error((:index_out_of_bounds, (3, 3))))
    r2 = trap(v[-1])
    assert(r2 == Error((:index_out_of_bounds, (-1, 3))))
    r3 = trap(Vec.set(v, 3, 9))
    assert(r3 == Error((:index_out_of_bounds, (3, 3))))
`)
}

func TestVectorLiteralAndSpread(t *testing.T) {
	runModuleSync(t, `module Main
fn build(i, acc) -> if i == 0 then acc else build(i - 1, Vec.push(acc, i))

fn main() ->
    assert(%[] == %[])
    assert(len(%[]) == 0)
    assert(%[1, 2, 3] == %[1, 2, 3])
    a = build(100, %[])
    b = %[..a, :x]
    assert(len(b) == 101)
    assert(b[100] == :x)
    assert(b[0] == a[0])
    assert(len(a) == 100)
    assert(%[..%[1, 2], ..%[3]] == %[1, 2, 3])
    # Вектор принимает спред списка, список вектор — нет (§5.2).
    assert(%[..[1, 2], 3] == %[1, 2, 3])
    assert(trap([..%[1]]) == Error((:type_error, (:spread, %[1]))))
    # Литерал длиннее листа собирается построителем.
    big = %[..build(40, %[]), 0]
    assert(len(big) == 41)
    assert(big[40] == 0)
    assert(big[39] == 1)
`)
}

func TestVectorEqualityAndTermOrder(t *testing.T) {
	runModuleSync(t, `module Main
fn build(i, acc) -> if i == 0 then acc else build(i - 1, Vec.push(acc, i))

fn main() ->
    a = build(1000, %[])
    assert(a == build(1000, %[]))
    assert(a != Vec.set(a, 999, :x))
    assert(a != Vec.push(a, 0))
    # Term order (§7.4): сначала длина, потом элементы; Vector < List.
    assert(%[1, 2] < %[1, 2, 3])
    assert(%[1, 2] < %[2, 2])
    assert(%[1] < [1])
    assert(%[2] < %[1, 1])
    # Вектор ключом Map: равные векторы — один ключ (§4.8).
    m = %{ a => :found }
    assert(m[build(1000, %[])] == Some(:found))
    assert(m[Vec.push(a, 0)] == None)
`)
}

func TestVectorPrintAndJson(t *testing.T) {
	got := runTimerSrc(t, `module Main
fn main() ->
    %[1, :a, "s"]
`)
	if got.Inspect() != `%[1, :a, "s"]` {
		t.Fatalf("Inspect = %s", got.Inspect())
	}
	runModuleSync(t, `module Main
fn build(i, acc) -> if i == 0 then acc else build(i - 1, Vec.push(acc, i))

fn main() ->
    assert(Json.encode(%[1, 2, 3]) == "[1,2,3]")
    assert(len(Json.encode(build(100, %[]))) > 0)
    assert(to_str(%[1, 2]) == "%[1, 2]")
`)
}
