package compiler

import (
	"fmt"
	"sync"

	"github.com/it1ro/brig-lang/internal/ast"
	"github.com/it1ro/brig-lang/internal/vm"
	"github.com/it1ro/brig-lang/stdlib"
)

// Встроенная stdlib на Brig (T-146): модули `List`, `Option`, `Result`
// компилируются один раз на процесс и ставятся в каждую ВМ глобалами
// `M.f` — так же, как vm.New ставит Go-нативные модули. Программа их не
// перекомпилирует: вызов `List.take(…)` — GETGLOBAL `List.take`
// (resolveModule). Чанки не меняются при исполнении, поэтому образ
// разделяют все ВМ процесса.

var (
	stdOnce sync.Once
	stdImg  *ProgramImage
	stdErr  error
)

// StdlibImage — скомпилированные модули stdlib. Модули — не входные:
// пустой входной модуль, у остальных префикс `M.` (как у импортированных).
// Образ всегда проходит vm.Verify, независимо от Verify.
func StdlibImage() (*ProgramImage, error) {
	stdOnce.Do(func() {
		stdImg, stdErr = compileStdlib()
	})
	return stdImg, stdErr
}

func compileStdlib() (*ProgramImage, error) {
	mods, err := stdlib.Modules()
	if err != nil {
		return nil, fmt.Errorf("internal: stdlib: %w", err)
	}
	cms := make([]Module, 0, len(mods)+1)
	cms = append(cms, Module{Prog: &ast.Program{}})
	for _, m := range mods {
		cms = append(cms, Module{Name: m.Name, Path: m.Path, Prog: m.Prog})
	}
	c := New()
	c.preludeQualified = true
	img, err := c.CompileProgram(cms)
	if err != nil {
		return nil, fmt.Errorf("internal: stdlib: %w", err)
	}
	if err := verifyImage(img); err != nil {
		return nil, fmt.Errorf("internal: stdlib: %w", err)
	}
	return img, nil
}

// InstallStdlib ставит функции stdlib в глобалы ВМ m. Ошибка — только
// если встроенный исходник не компилируется (дефект сборки).
func InstallStdlib(m *vm.VM) error {
	img, err := StdlibImage()
	if err != nil {
		return err
	}
	for name, fn := range img.Functions {
		m.DefineGlobal(name, vm.FuncValue(fn))
	}
	return nil
}
