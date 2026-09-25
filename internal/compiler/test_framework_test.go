package compiler_test

import "testing"

func TestTestFrameworkPassing(t *testing.T) {
	runModule(t, `module Main
fn add(a, b) -> a + b

fn main() ->
    Test.describe("add")
    Test.it("2 + 2 == 4", () -> Test.assert_eq(add(2, 2), 4))
    Test.it("0 + 0 == 0", () -> Test.assert_eq(add(0, 0), 0))
    Test.it("1 + 1 != 3", () -> Test.assert_ne(add(1, 1), 3))
    n = Test.run()
    assert(n == 0)
`)
}

func TestTestFrameworkReportsFailure(t *testing.T) {
	runModule(t, `module Main
fn main() ->
    Test.it("fails", () -> Test.assert_eq(1, 2))
    n = Test.run()
    assert(n == 1)
`)
}
