package examples

import (
	"strings"

	"github.com/it1ro/brig-lang/internal/repl"
	"github.com/it1ro/brig-lang/internal/vm"
)

// Doctests исполняет доктесты исходника path (T-147, research 22): блоки
// ```brig repl внутри doc-комментариев `##` — тот же формат, что REPL-блоки
// спеки (§G.5), ответ сравнивается через `==`. Ввод каждого блока
// исполняется в сессии на свежей ВМ из newVM, ответ — в отдельной сессии
// на другой свежей ВМ; в обеих стоит программа entry (T-245): функции,
// типы и конструкторы модуля файла видны голыми именами, fn модуля
// затеняет одноимённый хелпер Repl. entry == nil — только то, что даёт
// newVM (модули stdlib). Line у результата — строка в path. Прочие блоки
// внутри `##` не исполняются.
func Doctests(path, src string, newVM func() *vm.VM, entry *repl.Entry) []Result {
	var out []Result
	for _, d := range repl.DocComments(src) {
		for _, b := range extractBlocks(path, d.Text) {
			if modeByMeta(strings.TrimSpace(strings.TrimPrefix(b.lang, "brig"))) != "repl" {
				continue
			}
			b.line += d.Line - 1
			out = append(out, replResult(b, runRepl(b.raw, newVM(), newVM(), entry)))
		}
	}
	return out
}
