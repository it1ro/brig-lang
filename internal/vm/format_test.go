package vm_test

import "testing"

// TestFloatFormat — Float.round и Float.to_str (T-282).
func TestFloatFormat(t *testing.T) {
	runModuleSync(t, `module Main

fn main() ->
    assert(Float.to_str(34.456, 2) == "34.46")
    assert(Float.to_str(34.0, 2) == "34.00")
    assert(Float.to_str(0.5, 0) == "0")
    assert(Float.to_str(1.5, 0) == "2")
    assert(Float.to_str(-2.345, 1) == "-2.3")
    assert(Float.to_str(1234567.891, 1) == "1234567.9")
    assert(Float.to_str(0.1, 3) == "0.100")
    assert(Float.round(34.456, 2) == 34.46)
    assert(Float.round(2.0, 3) == 2.0)
    assert(Float.round(-1.239, 2) == -1.24)
    assert(Float.round(1234.5, 0) == 1234.0)
    assert(Float.round(1.5, 0) == 2.0)
    assert(Float.to_str(Float.round(0.1 + 0.2, 2), 2) == "0.30")
    assert(34.456 |> Float.round(1) == 34.5)

    # ошибки: ((:float, :f), v), v — неверный аргумент.
    assert(trap(Float.round(1, 2)) == Error((:type_error, ((:float, :round), 1))))
    assert(trap(Float.round("x", 2)) == Error((:type_error, ((:float, :round), "x"))))
    assert(trap(Float.round(1.5, 1.0)) == Error((:type_error, ((:float, :round), 1.0))))
    assert(trap(Float.round(1.5, -1)) == Error((:type_error, ((:float, :round), -1))))
    assert(trap(Float.to_str(1, 2)) == Error((:type_error, ((:float, :to_str), 1))))
    assert(trap(Float.to_str(1.5, "2")) == Error((:type_error, ((:float, :to_str), "2"))))
    assert(trap(Float.to_str(1.5, -1)) == Error((:type_error, ((:float, :to_str), -1))))
`)
}

// TestStrPad — Str.pad_left и Str.pad_right, ширина в кодпоинтах (T-282).
func TestStrPad(t *testing.T) {
	runModuleSync(t, `module Main

fn main() ->
    assert(Str.pad_left("7", 3, "0") == "007")
    assert(Str.pad_right("ab", 5, ".") == "ab...")
    assert(Str.pad_left("abc", 3, " ") == "abc")
    assert(Str.pad_left("abcd", 3, " ") == "abcd")
    assert(Str.pad_right("abcd", 0, " ") == "abcd")
    assert(Str.pad_right("abcd", -2, " ") == "abcd")
    assert(Str.pad_left("", 2, "x") == "xx")
    # ширина — кодпоинты, не байты
    assert(Str.pad_left("é", 3, "-") == "--é")
    assert(Str.pad_right("日本", 4, "*") == "日本**")
    assert(Str.pad_left("a", 3, "日") == "日日a")
    assert("5" |> Str.pad_left(2, "0") == "05")

    assert(trap(Str.pad_left(5, 3, "0")) == Error((:type_error, ((:str, :pad_left), 5))))
    assert(trap(Str.pad_left("a", "3", "0")) == Error((:type_error, ((:str, :pad_left), "3"))))
    assert(trap(Str.pad_left("a", 3, "")) == Error((:type_error, ((:str, :pad_left), ""))))
    assert(trap(Str.pad_left("a", 3, "ab")) == Error((:type_error, ((:str, :pad_left), "ab"))))
    assert(trap(Str.pad_left("a", 3, 0)) == Error((:type_error, ((:str, :pad_left), 0))))
    assert(trap(Str.pad_right(5, 3, "0")) == Error((:type_error, ((:str, :pad_right), 5))))
    assert(trap(Str.pad_right("a", 3.0, "0")) == Error((:type_error, ((:str, :pad_right), 3.0))))
    assert(trap(Str.pad_right("a", 3, "ab")) == Error((:type_error, ((:str, :pad_right), "ab"))))
`)
}
