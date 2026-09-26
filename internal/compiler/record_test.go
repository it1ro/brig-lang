package compiler_test

import (
	"strings"
	"testing"
)

// T-73: записи §4.7 — номинальный и анонимный литералы, доступ к полю,
// update и конвертация через `..`, равенство по §4.8, Inspect, Json.
func TestRecordLiterals(t *testing.T) {
	runModule(t, `module Main
type User { id: Int, name: Str }
type Admin { id: Int, name: Str }

fn main() ->
    u = User{ id: 1, name: "a" }
    assert(u.id == 1)
    assert(u.name == "a")
    a = { id: 1 }
    assert(a.id == 1)
    assert(User{ id: 1 }.id == 1)

    v = User{ ..u, name: "b" }
    assert(v.id == 1)
    assert(v.name == "b")
    assert(v == User{ id: 1, name: "b" })
    assert(u.name == "a")

    r = { ..u }
    assert(r == { id: 1, name: "a" })
    assert(r != u)
    w = User{ ..r }
    assert(w == u)
    assert({ ..a, x: 2 } == { id: 1, x: 2 })
    assert({ ..a, ..{ id: 5 } } == { id: 5 })

    assert(u == User{ name: "a", id: 1 })
    assert({ a: 1, b: 2 } == { b: 2, a: 1 })
    assert(User{ id: 1, name: "a" } != Admin{ id: 1, name: "a" })
    assert(User{ id: 1 } != { id: 1 })
    assert(User{ id: 1 } != User{ id: 1, name: "a" })
    assert(User{ id: 1 } != User{ id: 2 })
    assert(User{} == User{})
    assert({} != User{})

    assert(to_str(u) == "User{ id: 1, name: \"a\" }")
    assert(to_str(User{ name: "a", id: 1 }) == "User{ id: 1, name: \"a\" }")
    assert(to_str({ id: 1 }) == "{ id: 1 }")
    assert(to_str(User{}) == "User{}")
    assert(to_str({}) == "{}")
    assert(to_str([{ id: 1 }]) == "[{ id: 1 }]")

    assert(Json.encode(User{ id: 1 }) == "{\"id\":1}")
    assert(Json.encode(User{ id: 1 }, { type_tag: true }) == "{\"__type__\":\"User\",\"id\":1}")
    assert(Json.encode({ id: 1 }, { type_tag: true }) == "{\"id\":1}")
    assert(Json.encode([User{ id: 1 }], { type_tag: true }) == "[{\"__type__\":\"User\",\"id\":1}]")
    assert(Json.encode(u, { type_tag: false }) == "{\"id\":1,\"name\":\"a\"}")
`)
}

// Поле записи — любое выражение; значение поля — любое значение.
func TestRecordFieldValues(t *testing.T) {
	runModule(t, `module Main
type Box { v: Int }

fn get(b) -> b.v

fn mk(n) -> Box{ v: n * 2 }

fn main() ->
    assert(get(mk(21)) == 42)
    f = { g: fn (x) -> x + 1 }
    h = f.g
    assert(h(1) == 2)
    n = { inner: { x: 3 } }
    assert(n.inner.x == 3)
`)
}

// Чтение отсутствующего поля — runtime raise, ловится trap.
func TestRecordMissingFieldRaises(t *testing.T) {
	runModule(t, `module Main
type User { id: Int, name: Str }

fn main() ->
    u = User{ id: 1 }
    r = trap(u.name)
    assert(r == Error((:field_error, (:name, u))))
    a = { id: 1 }
    q = trap(a.nope)
    assert(q == Error((:field_error, (:nope, a))))
`)

	err := runModuleErr(t, `module Main
fn main() ->
    r = { id: 1 }
    r.name
`)
	if err == nil || !strings.Contains(err.Error(), "field_error") {
		t.Fatalf("want uncaught field_error raise, got %v", err)
	}
}

// Spread в номинальную запись поля, которого нет в декларации, — raise.
func TestRecordSpreadUnknownFieldRaises(t *testing.T) {
	runModule(t, `module Main
type User { id: Int }

fn main() ->
    r = { id: 1, extra: 2 }
    e = trap(User{ ..r })
    assert(e == Error((:field_error, (:extra, "User"))))
`)
}

// Литерал с неизвестным типом или неизвестным полем — ошибка компиляции
// с позицией.
func TestRecordCompileErrors(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{"unknown type", `module Main
fn main() ->
    x = Nope{ id: 1 }
    x
`, "3:9: "},
		{"unknown field", `module Main
type User { id: Int }
fn main() ->
    User{ id: 1, nme: "a" }
`, "4:18: "},
		{"duplicate field", `module Main
fn main() ->
    { a: 1, a: 2 }
`, "3:13: "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runModuleErr(t, tc.src)
			if err == nil {
				t.Fatal("want compile error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want position %q in error, got %v", tc.want, err)
			}
		})
	}
}
