package compiler_test

import (
	"strings"
	"testing"
)

// T-136: конструкторы пользовательских вариантов §14.2 — без аргументов
// значение, с аргументами функция; конструктор как значение-функция;
// неверная арность — ловимый (:function_clause, args).
func TestUserVariantConstructors(t *testing.T) {
	runModule(t, `module Main
type Color { Red, Green, Blue }
type Wrapper { Wrap(Int) }
type Pair<A, B> { MkPair(A, B) }

fn main() ->
    c = Red
    assert(c == Red)
    assert(Red != Green)
    assert(to_str(Blue) == "Blue")
    assert(to_str(Wrap(1)) == "Wrap(1)")
    assert(to_str(MkPair(1, "a")) == "MkPair(1, \"a\")")
    assert(Wrap(1) == Wrap(1))
    assert(Wrap(1) != Wrap(2))
    assert(Wrap(Red) != Wrap(Green))

    assert(map([1, 2], Wrap) == [Wrap(1), Wrap(2)])
    w = Wrap
    assert(w(3) == Wrap(3))
    assert(Wrap == Wrap)
    assert(to_str(Wrap) == "#<function Wrap/1>")
    assert(4 |> Wrap == Wrap(4))
    f = (x) -> Wrap(x)
    assert(f(5) == Wrap(5))

    e = trap(Wrap(1, 2))
    assert(e == Error((:function_clause, [1, 2])))
    e0 = trap(Wrap())
    assert(e0 == Error((:function_clause, [])))

    assert(%{ Red => 1 } == %{ Red => 1 })
    assert(%{ Red => 1 } != %{ Green => 1 })
`)
}

// Конструктор без аргументов не вызывается: значение варианта — не функция.
func TestUserVariantNullaryNotCallable(t *testing.T) {
	runModule(t, `module Main
type Color { Red }

fn main() ->
    r = trap(Red())
    ok = match r
        Error((:type_error, _)) -> true
        _ -> false
    assert(ok)
`)
}

// Мультиклозные fn и match по тегам; непокрытый тег — :function_clause /
// :case_clause.
func TestUserVariantPatterns(t *testing.T) {
	runModule(t, `module Main
type Color { Red, Green, Blue }
type Shape { Circle(Int), Rect(Int, Int) }

fn name(Red) -> "red"
fn name(Green) -> "green"

fn area(Circle(r)) -> 3 * r * r
fn area(Rect(w, h)) -> w * h

fn tall(Rect(1, h)) when h > 1 -> h
fn tall(_) -> 0

fn is_red(c) ->
    match c
        Red -> true
        _ -> false

fn main() ->
    assert(name(Red) == "red")
    assert(name(Green) == "green")
    e = trap(name(Blue))
    assert(e == Error((:function_clause, [Blue])))

    assert(area(Circle(2)) == 12)
    assert(area(Rect(2, 3)) == 6)
    assert(map([Circle(1), Rect(1, 2)], area) == [3, 2])

    assert(is_red(Red))
    assert(not is_red(Blue))
    assert(not is_red(Circle(1)))

    m = trap
        match Blue
            Red -> 1
            Green -> 2
    assert(m == Error((:case_clause, Blue)))

    Rect(w, _) = Rect(7, 8)
    assert(w == 7)
    b = trap
        Circle(r) = Rect(1, 1)
        r
    assert(b == Error((:badmatch, Rect(1, 1))))

    assert(tall(Rect(1, 2)) == 2)
    assert(tall(Rect(1, 1)) == 0)
`)
}

// Паттерн пользовательского конструктора не совпадает со встроенным
// вариантом того же тега и наоборот (сопоставление по типу и тегу).
func TestUserVariantShadowBuiltin(t *testing.T) {
	runModule(t, `module Main
type Opt { Some(Int), None }

fn builtin_some() -> Prelude.find([1], (x) -> true)

fn user(Some(n)) -> n
fn user(_) -> :other

fn main() ->
    assert(to_str(Some(1)) == "Some(1)")
    assert(user(Some(2)) == 2)
    assert(user(builtin_some()) == :other)
    assert(Some(1) != builtin_some())
    assert(None != Prelude.find([], (x) -> true))
`)
}

// Term order пользовательских вариантов (T-123, вариант A): ступень
// номинальных записей — имя типа, порядок тега в декларации, поля.
func TestUserVariantTermOrder(t *testing.T) {
	runModule(t, `module Main
type Color { Red, Green, Blue }
type Size { Small(Int), Big(Int) }
type Zebra { z: Int }
type Apple { a: Int }

fn main() ->
    assert(Red < Green)
    assert(Green < Blue)
    assert(Red < Blue)
    assert(Blue > Red)
    assert(Red <= Red)

    assert(Small(9) < Big(1))
    assert(Small(1) < Small(2))
    assert(Big(1) < Big(2))

    assert(Red < Small(0))
    assert(Big(9) < Zebra{ z: 0 })
    assert(Apple{ a: 0 } < Red)

    assert(Red < Some(1))
    assert(Red < None)
    assert(Red < Ok(1))
    assert({ a: 1 } > Red)
    assert(Red > [1])
`)
}

// Один тег в двух декларациях — ошибка компиляции.
func TestUserVariantDuplicateCtor(t *testing.T) {
	err := runModuleErr(t, `module Main
type A { X }
type B { X(Int) }

fn main() -> X
`)
	if err == nil || !strings.Contains(err.Error(), "constructor X already declared in type A") {
		t.Fatalf("want duplicate constructor error, got %v", err)
	}
}
