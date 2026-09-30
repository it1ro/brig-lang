package compiler

import (
	"strings"
	"testing"

	"github.com/it1ro/brig-lang/internal/ast"
)

// T-150 (#272): white-box пробы для сайтов `срез:`, недостижимых через
// parser.Parse — показывают, что до строки можно дойти только прямой
// сборкой AST, не через реальный Brig-исходник. Код компилятора не меняется.

// compiler.go:1275 (compileStmt, default) — *ast.BlockStmt реализует
// ast.Stmt, но не входит ни в один []ast.Stmt, который получает
// compileStmts (парсер разворачивает блок в стейтменты, а не кладёт
// его как элемент списка) — единственный способ дойти до default —
// передать *ast.BlockStmt в compileStmt прямой сборкой.
func TestT150UnreachableStmtDefault(t *testing.T) {
	c := New()
	fc := c.newFuncCompiler(nil)
	blk := ast.NewBlockStmt(nil, 0, 0)

	err := catchCompile(func() error {
		return fc.compileStmt(blk, discard)
	})
	want := "срез: неподдерживаемый стейтмент"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("err = %v, want contains %q", err, want)
	}
}

// compiler.go:1440 (compileExpr, default) — ast.NewAssignExpr существует
// в internal/ast/construct.go, но parser его нигде не вызывает (grep по
// internal/parser даёт 0 совпадений): мёртвый конструктор. Единственный
// способ дойти до default — собрать assignExpr вручную.
func TestT150UnreachableExprDefault(t *testing.T) {
	c := New()
	fc := c.newFuncCompiler(nil)
	assign := ast.NewAssignExpr("x", ast.NewLiteralExpr("1", 0, 0), 0, 0)

	err := catchCompile(func() error {
		return fc.compileExpr(assign, discard)
	})
	want := "срез: неподдерживаемое выражение"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("err = %v, want contains %q", err, want)
	}
}

// compiler.go:1800 (compileBinary, неизвестный OpStr()) — binOp()
// покрывает все операторные токены из brig.ebnf; парсер не может
// построить BinaryExpr с другим Op. Единственный способ дойти до
// default — собрать BinaryExpr с придуманным оператором вручную.
func TestT150UnreachableBinaryOpDefault(t *testing.T) {
	c := New()
	fc := c.newFuncCompiler(nil)
	bin := ast.NewBinaryExpr("не-оператор", ast.NewLiteralExpr("1", 0, 0), ast.NewLiteralExpr("2", 0, 0), 0, 0).(ast.BinaryExpr)

	err := catchCompile(func() error {
		return fc.compileBinary(bin, discard)
	})
	want := "срез: оператор"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("err = %v, want contains %q", err, want)
	}
}

// compiler.go:3707 (compilePattern, case PatternWildcard, String() != "_")
// — ast.PatternWildcard — тот же метод-набор, что и ast.Pattern, поэтому
// это структурный catch-all. Через reachable-из-парсера пути в него
// попадают только wildcardPat (String() всегда "_", до ошибки не доходит)
// и spreadPat — но bindRestTail обрабатывает голый `..name` без вызова
// compilePattern (compiler.go:1031-1034: ранний return для case без
// элементов перед `..`, и tail[:n-1] отрезает SpreadPattern-элемент
// перед вызовом compilePattern в оставшихся случаях). Проба показывает,
// что compilePattern(spreadPat) даёт ошибку, — при этом spreadPat
// никогда не передаётся в compilePattern из реального пайплайна.
func TestT150UnreachablePatternWildcardBranch(t *testing.T) {
	c := New()
	fc := c.newFuncCompiler(nil)
	sp := ast.NewSpreadPat("xs", 0, 0)

	_, err := fc.compilePattern(sp)
	want := "срез: неподдерживаемый паттерн"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("err = %v, want contains %q", err, want)
	}
}

// fakePattern — сторонняя реализация ast.Pattern вне ast.go/pattern.go,
// чтобы показать, что внешний default compilePattern (compiler.go:3709)
// в принципе недостижим: ast.PatternWildcard объявлен с тем же
// методом-набором, что и ast.Pattern (accessors.go:290-292), поэтому
// `case ast.PatternWildcard` в Go type switch ловит любое значение
// статического типа ast.Pattern — переключаться на default после
// switch не на чём.
type fakePattern struct{}

func (fakePattern) Pos() int        { return 0 }
func (fakePattern) End() int        { return 0 }
func (fakePattern) String() string  { return "<fake>" }
func (fakePattern) AsIdent() string { return "" }

func TestT150UnreachablePatternOuterDefault(t *testing.T) {
	c := New()
	fc := c.newFuncCompiler(nil)
	var pat ast.Pattern = fakePattern{}

	_, err := fc.compilePattern(pat)
	// Любая сторонняя реализация ast.Pattern попадает в ветку
	// PatternWildcard (compiler.go:3703-3707), не во внешний default
	// (compiler.go:3709) — сообщение содержит и тип, и String().
	want := "срез: неподдерживаемый паттерн compiler.fakePattern \"<fake>\""
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("err = %v, want contains %q", err, want)
	}
}
