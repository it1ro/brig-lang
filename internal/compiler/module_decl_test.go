package compiler_test

import "testing"

// T-134 (§11.1): alias встроенного модуля — доступ к нему как к оригиналу.
// Неизвестный модуль в `import`/`alias` — дело loader (T-135), см.
// TestLoadMissingModule и TestLoadMissingModuleAlias в internal/loader:
// Compile компилирует один файл и о существовании чужих модулей не знает.
func TestAliasBuiltinModule(t *testing.T) {
	runModule(t, `module Main
alias Json as J
fn main() ->
    assert(J.encode([1]) == "[1]")
`)
}
