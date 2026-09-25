package compiler_test

import "github.com/it1ro/brig-lang/internal/compiler"

// Активирует dataflow-верификатор vm.Verify во всех тестах пакета.
// При расхождении компилятора с инвариантами §7/§12 — падение на
// стадии Compile, а не на runtime.
func init() { compiler.Verify = true }
