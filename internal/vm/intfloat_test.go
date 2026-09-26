package vm_test

import "testing"

// T-85 (I-F8, #43): точное сравнение Int×Float в операторах и коллекциях.
func TestIntFloatExactOperators(t *testing.T) {
	runModuleSync(t, `module Main
fn main() ->
    big = 2 ** 53 + 1
    f = 2.0 ** 53
    assert(big != f)
    assert(not (big == f))
    assert(big > f)
    assert(f < big)
    assert(not (big <= f))
    assert(not (big < f))
    assert(2 ** 53 == f)
    assert(1 == 1.0)
    assert(1 < 1.5)
    # транзитивность: a == f, f != b, значит a != b
    a = 2 ** 53
    assert(a == f)
    assert(a != big)
    assert(a < big)
`)
}

func TestIntFloatSetAndMapDedup(t *testing.T) {
	runModuleSync(t, `module Main
fn main() ->
    big = 2 ** 53 + 1
    f = 2.0 ** 53
    assert(len(set(big, f)) == 2)
    assert(len(set(2 ** 53, f)) == 1)
    m = %{ big => :a, f => :b }
    assert(len(m) == 2)
    assert(Map.get(m, big) == Some(:a))
    assert(Map.get(m, f) == Some(:b))
    assert(Map.get(m, 2 ** 53) == Some(:b))
`)
}

func TestIntFloatInfNaNOperators(t *testing.T) {
	runModuleSync(t, `module Main
fn main() ->
    inf = 1.0e308 * 10.0
    nan = inf - inf
    assert(inf > 2 ** 2000)
    assert(-inf < 0 - 2 ** 2000)
    assert(2 ** 2000 < inf)
    assert(inf != 2 ** 2000)
    assert(nan != nan)
    assert(not (nan == nan))
    assert(nan != 1)
    assert(not (nan < 1))
    assert(not (nan > 1))
    assert(not (nan <= 1))
    assert(not (nan >= 1))
    assert(not (1 <= nan))
    assert(not (nan <= nan))
`)
}
