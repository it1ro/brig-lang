package vm_test

import "testing"

// T-284 (§0.2): to_int/to_float принимают только числа; Str — не число:
// (:type_error, (op, val)). Разбор строк — Str.to_int/Str.to_float
// (Option, без raise).
func TestToIntToFloatErrors(t *testing.T) {
	cases := map[string]string{
		"to_int_atom": `r = trap(to_int(:x))
    assert(r == Error((:type_error, (:to_int, :x))))`,
		"to_int_list": `r = trap(to_int([1]))
    assert(r == Error((:type_error, (:to_int, [1]))))`,
		"to_int_str": `r = trap(to_int("42"))
    assert(r == Error((:type_error, (:to_int, "42"))))`,
		"to_float_atom": `r = trap(to_float(:x))
    assert(r == Error((:type_error, (:to_float, :x))))`,
		"to_float_list": `r = trap(to_float([1]))
    assert(r == Error((:type_error, (:to_float, [1]))))`,
		"to_float_str": `r = trap(to_float("1.5"))
    assert(r == Error((:type_error, (:to_float, "1.5"))))`,
		"str_to_float": `assert(Str.to_float("1.5") == Some(1.5))
    assert(Str.to_float(" 2 ") == Some(2.0))
    assert(Str.to_float("x") == None)
    assert(Str.to_float("") == None)
    assert(Str.to_float("inf") == None)
    assert(Str.to_float("nan") == None)`,
		"str_to_float_type": `r = trap(Str.to_float(1))
    assert(r == Error((:type_error, ((:str, :to_float), 1))))`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			runModuleSync(t, "module Main\nfn main() ->\n    "+body+"\n")
		})
	}
}
