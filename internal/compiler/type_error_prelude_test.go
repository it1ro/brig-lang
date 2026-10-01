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
		"vec_push": `r = trap(Vec.push(5, 1))
    assert(r == Error((:type_error, ((:vec, :push), 5))))`,
		"vec_set": `r = trap(Vec.set(5, 0, 1))
    assert(r == Error((:type_error, ((:vec, :set), 5))))`,
		"vec_get": `r = trap(Vec.get(5, 0))
    assert(r == Error((:type_error, ((:vec, :get), 5))))`,
		"map_put": `r = trap(Map.put(5, 1, 2))
    assert(r == Error((:type_error, ((:map, :put), 5))))`,
		"map_get": `r = trap(Map.get(5, 1))
    assert(r == Error((:type_error, ((:map, :get), 5))))`,
		"map_remove": `r = trap(Map.remove(5, 1))
    assert(r == Error((:type_error, ((:map, :remove), 5))))`,
		"map_keys": `r = trap(Map.keys(5))
    assert(r == Error((:type_error, ((:map, :keys), 5))))`,
		"bytes_to_str": `r = trap(Bytes.to_str(5))
    assert(r == Error((:type_error, ((:bytes, :to_str), 5))))`,
		"str_to_bytes": `r = trap(Str.to_bytes(5))
    assert(r == Error((:type_error, ((:str, :to_bytes), 5))))`,
		"str_split": `r = trap(Str.split(5, ","))
    assert(r == Error((:type_error, ((:str, :split), 5))))`,
		"str_split_sep": `r = trap(Str.split("a", 5))
    assert(r == Error((:type_error, ((:str, :split), 5))))`,
		"str_join": `r = trap(Str.join(5, ","))
    assert(r == Error((:type_error, ((:str, :join), 5))))`,
		"str_join_elem": `r = trap(Str.join([1], ","))
    assert(r == Error((:type_error, ((:str, :join), 1))))`,
		"str_trim": `r = trap(Str.trim(5))
    assert(r == Error((:type_error, ((:str, :trim), 5))))`,
		"str_find": `r = trap(Str.find(5, "a"))
    assert(r == Error((:type_error, ((:str, :find), 5))))`,
		"str_replace": `r = trap(Str.replace(5, "a", "b"))
    assert(r == Error((:type_error, ((:str, :replace), 5))))`,
		"str_starts_with": `r = trap(Str.starts_with?(5, "a"))
    assert(r == Error((:type_error, ((:str, :starts_with?), 5))))`,
		"str_ends_with": `r = trap(Str.ends_with?(5, "a"))
    assert(r == Error((:type_error, ((:str, :ends_with?), 5))))`,
		"str_lower": `r = trap(Str.lower(5))
    assert(r == Error((:type_error, ((:str, :lower), 5))))`,
		"str_upper": `r = trap(Str.upper(5))
    assert(r == Error((:type_error, ((:str, :upper), 5))))`,
		"str_slice": `r = trap(Str.slice(5, 0, 1))
    assert(r == Error((:type_error, ((:str, :slice), 5))))`,
		"str_slice_idx": `r = trap(Str.slice("ab", :a, 1))
    assert(to_str(r) == "Error((:type_error, (:index_key, :a)))")`,
		"str_to_int": `r = trap(Str.to_int(5))
    assert(r == Error((:type_error, ((:str, :to_int), 5))))`,
		"bytes_slice": `r = trap(Bytes.slice(5, 0, 1))
    assert(r == Error((:type_error, ((:bytes, :slice), 5))))`,
		"bytes_find": `r = trap(Bytes.find(5, b"a"))
    assert(r == Error((:type_error, ((:bytes, :find), 5))))`,
		"bytes_split": `r = trap(Bytes.split(5, b","))
    assert(r == Error((:type_error, ((:bytes, :split), 5))))`,
		"bytes_concat": `r = trap(Bytes.concat(5, b"a"))
    assert(r == Error((:type_error, ((:bytes, :concat), 5))))`,
		"bytes_at": `r = trap(Bytes.at(5, 0))
    assert(r == Error((:type_error, ((:bytes, :at), 5))))`,

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
		"timer_ms": `r = trap(Timer.send_after(-1, self(), :x))
    assert(r == Error((:type_error, ((:timer, :send_after), -1))))`,
		"timer_pid": `r = trap(Timer.send_after(0, 1, :x))
    assert(r == Error((:type_error, ((:timer, :send_after), 1))))`,
		"timer_cancel": `r = trap(Timer.cancel(1))
    assert(r == Error((:type_error, ((:timer, :cancel), 1))))`,
		"timer_await": `ref = Timer.send_after(1000, self(), :x)
    r = trap(await(ref, 0))
    _ = Timer.cancel(ref)
    assert(to_str(r) == "Error((:type_error, (:await, #<ref 0>)))")`,

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
    r3 = trap(Enum.map([1], fn (a, b) -> a))
    assert(r3 == Error((:function_clause, [1])))
`)
}
