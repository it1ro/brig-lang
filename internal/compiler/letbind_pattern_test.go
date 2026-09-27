package compiler_test

import (
	"strings"
	"testing"
)

// T-133, §4.1/§5.1: `pattern = expr` связывает имена кортежа, записи и
// конструктора.
func TestLetBindTuplePattern(t *testing.T) {
	runModule(t, `module Main
type User { name: Str, id: Int }

fn f() -> Ok(42)

fn main() ->
    t = (1, "a", :ok)
    (a, b, c) = t
    assert(a == 1)
    assert(b == "a")
    assert(c == :ok)
    User{name: n} = User{name: "Ada", id: 1}
    assert(n == "Ada")
    {json: j} = {json: "{}", len: 2}
    assert(j == "{}")
    Ok(x) = f()
    assert(x == 42)
    (p, (q, _)) = (1, (2, 3))
    assert(p + q == 3)
    _ = 99
    (u, v) as all = (5, 6)
    assert(all == (5, 6) and u == 5 and v == 6)
`)
}

// T-133, §5.1: спред в list-паттерне связывания.
func TestLetBindListSpread(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    [h, ..t] = [1, 2, 3]
    assert(h == 1)
    assert(t == [2, 3])
    [x, y, ..] = [7, 8, 9]
    assert(x + y == 15)
    [only] = [4]
    assert(only == 4)
`)
}

// T-133, §5.1/§10.4: несовпадение — ловимый (:badmatch, v), v — значение
// правой части.
func TestLetBindBadmatch(t *testing.T) {
	runModule(t, `module Main
type User { name: Str, id: Int }

fn swap(pair) ->
    (a, b) = pair
    (b, a)

fn head(xs) ->
    [h, ..] = xs
    h

fn unwrap(r) ->
    Ok(v) = r
    v

# §4.8: анонимный паттерн сопоставляется только с анонимной записью.
fn anonName(u) ->
    {name: n} = u
    n

fn main() ->
    assert(swap((1, 2)) == (2, 1))
    r1 = trap(swap((1, 2, 3)))
    assert(r1 == Error((:badmatch, (1, 2, 3))))
    r2 = trap(swap(:x))
    assert(r2 == Error((:badmatch, :x)))
    r3 = trap(head([]))
    assert(r3 == Error((:badmatch, [])))
    r4 = trap(unwrap(Error(:e)))
    assert(r4 == Error((:badmatch, Error(:e))))
    r5 = trap(unwrap(Ok(3)))
    assert(r5 == Ok(3))
    r6 = trap(anonName(User{name: "Ada", id: 1}))
    assert(r6 == Error((:badmatch, User{name: "Ada", id: 1})))
`)
	err := runModuleErr(t, `module Main
fn main() ->
    (a, b) = (1, 2, 3)
    a
`)
	if err == nil || !strings.Contains(err.Error(), "badmatch") {
		t.Fatalf("want uncaught badmatch, got %v", err)
	}
}

// T-133: связывание с паттерном внутри блока trap — с ensure и без;
// имена паттерна видны ensure, несовпадение ловится этим trap.
func TestTrapLetBindPattern(t *testing.T) {
	runModule(t, `module Main
fn plain(v) ->
    r = trap
        (a, b) = v
        a + b
    r

fn withEnsure(v) ->
    r = trap
        [h, ..t] = v
        ensure send(self(), (:seen, h, t))
        h
    r

fn main() ->
    assert(plain((1, 2)) == Ok(3))
    assert(plain((1, 2, 3)) == Error((:badmatch, (1, 2, 3))))
    assert(withEnsure([1, 2]) == Ok(1))
    m = recv
        (:seen, h, t) -> (h, t)
    assert(m == (1, [2]))
    assert(withEnsure([]) == Error((:badmatch, [])))
`)
}
