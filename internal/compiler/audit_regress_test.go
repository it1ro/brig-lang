package compiler_test

import (
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/compiler"
	"github.com/it1ro/brig-lang/internal/parser"
)

// S-F1: интерполяция строк ещё не реализована (T-53/T-54). До готовности
// Compile обязан вернуть ошибку с текстом "interpolation", а не эмитить
// сырой текст "\(x)" (T-04). Экранированный "\\(" остаётся литералом.
func TestAuditInterpolationFailsFast(t *testing.T) {
	t.Run("interp_rejected", func(t *testing.T) {
		// Probe p/t3_interp.brig: "x = \(x)" must fail at compile time.
		src := `module Main
fn main() ->
    x = 5
    print("x = \(x)")
`
		prog, err := parser.ParseProgram(parser.ModeModule, src)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		_, err = compiler.New().Compile(prog)
		if err == nil {
			t.Fatalf("Compile: want error containing %q, got nil", "interpolation")
		}
		if !strings.Contains(err.Error(), "interpolation") {
			t.Fatalf("Compile: want error containing %q, got %v", "interpolation", err)
		}
	})

	t.Run("escaped_paren_ok", func(t *testing.T) {
		src := `module Main
fn main() ->
    print("\\(")
`
		prog, err := parser.ParseProgram(parser.ModeModule, src)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if _, err := compiler.New().Compile(prog); err != nil {
			t.Fatalf("Compile: escaped \\\\( must succeed, got %v", err)
		}
	})
}
