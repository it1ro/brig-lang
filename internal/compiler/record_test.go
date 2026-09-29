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

    r = Record.to_anon(u)
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
    assert(r == Error((:no_field, (:name, u))))
    a = { id: 1 }
    q = trap(a.nope)
    assert(q == Error((:no_field, (:nope, a))))
`)

	err := runModuleErr(t, `module Main
fn main() ->
    r = { id: 1 }
    r.name
`)
	if err == nil || !strings.Contains(err.Error(), "no_field") {
		t.Fatalf("want uncaught no_field raise, got %v", err)
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

// T-142: литерал без имени типа со спредом — record update, вид первого
// спреда (§4.7, проба g09).
func TestRecordUpdateKeepsKind(t *testing.T) {
	runModule(t, `module Main
type User { id: Int, name: Str }

fn rename(u, name) -> { ..u, name: name }

fn main() ->
    u = User{ id: 1, name: "a" }
    assert({ ..u, name: "b" } == User{ id: 1, name: "b" })
    assert(rename(u, "c") == User{ id: 1, name: "c" })
    assert({ ..u } == u)
    assert({ name: "z", ..u } == u)
    assert({ ..u, ..{ name: "d" } } == User{ id: 1, name: "d" })
    p = User{ id: 2 }
    assert({ ..p, name: "e" } == User{ id: 2, name: "e" })
    assert(to_str({ ..p, name: "e" }) == "User{ id: 2, name: \"e\" }")
    a = { id: 1 }
    assert({ ..a, x: 2 } == { id: 1, x: 2 })
    assert({ ..a, ..u } == { id: 1, name: "a" })
`)
}

// T-142: поле не из типа при update номинальной записи —
// (:no_field, (name, rec)), rec — первый спред; ловится trap.
func TestRecordUpdateUnknownFieldOnNominal(t *testing.T) {
	runModule(t, `module Main
type User { id: Int, name: Str }

fn main() ->
    u = User{ id: 1, name: "a" }
    e = trap({ ..u, age: 3 })
    assert(e == Error((:no_field, (:age, u))))
    f = trap({ ..u, ..{ extra: 1 } })
    assert(f == Error((:no_field, (:extra, u))))
    g = trap({ age: 3, ..u })
    assert(g == Error((:no_field, (:age, u))))
`)
}

// T-142: Record.to_anon — явная конвертация в анонимную запись.
func TestRecordToAnon(t *testing.T) {
	runModule(t, `module Main
type User { id: Int, name: Str }

fn main() ->
    u = User{ id: 1, name: "a" }
    anon = Record.to_anon(u)
    assert(anon == { id: 1, name: "a" })
    assert(anon != u)
    assert(to_str(anon) == "{ id: 1, name: \"a\" }")
    assert(Record.to_anon(anon) == anon)
    assert({ ..anon, x: 1 } == { id: 1, name: "a", x: 1 })
    assert({ ..Record.to_anon(u), kind: :user } == { id: 1, name: "a", kind: :user })
    assert(User{ ..anon } == u)
    e = trap(Record.to_anon(1))
    assert(e == Error((:type_error, (:to_anon, 1))))
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
