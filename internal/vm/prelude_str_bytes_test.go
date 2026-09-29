package vm_test

import "testing"

// T-148: базовый набор Str/Bytes (Go-native). Субъект первым аргументом
// (T-120). Ошибки — ловимые :type_error / :index_out_of_bounds (§10.4).

func TestStrSplitJoin(t *testing.T) {
	runModuleSync(t, `module Main
fn main() ->
    assert(Str.split("a,b,c", ",") == ["a", "b", "c"])
    assert(Str.split("ab", "") == ["a", "b"])
    assert(Str.join(["a", "b", "c"], ",") == "a,b,c")
    assert(Str.join([], ",") == "")
`)
}

func TestStrTrimFind(t *testing.T) {
	runModuleSync(t, `module Main
fn main() ->
    assert(Str.trim("  hi  ") == "hi")
    assert(Str.find("hello", "ll") == Some(2))
    assert(Str.find("hello", "zz") == None)
    assert(Str.find("héllo", "llo") == Some(2))
    assert(Str.replace("a.b.c", ".", "-") == "a-b-c")
    assert(Str.starts_with?("hello", "he") == true)
    assert(Str.starts_with?("hello", "lo") == false)
    assert(Str.ends_with?("hello", "lo") == true)
    assert(Str.lower("HeLLo") == "hello")
    assert(Str.upper("HeLLo") == "HELLO")
    assert(Str.to_int("42") == Some(42))
    assert(Str.to_int("abc") == None)
`)
}

func TestStrSliceCodepoints(t *testing.T) {
	runModuleSync(t, `module Main
fn main() ->
    assert(Str.slice("héllo", 0, 2) == "hé")
    assert(Str.slice("héllo", 5, 5) == "")
    r = trap(Str.slice("héllo", 0, 6))
    assert(r == Error((:index_out_of_bounds, (6, 5))))
    r2 = trap(Str.slice("héllo", -1, 2))
    assert(r2 == Error((:index_out_of_bounds, (-1, 5))))
`)
}

func TestBytesSliceFind(t *testing.T) {
	runModuleSync(t, `module Main
fn main() ->
    b = b"hello"
    assert(Bytes.slice(b, 1, 3) == b"el")
    assert(Bytes.find(b, b"ll") == Some(2))
    assert(Bytes.find(b, b"zz") == None)
    assert(Bytes.split(b"a,b,c", b",") == [b"a", b"b", b"c"])
    assert(Bytes.concat(b"ab", b"cd") == b"abcd")
    assert(Bytes.at(b, 0) == 104)
    r = trap(Bytes.at(b, 99))
    assert(r == Error((:index_out_of_bounds, (99, 5))))
    r2 = trap(Bytes.slice(b, 0, 99))
    assert(r2 == Error((:index_out_of_bounds, (99, 5))))
`)
}
