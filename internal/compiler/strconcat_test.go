package compiler_test

import "testing"

// Elixir-style `<>` — string concat operator and prefix-destructuring pattern.
func TestStrConcatOperator(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    assert("Hello, " <> "World" == "Hello, World")
    assert("a" <> "b" <> "c" == "abc")
`)
}

func TestStrConcatOperatorTypeError(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    r = trap(1 <> "x")
    assert(r == Error((:type_error, (:concat, (1, "x")))))
`)
}

func TestStrConcatPatternLetBind(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    "Hello, " <> name = "Hello, World"
    assert(name == "World")
    "a" <> "b" <> rest = "abcdef"
    assert(rest == "cdef")
`)
}

func TestStrConcatPatternFnClause(t *testing.T) {
	runModule(t, `module Main
fn greet("Hello, " <> name) -> name
fn greet(_) -> "?"

fn main() ->
    assert(greet("Hello, World") == "World")
    assert(greet("Bye") == "?")
`)
}

func TestStrConcatPatternBadmatch(t *testing.T) {
	runModule(t, `module Main
fn f(s) ->
    "Hello, " <> name = s
    name

fn main() ->
    r = trap(f("Bye"))
    assert(r == Error((:badmatch, "Bye")))
`)
}
