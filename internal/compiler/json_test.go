package compiler_test

import "testing"

func TestJsonEncodeBasics(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    print(Json.encode(42))
    print(Json.encode("hi"))
    print(Json.encode([1, 2, 3]))
    print(Json.encode(Some(42)))
    print(Json.encode(None))
`)
}

func TestJsonRoundTrip(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    s = Json.encode(%{ "a" => 1, "b" => [true, false] })
    print(s)
    result = Json.decode(s)
    print(result)
`)
}

func TestJsonDecodeMalformed(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    print(Json.decode("{"))
    print(Json.decode(""))
`)
}
