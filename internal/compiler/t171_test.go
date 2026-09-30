package compiler_test

import (
	"strings"
	"testing"
)

// T-171: `Behavior` — встроенная запись (§13.2): её литерал и паттерн
// доступны в любом модуле без import, имя типа в рантайме — без
// префикса модуля.
func TestBehaviorBuiltinRecord(t *testing.T) {
	runModule(t, `module Main

fn handlers(Behavior{ handlers: h }) -> h

fn main() ->
    b = Behavior{ handlers: %{ :get => fn (s) -> (s, s) } }
    assert(len(b.handlers) == 1)
    assert(b == Behavior{ handlers: b.handlers })
    assert(to_str(Behavior{ handlers: %{} }) == "Behavior{ handlers: %{} }")
    assert(len(handlers(b)) == 1)
    match b
        Behavior{ handlers: h } -> assert(len(h) == 1)
`)
}

// Своя декларация с тем же именем встроенную затеняет: поля берутся из
// неё, а имя типа получает префикс модуля.
func TestBehaviorShadowedByOwnType(t *testing.T) {
	runModule(t, `module Main
type Behavior { tag: Atom }

fn main() ->
    b = Behavior{ tag: :own }
    assert(b.tag == :own)
    assert(to_str(b) == "Behavior{ tag: :own }")
`)
}

// Неизвестное поле встроенной записи — ошибка компиляции, как у
// объявленной в модуле.
func TestBehaviorUnknownField(t *testing.T) {
	err := runModuleErr(t, `module Main

fn main() -> Behavior{ nope: 1 }
`)
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("want ошибку про поле nope, got %v", err)
	}
}
