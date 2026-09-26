package compiler_test

import (
	"strings"
	"testing"
)

// T-71: канонический process из §8.2 — Ok(c) на успехе, Error(e) на первом сбое.
func TestWithCanonical(t *testing.T) {
	runModule(t, `module Main
fn validate(x) -> if x > 0 then Ok(x) else Error(:invalid)
fn transform(x) -> if x < 100 then Ok(x * 2) else Error(:too_big)
fn save(x) -> Ok(x + 1)

fn process(input) ->
    with
        Ok(a) <- validate(input)
        Ok(b) <- transform(a)
        Ok(c) <- save(b)
        Ok(c)
    else
        Error(e) -> Error(e)

fn main() ->
    assert(process(5) == Ok(11))
    assert(process(-1) == Error(:invalid))
    assert(process(500) == Error(:too_big))
`)
}

// T-71, §8.2 п.1: без else первое несовпавшее значение пролетает наружу.
func TestWithoutElsePropagates(t *testing.T) {
	runModule(t, `module Main
fn chain(x, y) ->
    with
        Ok(a) <- x
        Some(b) <- y
        (a, b)

fn main() ->
    assert(chain(Ok(1), Some(2)) == (1, 2))
    assert(chain(Error(:e), Some(2)) == Error(:e))
    assert(chain(Ok(1), None) == None)
    assert(chain(42, None) == 42)
    v = with
        (:ok, n) <- (:ok, 7)
        n + 1
    assert(v == 8)
`)
}

// T-71, §8.2 п.3 / §10.4: неполный else — (:case_clause, val); else по порядку.
func TestWithIncompleteElse(t *testing.T) {
	runModule(t, `module Main
fn run(x) ->
    with
        Ok(a) <- x
        a
    else
        Error(:first) -> :one
        Error(_) -> :any_error
        Error(:never) -> :unreachable

fn main() ->
    assert(run(Ok(3)) == 3)
    assert(run(Error(:first)) == :one)
    assert(run(Error(:other)) == :any_error)
    r = trap(run(None))
    assert(r == Error((:case_clause, None)))
`)
}

// T-71: binds только в начале; тело — с первого statement без <-,
// видит связывания binds; binds видят предыдущие binds; else их не видит.
func TestWithBindsThenBody(t *testing.T) {
	runModule(t, `module Main
fn f(x) ->
    with
        Ok(a) <- Ok(x)
        [h, ..rest] <- [a, a + 1, a + 2]
        Ok(s) <- Ok(h + a)
        t = s * 10
        u = t + len(rest)
        u
    else
        other -> (:failed, other)

fn main() ->
    assert(f(1) == 22)
`)
}

// T-71: хвостовой вызов в теле и в ветке else — TAILCALL, без роста кадров.
func TestWithTailCall(t *testing.T) {
	src := `module Main
fn down(n) ->
    with
        true <- n > 0
        down(n - 1)
    else
        false -> finish(n)

fn finish(n) -> :done

fn main() ->
    assert(down(1000000) == :done)
`
	img := compileModule(t, src)
	dis := img.Functions["down"].Disassemble()
	if got := strings.Count(dis, "TAILCALL"); got != 2 {
		t.Fatalf("down: want 2 TAILCALL (body and else), got %d:\n%s", got, dis)
	}
	if strings.Contains(dis, " CALL ") {
		t.Fatalf("down has non-tail CALL:\n%s", dis)
	}
	runModule(t, src)
}
