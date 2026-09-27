package examples

import (
	"strings"

	"github.com/it1ro/brig-lang/internal/vm"
)

// Doctests исполняет доктесты исходника path (T-147, research 22): блоки
// ```brig repl внутри doc-комментариев `##` — тот же формат, что REPL-блоки
// спеки (§G.5), ответ сравнивается через `==`. Ввод каждого блока
// исполняется на свежей ВМ из newVM (с загруженным модулем). Line у
// результата — строка в path. Прочие блоки внутри `##` не исполняются.
func Doctests(path, src string, newVM func() *vm.VM) []Result {
	var out []Result
	for _, d := range docComments(src) {
		for _, b := range extractBlocks(path, d.text) {
			if modeByMeta(strings.TrimSpace(strings.TrimPrefix(b.lang, "brig"))) != "repl" {
				continue
			}
			b.line += d.line - 1
			out = append(out, replResult(b, runRepl(b.raw, newVM())))
		}
	}
	return out
}

// docComment — подряд идущие строки `##` без префикса; line — строка
// первой из них в исходнике.
type docComment struct {
	line int
	text string
}

// docComments собирает doc-комментарии: строки, которые после отступа
// начинаются с `##`, но не с `###`. Префикс `##` и один пробел за ним
// снимаются.
func docComments(src string) []docComment {
	var out []docComment
	var cur []string
	start := 0
	flush := func() {
		if cur != nil {
			out = append(out, docComment{line: start, text: strings.Join(cur, "\n")})
			cur = nil
		}
	}
	for i, ln := range strings.Split(src, "\n") {
		s := strings.TrimLeft(ln, " ")
		if !strings.HasPrefix(s, "##") || strings.HasPrefix(s, "###") {
			flush()
			continue
		}
		if cur == nil {
			start = i + 1
		}
		s = strings.TrimPrefix(s, "##")
		cur = append(cur, strings.TrimPrefix(s, " "))
	}
	flush()
	return out
}
