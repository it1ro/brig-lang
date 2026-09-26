package compiler_test

import (
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/parser"
)

var updateBytecode = flag.Bool(
	"update-bytecode", false, "rewrite bytecode golden files")

// bytecodeCases — детерминированный набор модулей; имена файлов
// в testdata/bytecode/ совпадают с полем name.
var bytecodeCases = []struct {
	name string
	src  string
}{
	{"hello", `module Main
fn main() ->
    print("hi")
`},
	{"arith", `module Main
fn main() ->
    x = 2 + 3 * 4
    print(x)
`},
	{"fib", `module Main
fn fib(n) ->
    if n < 2 then n else fib(n - 1) + fib(n - 2)
fn main() ->
    print(fib(10))
`},
	{"tail", `module Main
fn sum_to(n, acc) ->
    if n == 0 then acc else sum_to(n - 1, acc + n)
fn main() ->
    print(sum_to(10, 0))
`},
	{"closure", `module Main
fn main() ->
    base = 100
    add_base = x -> base + x
    print(add_base(5))
`},
	{"trap_ensure", `module Main
fn main() ->
    result = trap
        print("body")
        ensure print("cleanup-1")
        ensure print("cleanup-2")
        :ok
    print(result)
`},
	{"fn_multiclause", `module Main
fn fact(0) -> 1
fn fact(n) -> n * fact(n - 1)
fn classify(n) when n > 0 -> :positive
fn classify(0) -> :zero
fn first((a, _)) -> a
fn pow(base, exp) ->
    fn go(acc, 0) -> acc
    fn go(acc, n) -> go(acc * base, n - 1)
    go(1, exp)
fn main() ->
    print(fact(5))
`},
	{"recv_after", `module Main
fn worker() ->
    recv
        :a -> :got_a
    after 10 -> :timeout

fn main() ->
    worker()
`},
}

func TestBytecodeGolden(t *testing.T) {
	dir := filepath.Join("..", "..", "testdata", "bytecode")
	if *updateBytecode {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range bytecodeCases {
		t.Run(tc.name, func(t *testing.T) {
			prog, err := parser.ParseProgram(parser.ModeModule, tc.src)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			img, err := compiler.New().Compile(prog)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			got := renderAll(img)
			path := filepath.Join(dir, tc.name+".txt")
			if *updateBytecode {
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v (run make update-bytecode)", path, err)
			}
			if string(want) != got {
				t.Fatalf("%s mismatch:\n--- want ---\n%s\n--- got ---\n%s",
					path, want, got)
			}
		})
	}
}

// renderAll выводит все функции модуля в детерминированном порядке
// имён — совпадает с выводом `brig run --dump-bytecode` (sort в
// cmd/brig/main.go).
func renderAll(img *compiler.ProgramImage) string {
	names := make([]string, 0, len(img.Functions))
	for name := range img.Functions {
		names = append(names, name)
	}
	sort.Strings(names)
	var sb strings.Builder
	for _, name := range names {
		sb.WriteString(img.Functions[name].Disassemble())
	}
	return sb.String()
}
