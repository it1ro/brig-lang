package compiler_test

import "testing"

// A-F4 / T-96 (design decision #42, вариант A): авто-raise §10.4 в
// прелюдии, индексации и акторных примитивах — ловимый raise
// (:type_error, (op, val)) / (:function_clause, args), а не фатальная
// ошибка актора мимо trap.
func TestPreludeTypeErrorsAreCatchable(t *testing.T) {
	cases := map[string]string{
		// прелюдия
		"len": `r = trap(len(5))
    assert(r == Error((:type_error, (:len, 5))))`,
		"map": `r = trap(map(fn (x) -> x, 5))
    assert(r == Error((:type_error, (:map, 5))))`,
		"filter": `r = trap(filter(fn (x) -> x, 5))
    assert(r == Error((:type_error, (:filter, 5))))`,
		"find": `r = trap(find(fn (x) -> x, 5))
    assert(r == Error((:type_error, (:find, 5))))`,
		"all": `r = trap(all(fn (x) -> x, 5))
    assert(r == Error((:type_error, (:all, 5))))`,
		"any": `r = trap(any(fn (x) -> x, 5))
    assert(r == Error((:type_error, (:any, 5))))`,
		"fold": `r = trap(fold(fn (a, x) -> a, 0, 5))
    assert(r == Error((:type_error, (:fold, 5))))`,
		"filter_predicate": `r = trap(filter(fn (x) -> 1, [1]))
    assert(r == Error((:type_error, (:filter_predicate, 1))))`,
		"find_predicate": `r = trap(find(fn (x) -> 1, [1]))
    assert(r == Error((:type_error, (:find_predicate, 1))))`,
		"all_predicate": `r = trap(all(fn (x) -> 1, [1]))
    assert(r == Error((:type_error, (:all_predicate, 1))))`,
		"any_predicate": `r = trap(any(fn (x) -> 1, [1]))
    assert(r == Error((:type_error, (:any_predicate, 1))))`,
		"vec_push": `r = trap(Vec.push(5, 1))
    assert(to_str(r) == "Error((:type_error, (:Vec.push, 5)))")`,
		"vec_set": `r = trap(Vec.set(5, 0, 1))
    assert(to_str(r) == "Error((:type_error, (:Vec.set, 5)))")`,
		"vec_get": `r = trap(Vec.get(5, 0))
    assert(to_str(r) == "Error((:type_error, (:Vec.get, 5)))")`,
		"vec_len": `r = trap(Vec.len(5))
    assert(to_str(r) == "Error((:type_error, (:Vec.len, 5)))")`,
		"map_put": `r = trap(Map.put(5, 1, 2))
    assert(to_str(r) == "Error((:type_error, (:Map.put, 5)))")`,
		"map_get": `r = trap(Map.get(5, 1))
    assert(to_str(r) == "Error((:type_error, (:Map.get, 5)))")`,
		"map_remove": `r = trap(Map.remove(5, 1))
    assert(to_str(r) == "Error((:type_error, (:Map.remove, 5)))")`,
		"map_keys": `r = trap(Map.keys(5))
    assert(to_str(r) == "Error((:type_error, (:Map.keys, 5)))")`,
		"bytes_to_str": `r = trap(Bytes.to_str(5))
    assert(to_str(r) == "Error((:type_error, (:Bytes.to_str, 5)))")`,
		"str_to_bytes": `r = trap(Str.to_bytes(5))
    assert(to_str(r) == "Error((:type_error, (:Str.to_bytes, 5)))")`,

		// индексация, диапазоны, записи
		"index_key": `xs = [1, 2]
    r = trap(xs[:a])
    assert(r == Error((:type_error, (:index_key, :a))))`,
		"index": `x = 5
    r = trap(x[0])
    assert(r == Error((:type_error, (:index, 5))))`,
		"range": `a = 1
    s = "a"
    r = trap(a to s)
    assert(r == Error((:type_error, (:range, (1, "a")))))`,
		"record_spread": `s = 5
    r = trap({ ..s })
    assert(r == Error((:type_error, (:record_spread, 5))))`,
		"field": `s = 5
    r = trap(s.name)
    assert(r == Error((:type_error, (:field, ("name", 5)))))`,
		"spread_call": `f = fn (x) -> x
    s = 5
    r = trap(f(..s))
    assert(r == Error((:type_error, (:spread, 5))))`,

		// акторные примитивы
		"send": `r = trap(send(5, 1))
    assert(r == Error((:type_error, (:send, 5))))`,
		"watch": `r = trap(watch(5))
    assert(r == Error((:type_error, (:watch, 5))))`,
		"unwatch": `r = trap(unwatch(5))
    assert(r == Error((:type_error, (:unwatch, 5))))`,
		"mailbox_size": `r = trap(mailbox_size(5))
    assert(r == Error((:type_error, (:mailbox_size, 5))))`,
		"recv_after": `ms = "a"
    r = trap(recv_after(ms))
    assert(r == Error((:type_error, (:after, "a"))))`,

		// арность
		"arity_closure": `f = fn (x) -> x
    r = trap(f(1, 2))
    assert(r == Error((:function_clause, [1, 2])))`,
		"arity_native": `r = trap(len(1, 2))
    assert(r == Error((:function_clause, [1, 2])))`,
		"arity_json_encode": `r = trap(Json.encode())
    assert(r == Error((:function_clause, [])))`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			src := "module Main\nfn main() ->\n    " + body + "\n"
			if name == "recv_after" {
				src = "module Main\nfn wait(ms) ->\n    recv\n        _ -> 0\n    after (ms) -> 1\n\nfn main() ->\n    ms = \"a\"\n    r = trap(wait(ms))\n    assert(r == Error((:type_error, (:after, \"a\"))))\n"
			}
			if err := runModuleErr(t, src); err != nil {
				t.Fatalf("want caught raise, got %v", err)
			}
		})
	}
}

// Арность через хвостовой вызов (TAILCALL) и из колбэка прелюдии.
func TestArityErrorsAreCatchable(t *testing.T) {
	runModule(t, `module Main
fn one(x) -> x
fn tail(f) -> f(1, 2)
fn tailn(v) -> len(v, v)
fn main() ->
    r1 = trap(tail(one))
    assert(r1 == Error((:function_clause, [1, 2])))
    r2 = trap(tailn(3))
    assert(r2 == Error((:function_clause, [3, 3])))
    r3 = trap(map(fn (a, b) -> a, [1]))
    assert(r3 == Error((:function_clause, [1])))
`)
}
