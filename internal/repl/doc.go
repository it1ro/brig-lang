package repl

import "strings"

// DocComment — подряд идущие строки `##` без префикса (§11.6). Line —
// строка первой из них в исходнике. Тот же разбор, что у доктестов
// (internal/examples) и у `h` / Session.Doc.
type DocComment struct {
	Line int
	Text string
}

// DocComments собирает doc-комментарии: строки, которые после отступа
// начинаются с `##`, но не с `###`. Префикс `##` и один пробел за ним
// снимаются.
func DocComments(src string) []DocComment {
	var out []DocComment
	var cur []string
	start := 0
	flush := func() {
		if cur != nil {
			out = append(out, DocComment{Line: start, Text: strings.Join(cur, "\n")})
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
