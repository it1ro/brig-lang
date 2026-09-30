package compiler_test

import (
	"strings"
	"testing"
)

// T-150 (#272): пробы для оставшихся `срез:` в compiler.go, прогнанные
// через настоящий пайплайн parser → sema → compiler. Код компилятора
// не меняется — таблица результатов идёт в issue #272.

// Сайт compiler.go:1404 (compileExpr, ast.RegexExpr) — regex-литералы
// парсятся нормально, движок ещё не реализован (T-194 / #298).
func TestT150RegexNotImplemented(t *testing.T) {
	err := runModuleErr(t, `module Main
fn main() ->
    r = rx"foo"
    print(r)
`)
	want := "срез: regex не реализован"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("err = %v, want contains %q", err, want)
	}
}

// Сайт compiler.go:1629 (compileModulePath, ветка ref.builtin != "") —
// голый member встроенного модуля с большой буквы, не вызов. Реальная
// (хоть и бессмысленная) программа доходит именно до этой строки и
// получает протекающее сообщение вместо нормальной ошибки sema/`brig check`.
// Must-баг: sema пропускает это без диагностики.
func TestT150BareBuiltinModuleUpperMember(t *testing.T) {
	err := runModuleErr(t, `module Main
fn main() ->
    x = Vec.Foo
    print(x)
`)
	want := "срез: неподдерживаемое выражение Vec.Foo"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("err = %v, want contains %q", err, want)
	}
	if strings.HasPrefix(err.Error(), "sema:") {
		t.Fatalf("expected compile-time error (past sema), got sema rejection: %v", err)
	}
}

// Сайт compiler.go:2076 (compileGenericCall, спред-вызов локальной fn
// с захватом) — уже покрыт TestCallSpreadLocalCaptureStillRejected
// (spread_construct_test.go). T-155 / #209 закрывает эту дыру и сейчас
// заблокирован T-150.
