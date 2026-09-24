// Package examples implements the check-examples tool (A2): extracting
// ```brig fenced blocks from design docs and running them through the parser.
package examples

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/it1ro/brig-lang/internal/parser"
)

// Result — отчёт по одному блоку: file:line:col — status — [error] (A2).
type Result struct {
	File   string
	Line   int // строка открывающего fence
	Col    int
	Mode   string
	OK     bool
	ErrMsg string // сообщение об ошибке (пусто при OK)
}

func (r Result) String() string {
	status := "ok"
	if !r.OK {
		status = "FAIL"
	}
	if r.ErrMsg == "" {
		return fmt.Sprintf("%s:%d:%d — %s", r.File, r.Line, r.Col, status)
	}
	return fmt.Sprintf("%s:%d:%d — %s — [%s]", r.File, r.Line, r.Col, status, r.ErrMsg)
}

// CheckFile прогоняет все brig-блоки файла через парсер-заглушку (A2).
func CheckFile(path string) ([]Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	blocks := extractBlocks(path, string(data))
	results := make([]Result, 0, len(blocks))
	for _, b := range blocks {
		results = append(results, checkBlock(b))
	}
	return results, nil
}

// block — один fenced-блок с метаданными.
type block struct {
	file string
	line int // строка открывающего fence (1-based)
	lang string
	raw  string // содержимое блока
}

// fenceRe: линия-ограждение из 3+ бэктиков. CommonMark: закрывающий fence
// должен быть НЕ короче открывающего — в дизайн-доках brig-блоки
// закрываются и ``` и ```` (последнее — когда вокруг внешний 4-бэктиковый
// fence, который нужно честно пропускать).
var fenceRe = regexp.MustCompile("^[ \\t]*(`{3,})[ \\t]*(.*)$")

// extractBlocks находит fenced-блоки и возвращает только ```brig*.
func extractBlocks(path, src string) []block {
	lines := strings.Split(src, "\n")
	var out []block
	inFence := false
	fenceLen := 0
	var cur block
	var body []string
	for i, ln := range lines {
		if m := fenceRe.FindStringSubmatch(ln); m != nil {
			n := len(m[1])
			if !inFence {
				inFence = true
				fenceLen = n
				cur = block{file: path, line: i + 1, lang: strings.TrimSpace(m[2])}
				body = nil
				continue
			}
			if n >= fenceLen {
				inFence = false
				cur.raw = strings.Join(body, "\n")
				if strings.HasPrefix(cur.lang, "brig") {
					out = append(out, cur)
				}
				continue
			}
			// Закрывающий fence короче открывающего — это содержимое.
			body = append(body, ln)
			continue
		}
		if inFence {
			body = append(body, ln)
		}
	}
	return out
}

// modeByMeta — режим из метки fence (module/repl/expr/stmt) или "" (нет).
func modeByMeta(meta string) string {
	for _, m := range []string{"module", "repl", "expr", "stmt"} {
		if strings.HasPrefix(meta, m) {
			return m
		}
	}
	return ""
}

var (
	reModuleMarker = regexp.MustCompile(`\b(module|import|fn\s+main)\b`)
	reResultType   = regexp.MustCompile(`Result\s*\[`)
	reFnNoArgs     = regexp.MustCompile(`\bfn\s+->`)
	reArgDotDot    = regexp.MustCompile(`\(\s*\.\.\s*\)`)
	reOkEquiv      = regexp.MustCompile(`Ok\([^)]*\)\s*≡`)
	// reTopLevelBind: "<ident> =" на колонке 0 — top-level связывание,
	// несовместимое с module-режимом (§9). Тело fn в примерах всегда
	// индентировано, поэтому строки колонки 0 не могут быть внутри fn.
	reTopLevelBind = regexp.MustCompile(`(?m)^[a-z][a-zA-Z0-9_]*\s*=\s`)
)

// hasTopLevelBind — есть ли в блоке top-level связывание (см. reTopLevelBind).
func hasTopLevelBind(raw string) bool {
	return reTopLevelBind.MatchString(raw)
}

// explicit — есть ли в метке fence явный режим (module/repl/expr/stmt).
func explicit(b block) bool {
	meta := strings.TrimSpace(strings.TrimPrefix(b.lang, "brig"))
	return modeByMeta(meta) != ""
}

// heuristicMode — эвристика A2 для блоков без метки.
func heuristicMode(raw string) string {
	if reModuleMarker.MatchString(raw) {
		return "module"
	}
	for _, ln := range strings.Split(raw, "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), ">") {
			return "repl"
		}
	}
	var nonEmpty []string
	for _, ln := range strings.Split(raw, "\n") {
		if strings.TrimSpace(ln) != "" {
			nonEmpty = append(nonEmpty, strings.TrimSpace(ln))
		}
	}
	if len(nonEmpty) == 1 && !strings.Contains(nonEmpty[0], "=") {
		return "expr"
	}
	return "stmt"
}

// stripComments убирает '#'-комментарии для семантических текстовых проверок.
func stripComments(raw string) string {
	var out []string
	for _, ln := range strings.Split(raw, "\n") {
		if idx := strings.Index(ln, "#"); idx >= 0 {
			ln = ln[:idx]
		}
		out = append(out, ln)
	}
	return strings.Join(out, "\n")
}

// checkBlock: выбор режима, обёртка expr/stmt, парсинг, текстовые проверки A2.
func checkBlock(b block) Result {
	meta := strings.TrimSpace(strings.TrimPrefix(b.lang, "brig"))
	mode := modeByMeta(meta)
	if mode == "" {
		mode = heuristicMode(b.raw)
	}

	// A2: неоднозначный блок без метки (модульные маркеры + top-level let) —
	// эвристика даёт module, но такой код в module-режиме запрещён (§9).
	// Требуем явную метку module|stmt|repl.
	if !explicit(b) && mode == "module" && hasTopLevelBind(b.raw) {
		return fail(b, mode, "ambiguous block: module markers + top-level bind; "+
			"add explicit mode label (```brig module|stmt|repl)")
	}

	// Текстовые проверки A2 (на блоке без комментариев).
	clean := stripComments(b.raw)
	switch {
	case reResultType.MatchString(clean):
		return fail(b, mode, "Result[...] forbidden in type position (B4)")
	case reFnNoArgs.MatchString(clean):
		return fail(b, mode, "fn -> ... removed (B2); use () -> ...")
	case reArgDotDot.MatchString(clean):
		return fail(b, mode, "f(..) without operand forbidden in args (M-004)")
	case reOkEquiv.MatchString(clean):
		return fail(b, mode, "Ok(x) ≡ (:ok, x) only in comments (§2.9)")
	}

	src := wrapForMode(mode, b.raw)
	switch mode {
	case "repl":
		if err := parseRepl(src); err != nil {
			return fail(b, mode, err.Error())
		}
	default:
		if err := parser.Parse(parser.ModeModule, src); err != nil {
			return fail(b, mode, err.Error())
		}
	}
	return Result{File: b.file, Line: b.line, Col: 1, Mode: mode, OK: true}
}

func fail(b block, mode, msg string) Result {
	return Result{File: b.file, Line: b.line, Col: 1, Mode: mode, OK: false, ErrMsg: msg}
}

// wrapForMode: expr/stmt оборачиваются в fn main() -> ... (A2).
func wrapForMode(mode, raw string) string {
	switch mode {
	case "expr":
		return "fn main() ->\n    " + strings.TrimSpace(raw) + "\n"
	case "stmt":
		var sb strings.Builder
		sb.WriteString("fn main() ->\n")
		for _, ln := range strings.Split(raw, "\n") {
			if strings.TrimSpace(ln) == "" {
				sb.WriteString("\n")
				continue
			}
			sb.WriteString("    ")
			sb.WriteString(ln)
			sb.WriteString("\n")
		}
		return sb.String()
	}
	return raw
}

// parseRepl: каждая строка — отдельный top-level стейтмент (§10.7);
// '>' — приглашение; вывод REPL отбрасывается эвристикой (A2).
func parseRepl(src string) error {
	for _, ln := range strings.Split(src, "\n") {
		line := strings.TrimSpace(ln)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, ">") {
			line = strings.TrimSpace(strings.TrimPrefix(line, ">"))
		}
		if line == "" {
			continue
		}
		// Вывод REPL (значения без признаков кода) отбрасывается (A2).
		if isReplOutput(line) {
			continue
		}
		if err := parser.Parse(parser.ModeRepl, line+"\n"); err != nil {
			return fmt.Errorf("repl line %q: %w", line, err)
		}
	}
	return nil
}

// isReplOutput: строка без '=' и операторов/скобок — вероятный вывод,
// а не код (A2: "строки без =, без оператора, не начинающиеся с ключевого слова").
func isReplOutput(line string) bool {
	if strings.ContainsAny(line, "=()[]{}<>+*%|:") {
		return false
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_]+$`).MatchString(line) {
		return false
	}
	return true
}
