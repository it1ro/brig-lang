package vm_test

import "testing"

// T-196 (#220, DD #217 вариант B): to_int/to_float при ошибке бросают
// ловимый (:type_error, (op, val)) для значений не своего типа и
// (:parse_error, (op, val)) для Str, которая не разбирается как число —
// вместо (:badarg, v).
func TestToIntToFloatErrors(t *testing.T) {
	cases := map[string]string{
		"to_int_atom": `r = trap(to_int(:x))
    assert(r == Error((:type_error, (:to_int, :x))))`,
		"to_int_list": `r = trap(to_int([1]))
    assert(r == Error((:type_error, (:to_int, [1]))))`,
		"to_int_bad_str": `r = trap(to_int("abc"))
    assert(r == Error((:parse_error, (:to_int, "abc"))))`,
		"to_float_atom": `r = trap(to_float(:x))
    assert(r == Error((:type_error, (:to_float, :x))))`,
		"to_float_list": `r = trap(to_float([1]))
    assert(r == Error((:type_error, (:to_float, [1]))))`,
		"to_float_bad_str": `r = trap(to_float("abc"))
    assert(r == Error((:parse_error, (:to_float, "abc"))))`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			runModuleSync(t, "module Main\nfn main() ->\n    "+body+"\n")
		})
	}
}
